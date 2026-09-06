// Package jobs is the persistent background work queue. It stores work in
// SQLite so a restart never loses a pending preview, and runs it in a bounded
// worker pool with per-job deadlines (technical design §4.1, §6.3, task B-205).
package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
)

// Job priorities from design §6.2: metadata before previews, so a photo
// becomes describable before it becomes displayable.
const (
	PriorityExtractMeta  = 10
	PriorityBuildPreview = 5
	PriorityMaintenance  = 1
)

// MaxAttempts is the number of tries before a job is failed permanently.
const MaxAttempts = 5

// backoff is the retry schedule from design §6.3. Attempt n waits
// backoff[n-1]; the last entry repeats until MaxAttempts is reached.
var backoff = []time.Duration{
	time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	6 * time.Hour,
}

// Backoff returns the delay before the given attempt number is retried.
func Backoff(attempts int) time.Duration {
	if attempts <= 0 {
		return backoff[0]
	}
	if attempts > len(backoff) {
		return backoff[len(backoff)-1]
	}
	return backoff[attempts-1]
}

// Queue enqueues work and hides the deduplication semantics from callers.
type Queue struct {
	db *store.DB
	// tx binds the queue to a caller's batch transaction; nil means
	// auto-commit on the shared pool.
	tx  *sql.Tx
	now func() time.Time
}

// WithTx returns a queue that enqueues inside tx, so a caller batching many
// inserts takes the SQLite write lock once instead of once per job.
func (q *Queue) WithTx(tx *sql.Tx) *Queue {
	return &Queue{db: q.db, tx: tx, now: q.now}
}

// jobs returns the repository bound to the current transaction, if any.
func (q *Queue) jobs() *store.Jobs {
	if q.tx != nil {
		return q.db.Jobs().WithTx(q.tx)
	}
	return q.db.Jobs()
}

// NewQueue builds a queue over the job repository.
func NewQueue(db *store.DB, now func() time.Time) *Queue {
	if now == nil {
		now = time.Now
	}
	return &Queue{db: db, now: now}
}

// Request describes one unit of work.
type Request struct {
	Kind     domain.JobKind
	PhotoID  string
	SourceID string
	Priority int
	// RunAt delays the first attempt; zero means immediately.
	RunAt time.Time
}

// Enqueue adds a job unless an equivalent one is already queued or running.
// A duplicate is not an error: the caller's intent (this work should happen)
// is already satisfied.
func (q *Queue) Enqueue(ctx context.Context, req Request) error {
	if !req.Kind.Valid() {
		return fmt.Errorf("jobs: unknown kind %q", req.Kind)
	}
	repo := q.jobs()
	open, err := repo.HasOpen(ctx, req.Kind, req.PhotoID)
	if err != nil {
		return err
	}
	if open {
		return nil
	}
	now := q.now()
	runAt := req.RunAt
	if runAt.IsZero() {
		runAt = now
	}
	job := &domain.Job{
		Kind: req.Kind, PhotoID: req.PhotoID, SourceID: req.SourceID,
		Status: domain.JobQueued, Priority: req.Priority,
		NextRunAt: runAt, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Enqueue(ctx, job); err != nil {
		// The dedup index is the authority; losing a race is success.
		if errors.Is(err, domain.ErrConflict) {
			return nil
		}
		return err
	}
	return nil
}

// EnqueuePhoto is the common case: work attached to one photo.
func (q *Queue) EnqueuePhoto(ctx context.Context, kind domain.JobKind, photoID, sourceID string, priority int) error {
	return q.Enqueue(ctx, Request{Kind: kind, PhotoID: photoID, SourceID: sourceID, Priority: priority})
}

// Counts returns the number of jobs per status.
func (q *Queue) Counts(ctx context.Context) (map[domain.JobStatus]int64, error) {
	return q.jobs().Counts(ctx)
}
