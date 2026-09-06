package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// WidgetCache is the widget_cache repository.
type WidgetCache struct{ db *DB }

// WidgetCache returns the widget cache repository.
func (d *DB) WidgetCache() *WidgetCache { return &WidgetCache{db: d} }

// CachedWidget is one stored widget payload.
type CachedWidget struct {
	Widget    domain.WidgetType
	Payload   string
	FetchedAt time.Time
	ExpiresAt time.Time
	Error     string
}

// Put upserts a widget payload.
func (w *WidgetCache) Put(ctx context.Context, c CachedWidget) error {
	_, err := w.db.sql.ExecContext(ctx, `
		INSERT INTO widget_cache (widget, payload, fetched_at, expires_at, error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(widget) DO UPDATE SET
			payload = excluded.payload, fetched_at = excluded.fetched_at,
			expires_at = excluded.expires_at, error = excluded.error`,
		string(c.Widget), c.Payload, FormatTime(c.FetchedAt), FormatTime(c.ExpiresAt), nullString(c.Error))
	if err != nil {
		return fmt.Errorf("store: put widget cache %q: %w", c.Widget, err)
	}
	return nil
}

// SetError records a refresh failure while keeping the last payload.
func (w *WidgetCache) SetError(ctx context.Context, widget domain.WidgetType, message string) error {
	_, err := w.db.sql.ExecContext(ctx, `UPDATE widget_cache SET error = ? WHERE widget = ?`,
		nullString(message), string(widget))
	if err != nil {
		return fmt.Errorf("store: set widget error %q: %w", widget, err)
	}
	return nil
}

// Get returns the cached widget payload or domain.ErrNotFound.
func (w *WidgetCache) Get(ctx context.Context, widget domain.WidgetType) (*CachedWidget, error) {
	row := w.db.sql.QueryRowContext(ctx,
		`SELECT widget, payload, fetched_at, expires_at, error FROM widget_cache WHERE widget = ?`, string(widget))
	var (
		c                    CachedWidget
		name                 string
		fetchedAt, expiresAt string
		errText              sql.NullString
	)
	err := row.Scan(&name, &c.Payload, &fetchedAt, &expiresAt, &errText)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get widget cache %q: %w", widget, err)
	}
	c.Widget = domain.WidgetType(name)
	c.FetchedAt = timeVal(fetchedAt)
	c.ExpiresAt = timeVal(expiresAt)
	c.Error = errText.String
	return &c, nil
}

// Audit is the audit_log repository.
type Audit struct{ db *DB }

// Audit returns the audit log repository.
func (d *DB) Audit() *Audit { return &Audit{db: d} }

// Append records an administrative action.
func (a *Audit) Append(ctx context.Context, e *domain.AuditEntry) error {
	if e.ID == "" {
		e.ID = domain.NewID()
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	_, err := a.db.sql.ExecContext(ctx,
		`INSERT INTO audit_log (id, at, actor, action, target, detail) VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, FormatTime(e.At), e.Actor, e.Action, nullString(e.Target), nullString(e.Detail))
	if err != nil {
		return fmt.Errorf("store: append audit entry: %w", err)
	}
	return nil
}

// List returns audit entries newest first.
func (a *Audit) List(ctx context.Context, limit int) ([]domain.AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := a.db.sql.QueryContext(ctx,
		`SELECT id, at, actor, action, target, detail FROM audit_log ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audit log: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.AuditEntry
	for rows.Next() {
		var (
			e              domain.AuditEntry
			at             string
			target, detail sql.NullString
		)
		if err := rows.Scan(&e.ID, &at, &e.Actor, &e.Action, &target, &detail); err != nil {
			return nil, fmt.Errorf("store: scan audit entry: %w", err)
		}
		e.At = timeVal(at)
		e.Target = target.String
		e.Detail = detail.String
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate audit log: %w", err)
	}
	return out, nil
}

// DeleteOlderThan applies the seven-day audit retention rule.
func (a *Audit) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := a.db.sql.ExecContext(ctx, `DELETE FROM audit_log WHERE at < ?`, FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("store: prune audit log: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}
