package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// Pairings is the pairings repository.
type Pairings struct{ db *DB }

// Pairings returns the pairing repository.
func (d *DB) Pairings() *Pairings { return &Pairings{db: d} }

const pairingColumns = `id, code, status, client_hint, remote_ip, created_at, expires_at,
	approved_screen_id, approved_at, claimed_at`

// Create stores a new pending pairing. A duplicate code returns domain.ErrConflict.
func (p *Pairings) Create(ctx context.Context, pr *domain.Pairing) error {
	if pr.ID == "" {
		pr.ID = domain.NewID()
	}
	if pr.Status == "" {
		pr.Status = domain.PairingPending
	}
	_, err := p.db.sql.ExecContext(ctx, `
		INSERT INTO pairings (id, code, status, client_hint, remote_ip, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		pr.ID, pr.Code, string(pr.Status), nullString(pr.ClientHint), nullString(pr.RemoteIP),
		FormatTime(pr.CreatedAt), FormatTime(pr.ExpiresAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("pairing code: %w", domain.ErrConflict)
		}
		return fmt.Errorf("store: create pairing: %w", err)
	}
	return nil
}

// Get returns one pairing by ID.
func (p *Pairings) Get(ctx context.Context, id string) (*domain.Pairing, error) {
	row := p.db.sql.QueryRowContext(ctx, `SELECT `+pairingColumns+` FROM pairings WHERE id = ?`, id)
	pr, err := scanPairing(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get pairing %q: %w", id, err)
	}
	return pr, nil
}

// GetByCode returns one pairing by its six-digit code.
func (p *Pairings) GetByCode(ctx context.Context, code string) (*domain.Pairing, error) {
	row := p.db.sql.QueryRowContext(ctx, `SELECT `+pairingColumns+` FROM pairings WHERE code = ?`, code)
	pr, err := scanPairing(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get pairing by code: %w", err)
	}
	return pr, nil
}

// List returns pairings newest first.
func (p *Pairings) List(ctx context.Context, limit int) ([]domain.Pairing, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := p.db.sql.QueryContext(ctx,
		`SELECT `+pairingColumns+` FROM pairings ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list pairings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Pairing
	for rows.Next() {
		pr, err := scanPairing(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan pairing: %w", err)
		}
		out = append(out, *pr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate pairings: %w", err)
	}
	return out, nil
}

// Approve binds a screen to a pending pairing.
func (p *Pairings) Approve(ctx context.Context, id, screenID string, now time.Time) error {
	res, err := p.db.sql.ExecContext(ctx,
		`UPDATE pairings SET status = 'approved', approved_screen_id = ?, approved_at = ?
		 WHERE id = ? AND status = 'pending'`, screenID, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: approve pairing %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// Claim marks an approved pairing as claimed exactly once.
func (p *Pairings) Claim(ctx context.Context, id string, now time.Time) error {
	res, err := p.db.sql.ExecContext(ctx,
		`UPDATE pairings SET status = 'claimed', claimed_at = ? WHERE id = ? AND status = 'approved'`,
		FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: claim pairing %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetStatus forces a pairing status (expiry, rejection).
func (p *Pairings) SetStatus(ctx context.Context, id string, status domain.PairingStatus) error {
	res, err := p.db.sql.ExecContext(ctx, `UPDATE pairings SET status = ? WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("store: set pairing status %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// Delete removes a pairing row.
func (p *Pairings) Delete(ctx context.Context, id string) error {
	res, err := p.db.sql.ExecContext(ctx, `DELETE FROM pairings WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete pairing %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// DeleteForScreen removes the pairing history of a screen (revocation).
func (p *Pairings) DeleteForScreen(ctx context.Context, screenID string) error {
	_, err := p.db.sql.ExecContext(ctx, `DELETE FROM pairings WHERE approved_screen_id = ?`, screenID)
	if err != nil {
		return fmt.Errorf("store: delete pairings for screen %q: %w", screenID, err)
	}
	return nil
}

// ExpireOverdue marks pending or approved pairings past their TTL as expired.
func (p *Pairings) ExpireOverdue(ctx context.Context, now time.Time) (int64, error) {
	res, err := p.db.sql.ExecContext(ctx,
		`UPDATE pairings SET status = 'expired' WHERE status IN ('pending','approved') AND expires_at <= ?`,
		FormatTime(now))
	if err != nil {
		return 0, fmt.Errorf("store: expire pairings: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// DeleteOlderThan applies the one-day pairing retention rule.
func (p *Pairings) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := p.db.sql.ExecContext(ctx, `DELETE FROM pairings WHERE created_at < ?`, FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("store: prune pairings: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// CodeExists reports whether a code is currently in use by a live pairing.
func (p *Pairings) CodeExists(ctx context.Context, code string) (bool, error) {
	var n int
	err := p.db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pairings WHERE code = ?`, code).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: check pairing code: %w", err)
	}
	return n > 0, nil
}

func scanPairing(sc scanner) (*domain.Pairing, error) {
	var (
		pr                           domain.Pairing
		status, createdAt, expiresAt string
		clientHint, remoteIP         sql.NullString
		approvedScreen               sql.NullString
		approvedAt, claimedAt        sql.NullString
	)
	if err := sc.Scan(&pr.ID, &pr.Code, &status, &clientHint, &remoteIP, &createdAt, &expiresAt,
		&approvedScreen, &approvedAt, &claimedAt); err != nil {
		return nil, err
	}
	pr.Status = domain.PairingStatus(status)
	pr.ClientHint = clientHint.String
	pr.RemoteIP = remoteIP.String
	pr.CreatedAt = timeVal(createdAt)
	pr.ExpiresAt = timeVal(expiresAt)
	pr.ApprovedScreenID = approvedScreen.String
	pr.ApprovedAt = timePtr(approvedAt)
	pr.ClaimedAt = timePtr(claimedAt)
	return &pr, nil
}
