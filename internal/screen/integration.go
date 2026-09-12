package screen

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/oklog/ulid/v2"
)

// IssueIntegration authorizes and durably deduplicates before any delivery.
func (s *Service) IssueIntegration(ctx context.Context, principalID, operationID, screenID string, kind domain.CommandKind, payload domain.CommandPayload) (*domain.Command, error) {
	id, err := ulid.ParseStrict(operationID)
	if err != nil || id.String() != operationID {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "operation_id must be a canonical ULID")
	}
	if err := s.validateIntegrationPayload(kind, payload); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(struct {
		Screen  string
		Kind    domain.CommandKind
		Payload domain.CommandPayload
	}{screenID, kind, payload})
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	var cmd *domain.Command
	created := false
	err = s.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		created = false
		now := s.now()
		candidate := &domain.Command{ScreenID: screenID, Kind: kind, Payload: payload, IssuedBy: "integration:" + principalID, IssuedAt: now, ExpiresAt: now.Add(s.ttl), Status: domain.CommandAccepted}
		if err := s.authorizeIntegration(ctx, tx, principalID, "screens.control", candidate); err != nil {
			return err
		}
		existing, storedHash, err := s.db.Operations().WithTx(tx).Get(ctx, principalID, operationID)
		if err == nil {
			if storedHash != hash {
				return domain.Errorf(domain.CodeIdempotencyConflict, "operation_id already has different parameters")
			}
			cmd = existing
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		at := ulid.Time(id.Time())
		if at.Before(now.Add(-24*time.Hour)) || at.After(now.Add(5*time.Minute)) {
			return domain.Errorf(domain.CodeOperationExpired, "operation_id timestamp outside allowed window")
		}
		cmd = candidate
		if s.hub == nil || !s.hub.Online(screenID) {
			cmd.Status = domain.CommandFailed
			cmd.ErrorCode = CodeScreenOffline
			cmd.ResolvedAt = &now
		}
		if err := s.db.Commands().IssueInTx(ctx, tx, cmd); err != nil {
			return err
		}
		if err := s.db.Operations().WithTx(tx).Insert(ctx, principalID, operationID, hash, cmd.ID, now); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if cmd.ErrorCode == CodeScreenOffline {
		return cmd, domain.Errorf(domain.CodeScreenOffline, "screen has no live session")
	}
	if !created {
		return cmd, nil
	}
	if err := s.hub.Deliver(screenID, cmd); err != nil {
		s.resolve(ctx, cmd, domain.CommandFailed, CodeDeliveryFailed, nil)
		return cmd, nil
	}
	now := s.now()
	if err := s.db.Commands().MarkDelivered(ctx, cmd.ID, now); err != nil {
		return cmd, err
	}
	cmd.DeliveredAt = &now
	return cmd, nil
}
func (s *Service) validateIntegrationPayload(kind domain.CommandKind, p domain.CommandPayload) error {
	switch kind {
	case domain.CommandRefresh:
		if p == (domain.CommandPayload{}) {
			return nil
		}
	case domain.CommandNavigate:
		if p.PhotoID == "" && (p.Route != domain.RoutePhotos || p.Collection != "") {
			return s.validateNavigate(&p)
		}
	case domain.CommandShow:
		if p.PhotoID != "" && p.Route == "" && p.Collection == "" {
			return nil
		}
	}
	return domain.Errorf(domain.CodeInvalidCommand, "invalid kind-specific command payload")
}
func (s *Service) authorizeIntegration(ctx context.Context, tx *sql.Tx, principalID, permission string, cmd *domain.Command) error {
	p, err := s.db.Integrations().WithTx(tx).Get(ctx, principalID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Errorf(domain.CodeUnauthorized, "integration unavailable")
	}
	if err != nil {
		return err
	}
	if !p.Active(s.now()) {
		return domain.Errorf(domain.CodeUnauthorized, "integration unavailable")
	}
	if !p.Policy.Allows(permission) || !p.Policy.AllowsScreen(cmd.ScreenID) {
		return domain.Errorf(domain.CodeForbidden, "integration policy denies command")
	}
	var active int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM screens WHERE id=? AND status='active'`, cmd.ScreenID).Scan(&active)
	if err != nil {
		return err
	}
	if active == 0 {
		return domain.Errorf(domain.CodeNotFound, "screen unavailable")
	}
	if cmd.Kind == domain.CommandShow {
		_, err := s.db.Photos().WithTx(tx).GetScoped(ctx, p.Policy.SourceIDs, cmd.Payload.PhotoID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Errorf(domain.CodeForbidden, "photo unavailable under integration policy")
		}
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM data_sources WHERE status='active'`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if !p.Policy.AllowsSource(id) {
			return domain.Errorf(domain.CodeForbidden, "collection controls require all active sources")
		}
	}
	return rows.Err()
}

// GetIntegrationCommand returns only a principal's own currently authorized command.
func (s *Service) GetIntegrationCommand(ctx context.Context, principalID, commandID string) (*domain.Command, error) {
	return s.getIntegration(ctx, principalID, commandID, false)
}

// GetIntegrationOperation recovers a command when the original response was lost.
func (s *Service) GetIntegrationOperation(ctx context.Context, principalID, operationID string) (*domain.Command, error) {
	return s.getIntegration(ctx, principalID, operationID, true)
}
func (s *Service) getIntegration(ctx context.Context, principalID, id string, operation bool) (*domain.Command, error) {
	var cmd *domain.Command
	err := s.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		var err error
		r := s.db.Operations().WithTx(tx)
		if operation {
			cmd, _, err = r.Get(ctx, principalID, id)
		} else {
			cmd, err = r.GetCommand(ctx, principalID, id)
		}
		if err != nil {
			return err
		}
		return s.authorizeIntegration(ctx, tx, principalID, "commands.read", cmd)
	})
	if err != nil {
		return nil, err
	}
	return cmd, nil
}
