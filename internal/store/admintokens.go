package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// AdminTokens is the admin_tokens repository.
type AdminTokens struct{ db *DB }

// AdminTokens returns the admin token repository.
func (d *DB) AdminTokens() *AdminTokens { return &AdminTokens{db: d} }

const adminTokenColumns = //nolint:gosec // a column list, not a credential
`id, token_hash, label, created_at, revoked_at, last_used_at`

// Create stores a new admin credential hash.
func (a *AdminTokens) Create(ctx context.Context, t *domain.AdminToken) error {
	if t.ID == "" {
		t.ID = domain.NewID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	_, err := a.db.sql.ExecContext(ctx,
		`INSERT INTO admin_tokens (id, token_hash, label, created_at) VALUES (?, ?, ?, ?)`,
		t.ID, t.TokenHash, nullString(t.Label), FormatTime(t.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("admin token: %w", domain.ErrConflict)
		}
		return fmt.Errorf("store: create admin token: %w", err)
	}
	return nil
}

// GetByHash resolves a credential hash to its record.
func (a *AdminTokens) GetByHash(ctx context.Context, hash string) (*domain.AdminToken, error) {
	row := a.db.sql.QueryRowContext(ctx, `SELECT `+adminTokenColumns+` FROM admin_tokens WHERE token_hash = ?`, hash)
	t, err := scanAdminToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get admin token: %w", err)
	}
	return t, nil
}

// List returns all admin tokens, newest first.
func (a *AdminTokens) List(ctx context.Context) ([]domain.AdminToken, error) {
	rows, err := a.db.sql.QueryContext(ctx, `SELECT `+adminTokenColumns+` FROM admin_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: list admin tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.AdminToken
	for rows.Next() {
		t, err := scanAdminToken(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan admin token: %w", err)
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate admin tokens: %w", err)
	}
	return out, nil
}

// Revoke marks one token unusable.
func (a *AdminTokens) Revoke(ctx context.Context, id string, now time.Time) error {
	res, err := a.db.sql.ExecContext(ctx, `UPDATE admin_tokens SET revoked_at = ? WHERE id = ?`, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: revoke admin token %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// RevokeAll marks every live token unusable; used by offline token reset.
func (a *AdminTokens) RevokeAll(ctx context.Context, now time.Time) (int64, error) {
	res, err := a.db.sql.ExecContext(ctx,
		`UPDATE admin_tokens SET revoked_at = ? WHERE revoked_at IS NULL`, FormatTime(now))
	if err != nil {
		return 0, fmt.Errorf("store: revoke all admin tokens: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// TouchUsed records the last successful use of a token.
func (a *AdminTokens) TouchUsed(ctx context.Context, id string, now time.Time) error {
	_, err := a.db.sql.ExecContext(ctx, `UPDATE admin_tokens SET last_used_at = ? WHERE id = ?`, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: touch admin token %q: %w", id, err)
	}
	return nil
}

// CountLive returns the number of tokens that are not revoked.
func (a *AdminTokens) CountLive(ctx context.Context) (int64, error) {
	var n int64
	if err := a.db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM admin_tokens WHERE revoked_at IS NULL`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count admin tokens: %w", err)
	}
	return n, nil
}

func scanAdminToken(sc scanner) (*domain.AdminToken, error) {
	var (
		t                   domain.AdminToken
		label               sql.NullString
		createdAt           string
		revokedAt, lastUsed sql.NullString
	)
	if err := sc.Scan(&t.ID, &t.TokenHash, &label, &createdAt, &revokedAt, &lastUsed); err != nil {
		return nil, err
	}
	t.Label = label.String
	t.CreatedAt = timeVal(createdAt)
	t.RevokedAt = timePtr(revokedAt)
	t.LastUsedAt = timePtr(lastUsed)
	return &t, nil
}
