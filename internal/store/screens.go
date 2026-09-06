package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// Screens is the screens repository.
type Screens struct{ db *DB }

// Screens returns the screen repository.
func (d *DB) Screens() *Screens { return &Screens{db: d} }

const screenColumns = `id, name, token_hash, status, created_at, approved_at, revoked_at,
	last_seen_at, last_ip, client_version, current_route, last_sequence, applied_sequence`

// Create registers an approved screen. A duplicate ID returns domain.ErrConflict.
func (s *Screens) Create(ctx context.Context, sc *domain.Screen) error {
	if sc.Status == "" {
		sc.Status = domain.ScreenActive
	}
	if sc.CreatedAt.IsZero() {
		sc.CreatedAt = time.Now()
	}
	if sc.ApprovedAt.IsZero() {
		sc.ApprovedAt = sc.CreatedAt
	}
	_, err := s.db.sql.ExecContext(ctx, `
		INSERT INTO screens (id, name, token_hash, status, created_at, approved_at, last_sequence, applied_sequence)
		VALUES (?, ?, ?, ?, ?, ?, 0, 0)`,
		sc.ID, sc.Name, sc.TokenHash, string(sc.Status), FormatTime(sc.CreatedAt), FormatTime(sc.ApprovedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("screen %q: %w", sc.ID, domain.ErrConflict)
		}
		return fmt.Errorf("store: create screen: %w", err)
	}
	return nil
}

// Get returns one screen by ID.
func (s *Screens) Get(ctx context.Context, id string) (*domain.Screen, error) {
	row := s.db.sql.QueryRowContext(ctx, `SELECT `+screenColumns+` FROM screens WHERE id = ?`, id)
	sc, err := scanScreen(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get screen %q: %w", id, err)
	}
	return sc, nil
}

// GetByTokenHash resolves a credential to an active screen.
func (s *Screens) GetByTokenHash(ctx context.Context, hash string) (*domain.Screen, error) {
	row := s.db.sql.QueryRowContext(ctx, `SELECT `+screenColumns+` FROM screens WHERE token_hash = ?`, hash)
	sc, err := scanScreen(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get screen by token: %w", err)
	}
	return sc, nil
}

// List returns every screen ordered by ID.
func (s *Screens) List(ctx context.Context) ([]domain.Screen, error) {
	rows, err := s.db.sql.QueryContext(ctx, `SELECT `+screenColumns+` FROM screens ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list screens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Screen
	for rows.Next() {
		sc, err := scanScreen(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan screen: %w", err)
		}
		out = append(out, *sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate screens: %w", err)
	}
	return out, nil
}

// Revoke marks a screen revoked; its credential stops working immediately.
func (s *Screens) Revoke(ctx context.Context, id string, now time.Time) error {
	res, err := s.db.sql.ExecContext(ctx,
		`UPDATE screens SET status = 'revoked', revoked_at = ? WHERE id = ?`, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: revoke screen %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// TouchSeen records presence information observed on a request or heartbeat.
func (s *Screens) TouchSeen(ctx context.Context, id, ip, clientVersion string, now time.Time) error {
	_, err := s.db.sql.ExecContext(ctx, `
		UPDATE screens SET last_seen_at = ?,
			last_ip = COALESCE(NULLIF(?, ''), last_ip),
			client_version = COALESCE(NULLIF(?, ''), client_version)
		WHERE id = ?`, FormatTime(now), ip, clientVersion, id)
	if err != nil {
		return fmt.Errorf("store: touch screen %q: %w", id, err)
	}
	return nil
}

// SetRoute stores the client-reported route and applied sequence.
func (s *Screens) SetRoute(ctx context.Context, id string, route *domain.RouteState, appliedSeq int64, now time.Time) error {
	var blob sql.NullString
	if route != nil {
		b, err := json.Marshal(route)
		if err != nil {
			return fmt.Errorf("store: encode route: %w", err)
		}
		blob = sql.NullString{String: string(b), Valid: true}
	}
	_, err := s.db.sql.ExecContext(ctx,
		`UPDATE screens SET current_route = ?, applied_sequence = ?, last_seen_at = ? WHERE id = ?`,
		blob, appliedSeq, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set screen route %q: %w", id, err)
	}
	return nil
}

// NextSequence increments and returns the command sequence for a screen. It
// must be called inside the same transaction that inserts the command.
func (s *Screens) NextSequence(ctx context.Context, tx *sql.Tx, id string) (int64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE screens SET last_sequence = last_sequence + 1 WHERE id = ?`, id); err != nil {
		return 0, fmt.Errorf("store: bump sequence %q: %w", id, err)
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT last_sequence FROM screens WHERE id = ?`, id).Scan(&seq); err != nil {
		return 0, fmt.Errorf("store: read sequence %q: %w", id, err)
	}
	return seq, nil
}

func scanScreen(sc scanner) (*domain.Screen, error) {
	var (
		s                                domain.Screen
		status, createdAt, approvedAt    string
		revokedAt, lastSeenAt            sql.NullString
		lastIP, clientVersion, routeBlob sql.NullString
	)
	if err := sc.Scan(&s.ID, &s.Name, &s.TokenHash, &status, &createdAt, &approvedAt, &revokedAt,
		&lastSeenAt, &lastIP, &clientVersion, &routeBlob, &s.LastSequence, &s.AppliedSequence); err != nil {
		return nil, err
	}
	s.Status = domain.ScreenStatus(status)
	s.CreatedAt = timeVal(createdAt)
	s.ApprovedAt = timeVal(approvedAt)
	s.RevokedAt = timePtr(revokedAt)
	s.LastSeenAt = timePtr(lastSeenAt)
	s.LastIP = lastIP.String
	s.ClientVersion = clientVersion.String
	if routeBlob.Valid && routeBlob.String != "" {
		var r domain.RouteState
		if err := json.Unmarshal([]byte(routeBlob.String), &r); err == nil {
			s.CurrentRoute = &r
		}
	}
	return &s, nil
}

// SetTokenHash replaces the stored credential hash of a screen. The pairing
// claim uses it so a token is generated only when it is handed out.
func (s *Screens) SetTokenHash(ctx context.Context, id, hash string) error {
	res, err := s.db.sql.ExecContext(ctx, `UPDATE screens SET token_hash = ? WHERE id = ?`, hash, id)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("screen token: %w", domain.ErrConflict)
		}
		return fmt.Errorf("store: set screen token hash %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// RecordHeartbeat stores everything one heartbeat carries in a single
// statement: presence, the client-reported route, the applied sequence, the
// client version and the observed address (design §6.5).
func (s *Screens) RecordHeartbeat(ctx context.Context, id, ip, clientVersion string,
	route *domain.RouteState, appliedSeq int64, now time.Time) error {
	var blob sql.NullString
	if route != nil && route.Valid() {
		b, err := json.Marshal(route)
		if err != nil {
			return fmt.Errorf("store: encode route: %w", err)
		}
		blob = sql.NullString{String: string(b), Valid: true}
	}
	_, err := s.db.sql.ExecContext(ctx, `
		UPDATE screens SET last_seen_at = ?,
			last_ip = COALESCE(NULLIF(?, ''), last_ip),
			client_version = COALESCE(NULLIF(?, ''), client_version),
			current_route = COALESCE(?, current_route),
			applied_sequence = MAX(applied_sequence, ?)
		WHERE id = ?`,
		FormatTime(now), ip, clientVersion, blob, appliedSeq, id)
	if err != nil {
		return fmt.Errorf("store: record heartbeat %q: %w", id, err)
	}
	return nil
}
