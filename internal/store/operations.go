package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DituLin/Atrium/internal/domain"
	"time"
)

// Operations records durable principal-scoped idempotency mappings.
type Operations struct{ ex execer }

// Operations returns the durable operation repository.
func (d *DB) Operations() *Operations { return &Operations{ex: d.sql} }

// WithTx binds reads and writes to a caller-owned transaction.
func (r *Operations) WithTx(tx *sql.Tx) *Operations { return &Operations{ex: tx} }

// Get returns the mapped command and canonical request hash.
func (r *Operations) Get(ctx context.Context, principal, operation string) (*domain.Command, string, error) {
	var id, hash string
	err := r.ex.QueryRowContext(ctx, `SELECT command_id,payload_hash FROM integration_operations WHERE principal_id=? AND operation_id=?`, principal, operation).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", domain.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	cmd, err := scanCommand(r.ex.QueryRowContext(ctx, `SELECT `+commandColumns+` FROM screen_commands WHERE id=?`, id))
	return cmd, hash, err
}

// GetCommand returns a command only when its operation belongs to this principal.
func (r *Operations) GetCommand(ctx context.Context, principal, id string) (*domain.Command, error) {
	cmd, err := scanCommand(r.ex.QueryRowContext(ctx, `SELECT `+commandColumns+` FROM screen_commands WHERE id=? AND EXISTS(SELECT 1 FROM integration_operations WHERE principal_id=? AND command_id=screen_commands.id)`, id, principal))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return cmd, err
}

// Insert records a mapping atomically with IssueInTx when bound with WithTx.
func (r *Operations) Insert(ctx context.Context, principal, operation, hash, command string, now time.Time) error {
	_, err := r.ex.ExecContext(ctx, `INSERT INTO integration_operations(principal_id,operation_id,payload_hash,command_id,created_at) VALUES(?,?,?,?,?)`, principal, operation, hash, command, FormatTime(now))
	return err
}
