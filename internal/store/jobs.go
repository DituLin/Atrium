package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// Jobs is the persistent job queue repository.
type Jobs struct {
	db *DB
	// ex is the database or the caller's batch transaction.
	ex execer
}

// Jobs returns the job repository.
func (d *DB) Jobs() *Jobs { return &Jobs{db: d, ex: d.sql} }

const jobColumns = `id, kind, photo_id, source_id, status, priority, attempts, next_run_at,
	locked_by, locked_at, last_error, created_at, updated_at`

// Enqueue inserts a job. A duplicate open job for the same (kind, photo_id)
// is rejected by the dedup index and reported as domain.ErrConflict.
func (j *Jobs) Enqueue(ctx context.Context, job *domain.Job) error {
	if job.ID == "" {
		job.ID = domain.NewID()
	}
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	if job.NextRunAt.IsZero() {
		job.NextRunAt = now
	}
	if job.Status == "" {
		job.Status = domain.JobQueued
	}
	_, err := j.ex.ExecContext(ctx, `
		INSERT INTO jobs (`+jobColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, string(job.Kind), nullString(job.PhotoID), nullString(job.SourceID), string(job.Status),
		job.Priority, job.Attempts, FormatTime(job.NextRunAt),
		nullString(job.LockedBy), FormatTimePtr(job.LockedAt), nullString(job.LastError),
		FormatTime(job.CreatedAt), FormatTime(job.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("job %s/%s: %w", job.Kind, job.PhotoID, domain.ErrConflict)
		}
		return fmt.Errorf("store: enqueue job: %w", err)
	}
	return nil
}

// Get returns one job by ID.
func (j *Jobs) Get(ctx context.Context, id string) (*domain.Job, error) {
	row := j.ex.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get job %q: %w", id, err)
	}
	return job, nil
}

// Claim locks the next runnable job for worker and returns it, or
// domain.ErrNotFound when the queue has nothing due.
func (j *Jobs) Claim(ctx context.Context, worker string, now time.Time) (*domain.Job, error) {
	var claimed *domain.Job
	err := j.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs
			WHERE status = 'queued' AND next_run_at <= ?
			ORDER BY priority DESC, next_run_at ASC LIMIT 1`, FormatTime(now))
		job, err := scanJob(row)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE jobs SET status = 'running', locked_by = ?, locked_at = ?, attempts = attempts + 1, updated_at = ? WHERE id = ?`,
			worker, FormatTime(now), FormatTime(now), job.ID); err != nil {
			return fmt.Errorf("store: lock job: %w", err)
		}
		job.Status = domain.JobRunning
		job.LockedBy = worker
		job.LockedAt = &now
		job.Attempts++
		claimed = job
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// Complete marks a job done.
func (j *Jobs) Complete(ctx context.Context, id string, now time.Time) error {
	res, err := j.ex.ExecContext(ctx,
		`UPDATE jobs SET status = 'done', locked_by = NULL, locked_at = NULL, last_error = NULL, updated_at = ? WHERE id = ?`,
		FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: complete job %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// Retry returns a job to the queue with a later run time.
func (j *Jobs) Retry(ctx context.Context, id string, nextRun time.Time, lastErr string, now time.Time) error {
	res, err := j.ex.ExecContext(ctx,
		`UPDATE jobs SET status = 'queued', locked_by = NULL, locked_at = NULL, next_run_at = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		FormatTime(nextRun), nullString(lastErr), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: retry job %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// Fail marks a job permanently failed.
func (j *Jobs) Fail(ctx context.Context, id, lastErr string, now time.Time) error {
	res, err := j.ex.ExecContext(ctx,
		`UPDATE jobs SET status = 'failed', locked_by = NULL, locked_at = NULL, last_error = ?, updated_at = ? WHERE id = ?`,
		nullString(lastErr), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: fail job %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// ReleaseRunning returns every running job to the queue. It runs at startup so
// that jobs locked by a crashed process are picked up again (design §4.3).
func (j *Jobs) ReleaseRunning(ctx context.Context, now time.Time) (int64, error) {
	res, err := j.ex.ExecContext(ctx,
		`UPDATE jobs SET status = 'queued', locked_by = NULL, locked_at = NULL, updated_at = ? WHERE status = 'running'`,
		FormatTime(now))
	if err != nil {
		return 0, fmt.Errorf("store: release running jobs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// Counts returns the number of jobs per status.
func (j *Jobs) Counts(ctx context.Context) (map[domain.JobStatus]int64, error) {
	rows, err := j.ex.QueryContext(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("store: job counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[domain.JobStatus]int64)
	for rows.Next() {
		var s string
		var n int64
		if err := rows.Scan(&s, &n); err != nil {
			return nil, fmt.Errorf("store: scan job counts: %w", err)
		}
		out[domain.JobStatus(s)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate job counts: %w", err)
	}
	return out, nil
}

// OldestQueuedAt returns the creation time of the oldest queued job.
func (j *Jobs) OldestQueuedAt(ctx context.Context) (*time.Time, error) {
	var s sql.NullString
	err := j.ex.QueryRowContext(ctx, `SELECT MIN(created_at) FROM jobs WHERE status = 'queued'`).Scan(&s)
	if err != nil {
		return nil, fmt.Errorf("store: oldest queued job: %w", err)
	}
	return timePtr(s), nil
}

func scanJob(sc scanner) (*domain.Job, error) {
	var (
		job                         domain.Job
		photoID, sourceID, lockedBy sql.NullString
		lockedAt, lastErr           sql.NullString
		kind, status                string
		nextRun, createdAt, updated string
	)
	if err := sc.Scan(&job.ID, &kind, &photoID, &sourceID, &status, &job.Priority, &job.Attempts,
		&nextRun, &lockedBy, &lockedAt, &lastErr, &createdAt, &updated); err != nil {
		return nil, err
	}
	job.Kind = domain.JobKind(kind)
	job.Status = domain.JobStatus(status)
	job.PhotoID = photoID.String
	job.SourceID = sourceID.String
	job.LockedBy = lockedBy.String
	job.LockedAt = timePtr(lockedAt)
	job.LastError = lastErr.String
	job.NextRunAt = timeVal(nextRun)
	job.CreatedAt = timeVal(createdAt)
	job.UpdatedAt = timeVal(updated)
	return &job, nil
}

// HasOpen reports whether a queued or running job already exists for the
// (kind, photo_id) pair. The partial unique index only covers rows with a
// photo, so jobs without one need this explicit check to stay deduplicated.
func (j *Jobs) HasOpen(ctx context.Context, kind domain.JobKind, photoID string) (bool, error) {
	q := `SELECT EXISTS(SELECT 1 FROM jobs WHERE kind = ? AND status IN ('queued','running') AND `
	args := []any{string(kind)}
	if photoID == "" {
		q += `photo_id IS NULL)`
	} else {
		q += `photo_id = ?)`
		args = append(args, photoID)
	}
	var exists int
	if err := j.ex.QueryRowContext(ctx, q, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("store: check open job: %w", err)
	}
	return exists != 0, nil
}

// ReleaseStale returns jobs locked before cutoff to the queue. It recovers
// work whose worker died without releasing the lock.
func (j *Jobs) ReleaseStale(ctx context.Context, cutoff, now time.Time) (int64, error) {
	res, err := j.ex.ExecContext(ctx, `
		UPDATE jobs SET status = 'queued', locked_by = NULL, locked_at = NULL, updated_at = ?
		WHERE status = 'running' AND (locked_at IS NULL OR locked_at < ?)`,
		FormatTime(now), FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("store: release stale jobs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// DeleteTerminalBefore prunes done and failed jobs older than cutoff.
func (j *Jobs) DeleteTerminalBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := j.ex.ExecContext(ctx,
		`DELETE FROM jobs WHERE status IN ('done','failed') AND updated_at < ?`, FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("store: prune jobs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// Reschedule returns a job to the queue at a specific time and rewrites its
// attempt counter. It expresses "not now" (an offline source, a full disk)
// without consuming the retry budget of an otherwise healthy job.
func (j *Jobs) Reschedule(ctx context.Context, id string, nextRun time.Time, attempts int, note string, now time.Time) error {
	if attempts < 0 {
		attempts = 0
	}
	res, err := j.ex.ExecContext(ctx, `
		UPDATE jobs SET status = 'queued', locked_by = NULL, locked_at = NULL,
			next_run_at = ?, attempts = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		FormatTime(nextRun), attempts, nullString(note), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: reschedule job %q: %w", id, err)
	}
	return affectedOne(res, id)
}
