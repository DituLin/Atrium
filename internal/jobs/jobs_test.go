package jobs_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
)

func TestBackoffSchedule(t *testing.T) {
	assert.Equal(t, time.Minute, jobs.Backoff(1))
	assert.Equal(t, 5*time.Minute, jobs.Backoff(2))
	assert.Equal(t, 30*time.Minute, jobs.Backoff(3))
	assert.Equal(t, 6*time.Hour, jobs.Backoff(4))
	assert.Equal(t, 6*time.Hour, jobs.Backoff(9))
	assert.Equal(t, time.Minute, jobs.Backoff(0))
}

func TestQueueDeduplicatesOpenWork(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	q := jobs.NewQueue(db, time.Now)

	require.NoError(t, q.EnqueuePhoto(ctx, domain.JobExtractMeta, "photo1", "src", jobs.PriorityExtractMeta))
	require.NoError(t, q.EnqueuePhoto(ctx, domain.JobExtractMeta, "photo1", "src", jobs.PriorityExtractMeta))
	// A different kind for the same photo is a different unit of work.
	require.NoError(t, q.EnqueuePhoto(ctx, domain.JobBuildPreview, "photo1", "src", jobs.PriorityBuildPreview))
	// Jobs without a photo are deduplicated explicitly, the partial index
	// cannot cover them.
	require.NoError(t, q.Enqueue(ctx, jobs.Request{Kind: domain.JobRecomputeDay}))
	require.NoError(t, q.Enqueue(ctx, jobs.Request{Kind: domain.JobRecomputeDay}))

	counts, err := q.Counts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, counts[domain.JobQueued])
}

func TestQueueRejectsUnknownKind(t *testing.T) {
	db := testutil.NewDB(t)
	err := jobs.NewQueue(db, time.Now).Enqueue(context.Background(), jobs.Request{Kind: "nope"})
	assert.Error(t, err)
}

// runPool starts a pool and stops it once fn's condition is satisfied.
func runPool(t *testing.T, p *jobs.Pool, until func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	assert.Eventually(t, until, 5*time.Second, 5*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pool did not stop")
	}
}

func newPool(db *store.DB, workers int) *jobs.Pool {
	return jobs.NewPool(jobs.PoolOptions{
		DB: db, Workers: workers, JobTimeout: time.Second,
		PollInterval: 5 * time.Millisecond, Logger: quiet(),
	})
}

func TestPoolRunsHandlerAndCompletesJob(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	require.NoError(t, jobs.NewQueue(db, time.Now).EnqueuePhoto(ctx, domain.JobExtractMeta, "p1", "s", 10))

	var seen atomic.Int64
	p := newPool(db, 2)
	p.Register(domain.JobExtractMeta, jobs.HandlerFunc(func(_ context.Context, job *domain.Job) error {
		assert.Equal(t, "p1", job.PhotoID)
		seen.Add(1)
		return nil
	}))
	runPool(t, p, func() bool { return seen.Load() == 1 })

	counts, err := db.Jobs().Counts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, counts[domain.JobDone])
}

func TestPoolRetriesWithBackoffThenFailsPermanently(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	q := jobs.NewQueue(db, time.Now)
	require.NoError(t, q.EnqueuePhoto(ctx, domain.JobBuildPreview, "p1", "s", 5))

	var calls atomic.Int64
	p := newPool(db, 1)
	p.Register(domain.JobBuildPreview, jobs.HandlerFunc(func(context.Context, *domain.Job) error {
		calls.Add(1)
		return errors.New("decode_failed")
	}))
	runPool(t, p, func() bool { return calls.Load() >= 1 })

	rows, err := db.Jobs().Counts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, rows[domain.JobQueued], "a retryable failure returns to the queue")

	// The retry is scheduled a minute out, so it is not picked up again now.
	_, err = db.Jobs().Claim(ctx, "probe", time.Now())
	assert.ErrorIs(t, err, domain.ErrNotFound)
	_, err = db.Jobs().Claim(ctx, "probe", time.Now().Add(2*time.Minute))
	assert.NoError(t, err)
}

func TestPoolPermanentErrorSkipsRetries(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	require.NoError(t, jobs.NewQueue(db, time.Now).EnqueuePhoto(ctx, domain.JobBuildPreview, "p1", "s", 5))

	var calls atomic.Int64
	p := newPool(db, 1)
	p.Register(domain.JobBuildPreview, jobs.HandlerFunc(func(context.Context, *domain.Job) error {
		calls.Add(1)
		return jobs.Permanent(errors.New("unsupported"))
	}))
	runPool(t, p, func() bool { return calls.Load() >= 1 })

	counts, err := db.Jobs().Counts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, counts[domain.JobFailed])
	assert.EqualValues(t, 1, calls.Load())
}

func TestPoolDeferDoesNotConsumeAttempts(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	require.NoError(t, jobs.NewQueue(db, time.Now).EnqueuePhoto(ctx, domain.JobBuildPreview, "p1", "s", 5))

	var calls atomic.Int64
	p := newPool(db, 1)
	p.Register(domain.JobBuildPreview, jobs.HandlerFunc(func(context.Context, *domain.Job) error {
		calls.Add(1)
		return jobs.Defer(2*time.Minute, errors.New("source_offline"))
	}))
	runPool(t, p, func() bool { return calls.Load() >= 1 })

	job, err := db.Jobs().Claim(ctx, "probe", time.Now().Add(3*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, 1, job.Attempts, "the deferred run must not count against the retry budget")
}

func TestPoolRecoversFromHandlerPanic(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	require.NoError(t, jobs.NewQueue(db, time.Now).EnqueuePhoto(ctx, domain.JobExtractMeta, "p1", "s", 10))

	var calls atomic.Int64
	p := newPool(db, 1)
	p.Register(domain.JobExtractMeta, jobs.HandlerFunc(func(context.Context, *domain.Job) error {
		calls.Add(1)
		panic("boom")
	}))
	runPool(t, p, func() bool { return calls.Load() >= 1 })
	// The worker survived; the job stays locked and is recovered on restart.
	n, err := db.Jobs().ReleaseRunning(ctx, time.Now())
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}

func TestRecoverReleasesStaleLocks(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	require.NoError(t, jobs.NewQueue(db, time.Now).EnqueuePhoto(ctx, domain.JobExtractMeta, "p1", "s", 10))
	_, err := db.Jobs().Claim(ctx, "dead-worker", time.Now())
	require.NoError(t, err)
	// Backdate the lock as if the worker died an hour ago.
	_, err = db.SQL().ExecContext(ctx, `UPDATE jobs SET locked_at = ?`,
		store.FormatTime(time.Now().Add(-time.Hour)))
	require.NoError(t, err)

	p := jobs.NewPool(jobs.PoolOptions{DB: db, Logger: quiet(), StaleLockAfter: time.Minute})
	n, err := p.Recover(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	counts, err := db.Jobs().Counts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, counts[domain.JobQueued])
}

func TestPoolJobTimeoutCancelsHandler(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	require.NoError(t, jobs.NewQueue(db, time.Now).EnqueuePhoto(ctx, domain.JobBuildPreview, "p1", "s", 5))

	deadlineHit := make(chan struct{}, 1)
	p := jobs.NewPool(jobs.PoolOptions{
		DB: db, Workers: 1, JobTimeout: 30 * time.Millisecond,
		PollInterval: 5 * time.Millisecond, Logger: quiet(),
	})
	p.Register(domain.JobBuildPreview, jobs.HandlerFunc(func(c context.Context, _ *domain.Job) error {
		<-c.Done()
		deadlineHit <- struct{}{}
		return c.Err()
	}))
	runPool(t, p, func() bool {
		select {
		case <-deadlineHit:
			return true
		default:
			return false
		}
	})
}
