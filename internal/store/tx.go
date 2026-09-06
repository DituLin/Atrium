package store

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// execer abstracts *sql.DB and *sql.Tx so a repository can run either on its
// own connection (auto-commit) or inside a caller's batch transaction. The
// indexer uses the second form to commit a few hundred files at a time instead
// of taking the write lock once per statement.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// busyRetries and busyBackoff bound the retry loop for short writers. SQLite
// serialises writers, so a busy database is normal contention rather than a
// fault; retrying quietly for about a second is what keeps the job pool from
// logging a warning every time the indexer commits a batch.
const (
	busyRetries    = 6
	busyBaseBackof = 15 * time.Millisecond
)

// IsBusy reports whether err is a SQLite contention error: SQLITE_BUSY (5),
// SQLITE_BUSY_SNAPSHOT (517) or SQLITE_LOCKED (6).
func IsBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "(5)") && strings.Contains(msg, "locked") ||
		strings.Contains(msg, "(517)")
}

// RetryBusy runs fn, retrying with jittered backoff while SQLite reports
// contention. It gives up after busyRetries attempts and returns the last
// error so a genuine failure is still surfaced.
func RetryBusy(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < busyRetries; attempt++ {
		err = fn()
		if !IsBusy(err) {
			return err
		}
		//nolint:gosec // jitter needs no cryptographic randomness
		delay := busyBaseBackof<<attempt + time.Duration(rand.Int64N(int64(busyBaseBackof)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}

// InWriteTx runs fn inside a write transaction. The connection DSN sets
// `_txlock=immediate`, so the write lock is taken by BEGIN rather than by the
// first UPDATE; a deferred transaction that reads and then writes is what
// produces SQLITE_BUSY_SNAPSHOT, which no busy timeout can resolve.
// Contention on BEGIN itself is retried with backoff.
func (d *DB) InWriteTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return RetryBusy(ctx, func() error { return d.InTx(ctx, fn) })
}

// WithTx returns a photo repository bound to tx, so a caller can batch many
// files into one commit.
func (p *Photos) WithTx(tx *sql.Tx) *Photos { return &Photos{db: p.db, ex: tx} }

// WithTx returns a job repository bound to tx.
func (j *Jobs) WithTx(tx *sql.Tx) *Jobs { return &Jobs{db: j.db, ex: tx} }

// WithTx returns a preview repository bound to tx.
func (p *Previews) WithTx(tx *sql.Tx) *Previews { return &Previews{db: p.db, ex: tx} }

// BeginWrite starts a write transaction, retrying while SQLite is busy. The
// DSN makes it BEGIN IMMEDIATE, so the write lock is held for the whole
// transaction and no read-then-write upgrade can fail with BUSY_SNAPSHOT.
func (d *DB) BeginWrite(ctx context.Context) (*sql.Tx, error) {
	var tx *sql.Tx
	err := RetryBusy(ctx, func() error {
		var err error
		tx, err = d.sql.BeginTx(ctx, nil)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("store: begin write tx: %w", err)
	}
	return tx, nil
}
