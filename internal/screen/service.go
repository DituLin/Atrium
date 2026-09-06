// Package screen owns the command lifecycle of technical design §6.5:
// validation, sequencing, delivery through the WebSocket hub, acknowledgement
// and the expiry rules that separate `expired` from `unknown`.
package screen

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
)

// AckGrace is how long a delivered command waits for its acknowledgement past
// its expiry before it becomes `unknown` (design §6.5).
const AckGrace = 2 * time.Second

// ExpirerInterval is the expirer tick from design §4.1.
const ExpirerInterval = time.Second

// Error codes recorded on a command. They are stable strings shared with the
// CLI and the runbook.
const (
	CodeScreenOffline   = "screen_offline"
	CodeDeliveryFailed  = "delivery_failed"
	CodeExpired         = "expired"
	CodeAckTimeout      = "ack_timeout"
	CodeServerRestart   = "server_restart"
	CodeSessionReplaced = "session_replaced"
)

// Deliverer is the hub seam: the service never touches a socket directly.
type Deliverer interface {
	// Deliver queues a command for a screen's live session.
	Deliver(screenID string, cmd *domain.Command) error
	// Online reports session presence with a fresh heartbeat.
	Online(screenID string) bool
}

// Options configures a Service.
type Options struct {
	DB     *store.DB
	Hub    Deliverer
	Logger *slog.Logger
	Now    func() time.Time
	// TTL is screens.command_ttl (10 s by default).
	TTL time.Duration
}

// Service issues and resolves screen commands.
type Service struct {
	db  *store.DB
	hub Deliverer
	log *slog.Logger
	now func() time.Time
	ttl time.Duration
}

// NewService builds the command service.
func NewService(opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.TTL <= 0 {
		opts.TTL = 10 * time.Second
	}
	return &Service{db: opts.DB, hub: opts.Hub, log: opts.Logger, now: opts.Now, ttl: opts.TTL}
}

// Issue validates, persists and delivers one command. An offline screen still
// produces a stored command, resolved as failed/screen_offline, and a
// *domain.Error with CodeScreenOffline so the route can answer 409 with the
// command body (PRD §5.3: nothing is queued for an offline screen).
func (s *Service) Issue(ctx context.Context, screenID string, kind domain.CommandKind,
	payload domain.CommandPayload, issuedBy string) (*domain.Command, error) {
	sc, err := s.db.Screens().Get(ctx, screenID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.Errorf(domain.CodeNotFound, "screen %q is not registered", screenID)
		}
		return nil, err
	}
	if sc.Status != domain.ScreenActive {
		return nil, domain.Errorf(domain.CodeNotFound, "screen %q is not registered", screenID)
	}
	if err := s.Validate(ctx, kind, &payload); err != nil {
		return nil, err
	}

	now := s.now()
	cmd := &domain.Command{
		ScreenID: screenID, Kind: kind, Payload: payload, IssuedBy: issuedBy,
		IssuedAt: now, ExpiresAt: now.Add(s.ttl), Status: domain.CommandAccepted,
	}

	if s.hub == nil || !s.hub.Online(screenID) {
		cmd.Status = domain.CommandFailed
		cmd.ErrorCode = CodeScreenOffline
		cmd.ResolvedAt = &now
		if err := s.db.Commands().Issue(ctx, cmd); err != nil {
			return nil, err
		}
		return cmd, domain.Errorf(domain.CodeScreenOffline, "screen %q has no live session", screenID)
	}

	if err := s.db.Commands().Issue(ctx, cmd); err != nil {
		return nil, err
	}
	if err := s.hub.Deliver(screenID, cmd); err != nil {
		s.log.Warn("command not delivered", "component", "screen",
			"event", "delivery_failed", "screen_id", screenID, "command_id", cmd.ID)
		s.resolve(ctx, cmd, domain.CommandFailed, CodeDeliveryFailed, nil)
		return cmd, nil
	}
	delivered := s.now()
	if err := s.db.Commands().MarkDelivered(ctx, cmd.ID, delivered); err != nil {
		return nil, err
	}
	cmd.DeliveredAt = &delivered
	s.log.Info("command issued", "component", "screen", "event", "command_issued",
		"screen_id", screenID, "command_id", cmd.ID, "kind", string(kind), "sequence", cmd.Sequence)
	return cmd, nil
}

// resolve applies a terminal status and mirrors it onto the in-memory command.
func (s *Service) resolve(ctx context.Context, cmd *domain.Command, status domain.CommandStatus,
	errCode string, result *domain.CommandResult) {
	now := s.now()
	changed, err := s.db.Commands().Resolve(ctx, cmd.ID, status, errCode, result, now)
	if err != nil {
		s.log.Warn("command not resolved", "component", "screen",
			"event", "resolve_failed", "command_id", cmd.ID, "error", err.Error())
		return
	}
	if !changed {
		return
	}
	cmd.Status = status
	cmd.ErrorCode = errCode
	cmd.ResolvedAt = &now
	if result != nil {
		cmd.Result = result
	}
}

// Get returns one command.
func (s *Service) Get(ctx context.Context, id string) (*domain.Command, error) {
	return s.db.Commands().Get(ctx, id)
}

// List returns recent commands, filtered by screen and status.
func (s *Service) List(ctx context.Context, screenID string, status domain.CommandStatus, limit int) ([]domain.Command, error) {
	return s.db.Commands().List(ctx, screenID, status, limit)
}
