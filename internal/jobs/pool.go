package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
)

// Handler executes one job. Returning nil marks it done; returning an error
// retries it with backoff unless the error is wrapped with Permanent or
// Defer.
type Handler interface {
	Handle(ctx context.Context, job *domain.Job) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, job *domain.Job) error

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, job *domain.Job) error { return f(ctx, job) }

// permanentError fails a job immediately, without further attempts.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks an error as not worth retrying.
func Permanent(err error) error { return permanentError{err: err} }

// IsPermanent reports whether the error must not be retried.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// deferError re-queues a job at a fixed time without counting an attempt. It
// expresses "the preconditions are not met yet" (NAS offline, disk full),
// which must not consume the retry budget of a perfectly good photo.
type deferError struct {
	after time.Duration
	err   error
}

func (e deferError) Error() string { return e.err.Error() }
func (e deferError) Unwrap() error { return e.err }

// Defer postpones a job by d without counting an attempt.
func Defer(d time.Duration, err error) error { return deferError{after: d, err: err} }

// PoolOptions configures the worker pool.
type PoolOptions struct {
	DB      *store.DB
	Logger  *slog.Logger
	Workers int
	// JobTimeout bounds a single job execution (media.decode_timeout).
	JobTimeout time.Duration
	// PollInterval is how often an idle worker looks for new work.
	PollInterval time.Duration
	// StaleLockAfter recovers jobs whose worker vanished.
	StaleLockAfter time.Duration
	Now            func() time.Time
}

// Pool runs handlers against the persistent queue.
type Pool struct {
	opts     PoolOptions
	handlers map[domain.JobKind]Handler
	mu       sync.RWMutex
	// idle is closed and recreated to wake workers as soon as work arrives.
	wake chan struct{}
}

// NewPool builds a pool. Handlers are registered before Run.
func NewPool(opts PoolOptions) *Pool {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Workers <= 0 {
		opts.Workers = 2
	}
	if opts.JobTimeout <= 0 {
		opts.JobTimeout = 30 * time.Second
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.StaleLockAfter <= 0 {
		opts.StaleLockAfter = 15 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Pool{opts: opts, handlers: map[domain.JobKind]Handler{}, wake: make(chan struct{}, 1)}
}

// Register attaches a handler to a job kind.
func (p *Pool) Register(kind domain.JobKind, h Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[kind] = h
}

// Wake asks an idle worker to poll immediately.
func (p *Pool) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Recover returns jobs left locked by a previous process to the queue. It runs
// once at startup (design §4.2 step 5 and §4.3).
func (p *Pool) Recover(ctx context.Context) (int64, error) {
	now := p.opts.Now()
	n, err := p.opts.DB.Jobs().ReleaseStale(ctx, now.Add(-p.opts.StaleLockAfter), now)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		p.opts.Logger.Info("recovered stale jobs", "component", "jobs",
			"event", "stale_locks_released", "count", n)
	}
	return n, nil
}

// Run starts the workers and blocks until ctx is cancelled. Jobs still running
// at that point are released back to the queue by the caller's shutdown path.
func (p *Pool) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < p.opts.Workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			p.worker(ctx, fmt.Sprintf("worker-%d", n))
		}(i)
	}
	wg.Wait()
}

// worker polls for work. A panic inside a handler is recovered, the job is
// retried with backoff and the worker restarts after a short delay rather
// than taking the process down (design §4.1).
func (p *Pool) worker(ctx context.Context, name string) {
	ticker := time.NewTicker(p.opts.PollInterval)
	defer ticker.Stop()
	for {
		worked := p.step(ctx, name)
		if ctx.Err() != nil {
			return
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-p.wake:
		}
	}
}

// step claims and runs at most one job. It reports whether work was done.
func (p *Pool) step(ctx context.Context, name string) (worked bool) {
	defer func() {
		if rec := recover(); rec != nil {
			p.opts.Logger.Error("job worker panicked", "component", "jobs", "event", "panic",
				"worker", name, "panic", rec, "stack", string(debug.Stack()))
			// Back off briefly so a reproducible panic cannot spin the CPU.
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			worked = false
		}
	}()

	job, err := p.opts.DB.Jobs().Claim(ctx, name, p.opts.Now())
	if errors.Is(err, domain.ErrNotFound) {
		return false
	}
	if err != nil {
		if ctx.Err() != nil {
			return false
		}
		// Contention with the indexer is expected while a large share is being
		// imported: the claim already retried with backoff, so a remaining
		// busy result is a "try again next tick", not an operational fault.
		if store.IsBusy(err) {
			p.opts.Logger.Debug("job claim contended", "component", "jobs",
				"event", "claim_busy")
			return false
		}
		p.opts.Logger.Warn("job claim failed", "component", "jobs",
			"event", "claim_failed", "error", err.Error())
		return false
	}
	p.execute(ctx, job)
	return true
}

// execute runs one claimed job under its deadline and records the outcome.
func (p *Pool) execute(ctx context.Context, job *domain.Job) {
	p.mu.RLock()
	handler, ok := p.handlers[job.Kind]
	p.mu.RUnlock()
	if !ok {
		p.finishFailed(job, fmt.Errorf("jobs: no handler for kind %q", job.Kind))
		return
	}

	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.opts.JobTimeout)
	defer cancel()
	err := handler.Handle(runCtx, job)
	p.finish(job, err)
}

// finish applies the retry policy. It deliberately uses a background context:
// the job outcome must be recorded even when the server is shutting down.
func (p *Pool) finish(job *domain.Job, err error) {
	if err == nil {
		p.complete(job)
		return
	}
	var deferred deferError
	if errors.As(err, &deferred) {
		p.deferJob(job, deferred)
		return
	}
	if IsPermanent(err) || job.Attempts >= MaxAttempts {
		p.finishFailed(job, err)
		return
	}
	p.retry(job, err)
}

func (p *Pool) writeCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (p *Pool) complete(job *domain.Job) {
	ctx, cancel := p.writeCtx()
	defer cancel()
	if err := p.opts.DB.Jobs().Complete(ctx, job.ID, p.opts.Now()); err != nil {
		p.opts.Logger.Warn("job completion not recorded", "component", "jobs",
			"event", "complete_failed", "job_id", job.ID, "error", err.Error())
	}
}

func (p *Pool) deferJob(job *domain.Job, d deferError) {
	ctx, cancel := p.writeCtx()
	defer cancel()
	now := p.opts.Now()
	// A deferred job did not really attempt anything, so its attempt counter
	// is rolled back; otherwise an offline NAS would exhaust every retry.
	if err := p.opts.DB.Jobs().Reschedule(ctx, job.ID, now.Add(d.after), job.Attempts-1, d.Error(), now); err != nil {
		p.opts.Logger.Warn("job deferral not recorded", "component", "jobs",
			"event", "defer_failed", "job_id", job.ID, "error", err.Error())
	}
}

func (p *Pool) retry(job *domain.Job, cause error) {
	ctx, cancel := p.writeCtx()
	defer cancel()
	now := p.opts.Now()
	next := now.Add(Backoff(job.Attempts))
	p.opts.Logger.Info("job retry scheduled", "component", "jobs", "event", "job_retry",
		"kind", string(job.Kind), "job_id", job.ID, "attempts", job.Attempts, "code", errCode(cause))
	if err := p.opts.DB.Jobs().Retry(ctx, job.ID, next, errCode(cause), now); err != nil {
		p.opts.Logger.Warn("job retry not recorded", "component", "jobs",
			"event", "retry_failed", "job_id", job.ID, "error", err.Error())
	}
}

func (p *Pool) finishFailed(job *domain.Job, cause error) {
	ctx, cancel := p.writeCtx()
	defer cancel()
	p.opts.Logger.Warn("job failed", "component", "jobs", "event", "job_failed",
		"kind", string(job.Kind), "job_id", job.ID, "attempts", job.Attempts, "code", errCode(cause))
	if err := p.opts.DB.Jobs().Fail(ctx, job.ID, errCode(cause), p.opts.Now()); err != nil {
		p.opts.Logger.Warn("job failure not recorded", "component", "jobs",
			"event", "fail_failed", "job_id", job.ID, "error", err.Error())
	}
}

// errCode renders an error for the last_error column without leaking a path.
func errCode(err error) string {
	if err == nil {
		return ""
	}
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return err.Error()
}

// IsDeferred reports whether the error asks for a re-queue without counting an
// attempt.
func IsDeferred(err error) bool {
	var d deferError
	return errors.As(err, &d)
}
