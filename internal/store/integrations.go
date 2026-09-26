package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/domain"
	"time"
)

// Integrations stores principals independently from replaceable credentials.
type Integrations struct {
	db *DB
	ex execer
}

// Integrations returns the integration repository.
func (d *DB) Integrations() *Integrations { return &Integrations{db: d, ex: d.sql} }

// WithTx binds reads to a caller-owned transaction.
func (r *Integrations) WithTx(tx *sql.Tx) *Integrations { return &Integrations{db: r.db, ex: tx} }

const integrationColumns = `id,label,enabled,revoked_at,expires_at,policy_json,policy_version,created_at,updated_at`

func scanIntegration(s scanner) (*domain.IntegrationPrincipal, error) {
	var p domain.IntegrationPrincipal
	var revoked sql.NullString
	var expires, policy, created, updated string
	if err := s.Scan(&p.ID, &p.Label, &p.Enabled, &revoked, &expires, &policy, &p.PolicyVersion, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(policy), &p.Policy); err != nil {
		return nil, err
	}
	if err := p.Policy.Validate(); err != nil {
		return nil, err
	}
	p.RevokedAt = timePtr(revoked)
	p.ExpiresAt = timeVal(expires)
	p.CreatedAt = timeVal(created)
	p.UpdatedAt = timeVal(updated)
	return &p, nil
}

// Get looks up a stable principal ID.
func (r *Integrations) Get(ctx context.Context, id string) (*domain.IntegrationPrincipal, error) {
	return scanIntegration(r.ex.QueryRowContext(ctx, `SELECT `+integrationColumns+` FROM integration_principals WHERE id=?`, id))
}

// List returns principals without credential secrets.
func (r *Integrations) List(ctx context.Context) ([]domain.IntegrationPrincipal, error) {
	rows, err := r.ex.QueryContext(ctx, `SELECT `+integrationColumns+` FROM integration_principals ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []domain.IntegrationPrincipal{}
	for rows.Next() {
		p, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetByTokenHash returns credential and current principal state for authentication.
func (r *Integrations) GetByTokenHash(ctx context.Context, hash string) (*domain.IntegrationPrincipal, *domain.IntegrationCredential, error) {
	var c domain.IntegrationCredential
	var created, expires string
	var revoked sql.NullString
	err := r.ex.QueryRowContext(ctx, `SELECT id,principal_id,token_hash,created_at,expires_at,revoked_at FROM integration_credentials WHERE token_hash=?`, hash).Scan(&c.ID, &c.PrincipalID, &c.TokenHash, &created, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	c.CreatedAt = timeVal(created)
	c.ExpiresAt = timeVal(expires)
	c.RevokedAt = timePtr(revoked)
	p, err := r.Get(ctx, c.PrincipalID)
	return p, &c, err
}
func (r *Integrations) validateReferences(ctx context.Context, p domain.IntegrationPolicy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	for _, list := range []struct {
		ids   []string
		table string
	}{{p.SourceIDs, "data_sources"}, {p.ScreenIDs, "screens"}} {
		for _, id := range list.ids {
			var n int
			err := r.ex.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+list.table+` WHERE id=? AND status='active'`, id).Scan(&n)
			if err != nil {
				return err
			}
			if n != 1 {
				return domain.Errorf(domain.CodeInvalidRequest, "policy references an unknown or revoked resource")
			}
		}
	}
	return nil
}

// Create atomically inserts a principal and its first credential.
func (r *Integrations) Create(ctx context.Context, p *domain.IntegrationPrincipal, c *domain.IntegrationCredential) error {
	return r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		repo := r.WithTx(tx)
		if err := repo.validateReferences(ctx, p.Policy); err != nil {
			return err
		}
		blob, err := json.Marshal(p.Policy)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO integration_principals (`+integrationColumns+`) VALUES (?,?,?,?,?,?,?,?,?)`, p.ID, p.Label, p.Enabled, FormatTimePtr(p.RevokedAt), FormatTime(p.ExpiresAt), string(blob), p.PolicyVersion, FormatTime(p.CreatedAt), FormatTime(p.UpdatedAt))
		if err != nil {
			return err
		}
		return repo.insertCredential(ctx, c)
	})
}
func (r *Integrations) insertCredential(ctx context.Context, c *domain.IntegrationCredential) error {
	_, err := r.ex.ExecContext(ctx, `INSERT INTO integration_credentials(id,principal_id,token_hash,created_at,expires_at,revoked_at) VALUES(?,?,?,?,?,?)`, c.ID, c.PrincipalID, c.TokenHash, FormatTime(c.CreatedAt), FormatTime(c.ExpiresAt), FormatTimePtr(c.RevokedAt))
	return err
}

// Rotate revokes the old credential and installs its replacement atomically.
func (r *Integrations) Rotate(ctx context.Context, id string, c *domain.IntegrationCredential, now time.Time) error {
	return r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		repo := r.WithTx(tx)
		p, err := repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if !p.Active(now) {
			return domain.Errorf(domain.CodeUnauthorized, "integration is inactive")
		}
		if c.PrincipalID != id || c.ExpiresAt.After(p.ExpiresAt) {
			return domain.Errorf(domain.CodeInvalidRequest, "credential expiry exceeds principal expiry")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE integration_credentials SET revoked_at=? WHERE principal_id=? AND revoked_at IS NULL`, FormatTime(now), id); err != nil {
			return err
		}
		return repo.insertCredential(ctx, c)
	})
}

// Revoke immediately disables the principal and its credentials.
func (r *Integrations) Revoke(ctx context.Context, id string, now time.Time) error {
	return r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE integration_principals SET enabled=0,revoked_at=COALESCE(revoked_at,?),updated_at=? WHERE id=?`, FormatTime(now), FormatTime(now), id)
		if err != nil {
			return err
		}
		if err = affectedOne(res, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE integration_credentials SET revoked_at=COALESCE(revoked_at,?) WHERE principal_id=?`, FormatTime(now), id)
		return err
	})
}

// UpdatePolicy replaces all allowlists and increments the policy version.
func (r *Integrations) UpdatePolicy(ctx context.Context, id string, policy domain.IntegrationPolicy, now time.Time) error {
	return r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		repo := r.WithTx(tx)
		if err := repo.validateReferences(ctx, policy); err != nil {
			return err
		}
		blob, err := json.Marshal(policy)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE integration_principals SET policy_json=?,policy_version=policy_version+1,updated_at=? WHERE id=?`, string(blob), FormatTime(now), id)
		if err != nil {
			return err
		}
		return affectedOne(res, id)
	})
}
