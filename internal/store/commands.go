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

// Commands is the screen_commands repository.
type Commands struct{ db *DB }

// Commands returns the command repository.
func (d *DB) Commands() *Commands { return &Commands{db: d} }

const commandColumns = `id, screen_id, sequence, kind, payload, issued_by, issued_at, expires_at,
	status, delivered_at, resolved_at, error_code, result`

// Issue allocates the next sequence for the screen and stores the command in
// one transaction (design §6.5).
func (c *Commands) Issue(ctx context.Context, cmd *domain.Command) error {
	if cmd.ID == "" {
		cmd.ID = domain.NewID()
	}
	payload, err := json.Marshal(cmd.Payload)
	if err != nil {
		return fmt.Errorf("store: encode command payload: %w", err)
	}
	return c.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		seq, err := c.db.Screens().NextSequence(ctx, tx, cmd.ScreenID)
		if err != nil {
			return err
		}
		cmd.Sequence = seq
		result, err := marshalResult(cmd.Result)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO screen_commands (`+commandColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			cmd.ID, cmd.ScreenID, cmd.Sequence, string(cmd.Kind), string(payload), cmd.IssuedBy,
			FormatTime(cmd.IssuedAt), FormatTime(cmd.ExpiresAt), string(cmd.Status),
			FormatTimePtr(cmd.DeliveredAt), FormatTimePtr(cmd.ResolvedAt),
			nullString(cmd.ErrorCode), result); err != nil {
			return fmt.Errorf("store: insert command: %w", err)
		}
		return nil
	})
}

// Get returns one command by ID.
func (c *Commands) Get(ctx context.Context, id string) (*domain.Command, error) {
	row := c.db.sql.QueryRowContext(ctx, `SELECT `+commandColumns+` FROM screen_commands WHERE id = ?`, id)
	cmd, err := scanCommand(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get command %q: %w", id, err)
	}
	return cmd, nil
}

// List returns recent commands, optionally filtered by screen and status.
func (c *Commands) List(ctx context.Context, screenID string, status domain.CommandStatus, limit int) ([]domain.Command, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + commandColumns + ` FROM screen_commands WHERE 1 = 1`
	var args []any
	if screenID != "" {
		q += ` AND screen_id = ?`
		args = append(args, screenID)
	}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, string(status))
	}
	q += ` ORDER BY issued_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list commands: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Command
	for rows.Next() {
		cmd, err := scanCommand(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan command: %w", err)
		}
		out = append(out, *cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate commands: %w", err)
	}
	return out, nil
}

// MarkDelivered records the delivery timestamp.
func (c *Commands) MarkDelivered(ctx context.Context, id string, now time.Time) error {
	res, err := c.db.sql.ExecContext(ctx,
		`UPDATE screen_commands SET delivered_at = ? WHERE id = ? AND delivered_at IS NULL`,
		FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: mark command delivered %q: %w", id, err)
	}
	_, _ = res.RowsAffected()
	return nil
}

// Resolve sets a terminal status exactly once; later calls are no-ops.
func (c *Commands) Resolve(ctx context.Context, id string, status domain.CommandStatus, errCode string, result *domain.CommandResult, now time.Time) (bool, error) {
	blob, err := marshalResult(result)
	if err != nil {
		return false, err
	}
	res, err := c.db.sql.ExecContext(ctx, `
		UPDATE screen_commands SET status = ?, error_code = ?, result = COALESCE(?, result), resolved_at = ?
		WHERE id = ? AND status = 'accepted'`,
		string(status), nullString(errCode), blob, FormatTime(now), id)
	if err != nil {
		return false, fmt.Errorf("store: resolve command %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: rows affected: %w", err)
	}
	return n > 0, nil
}

// MarkAllAcceptedUnknown resolves every open command at startup (design §4.2).
func (c *Commands) MarkAllAcceptedUnknown(ctx context.Context, errCode string, now time.Time) (int64, error) {
	res, err := c.db.sql.ExecContext(ctx,
		`UPDATE screen_commands SET status = 'unknown', error_code = ?, resolved_at = ? WHERE status = 'accepted'`,
		errCode, FormatTime(now))
	if err != nil {
		return 0, fmt.Errorf("store: mark commands unknown: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// OpenBefore returns accepted commands whose expiry has passed.
func (c *Commands) OpenBefore(ctx context.Context, cutoff time.Time) ([]domain.Command, error) {
	rows, err := c.db.sql.QueryContext(ctx,
		`SELECT `+commandColumns+` FROM screen_commands WHERE status = 'accepted' AND expires_at <= ? ORDER BY expires_at`,
		FormatTime(cutoff))
	if err != nil {
		return nil, fmt.Errorf("store: list open commands: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Command
	for rows.Next() {
		cmd, err := scanCommand(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan command: %w", err)
		}
		out = append(out, *cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate open commands: %w", err)
	}
	return out, nil
}

// CountOpen returns the number of accepted commands for a screen.
func (c *Commands) CountOpen(ctx context.Context, screenID string) (int64, error) {
	var n int64
	err := c.db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM screen_commands WHERE screen_id = ? AND status = 'accepted'`, screenID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count open commands: %w", err)
	}
	return n, nil
}

func marshalResult(r *domain.CommandResult) (sql.NullString, error) {
	if r == nil {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("store: encode command result: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func scanCommand(sc scanner) (*domain.Command, error) {
	var (
		cmd                          domain.Command
		kind, status                 string
		payload, issuedAt, expiresAt string
		deliveredAt, resolvedAt      sql.NullString
		errorCode, result            sql.NullString
	)
	if err := sc.Scan(&cmd.ID, &cmd.ScreenID, &cmd.Sequence, &kind, &payload, &cmd.IssuedBy,
		&issuedAt, &expiresAt, &status, &deliveredAt, &resolvedAt, &errorCode, &result); err != nil {
		return nil, err
	}
	cmd.Kind = domain.CommandKind(kind)
	cmd.Status = domain.CommandStatus(status)
	cmd.IssuedAt = timeVal(issuedAt)
	cmd.ExpiresAt = timeVal(expiresAt)
	cmd.DeliveredAt = timePtr(deliveredAt)
	cmd.ResolvedAt = timePtr(resolvedAt)
	cmd.ErrorCode = errorCode.String
	if payload != "" {
		if err := json.Unmarshal([]byte(payload), &cmd.Payload); err != nil {
			return nil, fmt.Errorf("store: decode command payload: %w", err)
		}
	}
	if result.Valid && result.String != "" {
		var r domain.CommandResult
		if err := json.Unmarshal([]byte(result.String), &r); err == nil {
			cmd.Result = &r
		}
	}
	return &cmd, nil
}

// NewestPending returns the newest still-open command for a screen, which is
// what session.ready redelivers on reconnect (design §9). A command whose TTL
// has passed is never redelivered: the expirer owns it.
func (c *Commands) NewestPending(ctx context.Context, screenID string, now time.Time) (*domain.Command, error) {
	row := c.db.sql.QueryRowContext(ctx, `SELECT `+commandColumns+` FROM screen_commands
		WHERE screen_id = ? AND status = 'accepted' AND expires_at > ?
		ORDER BY sequence DESC LIMIT 1`, screenID, FormatTime(now))
	cmd, err := scanCommand(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: newest pending command: %w", err)
	}
	return cmd, nil
}

// ListUnknownMissingObserved returns commands stuck at unknown that have not
// yet been annotated with what the screen reports, newest first.
func (c *Commands) ListUnknownMissingObserved(ctx context.Context, screenID string, limit int) ([]domain.Command, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := c.db.sql.QueryContext(ctx, `SELECT `+commandColumns+` FROM screen_commands
		WHERE screen_id = ? AND status = 'unknown'
		  AND (result IS NULL OR result NOT LIKE '%"observed"%')
		ORDER BY sequence DESC LIMIT ?`, screenID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list unknown commands: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Command
	for rows.Next() {
		cmd, err := scanCommand(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan command: %w", err)
		}
		out = append(out, *cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate unknown commands: %w", err)
	}
	return out, nil
}

// SetResult replaces the result document of a command without touching its
// status: an unknown command is never flipped automatically (design §6.5).
func (c *Commands) SetResult(ctx context.Context, id string, result *domain.CommandResult) error {
	blob, err := marshalResult(result)
	if err != nil {
		return err
	}
	if _, err := c.db.sql.ExecContext(ctx,
		`UPDATE screen_commands SET result = ? WHERE id = ?`, blob, id); err != nil {
		return fmt.Errorf("store: set command result %q: %w", id, err)
	}
	return nil
}

// DeleteOlderThan removes resolved commands past the retention window.
func (c *Commands) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := c.db.sql.ExecContext(ctx,
		`DELETE FROM screen_commands WHERE issued_at < ?`, FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("store: prune commands: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// CountByStatusSince counts commands issued since a cutoff, grouped by status.
func (c *Commands) CountByStatusSince(ctx context.Context, since time.Time) (map[domain.CommandStatus]int64, error) {
	rows, err := c.db.sql.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM screen_commands WHERE issued_at >= ? GROUP BY status`,
		FormatTime(since))
	if err != nil {
		return nil, fmt.Errorf("store: count commands: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[domain.CommandStatus]int64, 5)
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("store: scan command counts: %w", err)
		}
		out[domain.CommandStatus(status)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate command counts: %w", err)
	}
	return out, nil
}
