package video

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"os"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
)

// Processor consumes fresh authorized descriptors for each bounded operation.
type Processor interface {
	Probe(context.Context, *os.File) (domain.VideoMetadata, error)
	Cover(context.Context, *os.File) ([]byte, error)
}

// ErrStale means the work no longer matches its file, source or authorization.
var ErrStale = errors.New("video: source or task changed")

// WorkerOptions configures a serial video processor with a dedicated cache.
type WorkerOptions struct {
	DB        *store.DB
	Sources   *source.Manager
	Processor Processor
	Cache     *CoverCache
	Now       func() time.Time
	Timeout   time.Duration
}

// Worker holds its slot until actual work exits, even after a caller timeout.
type Worker struct {
	opts WorkerOptions
	slot chan struct{}
}

// NewWorker constructs a worker; Run starts its cancellable polling loop.
func NewWorker(opts WorkerOptions) (*Worker, error) {
	if opts.DB == nil || opts.Sources == nil || opts.Processor == nil || opts.Cache == nil {
		return nil, errors.New("video: incomplete worker options")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 45 * time.Second
	}
	if opts.Timeout > 45*time.Second {
		return nil, errors.New("video: worker timeout exceeds lease margin")
	}
	return &Worker{opts: opts, slot: make(chan struct{}, 1)}, nil
}

// Run processes due work serially and backs off when idle, blocked or failed.
func (w *Worker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		worked, err := w.RunOnce(ctx)
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// RunOnce returns whether it claimed work. Timeouts do not free a slot whose
// kernel I/O is still pending, preventing repeated calls from accumulating it.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	select {
	case w.slot <- struct{}{}:
	default:
		return false, ErrToolBusy
	}
	jobCtx, cancel := context.WithTimeout(ctx, w.opts.Timeout)
	type result struct {
		worked bool
		err    error
	}
	done := make(chan result, 1)
	go func() {
		var outcome result
		defer func() { <-w.slot; done <- outcome }()
		err := w.reconcileCovers(jobCtx)
		if err != nil {
			outcome = result{err: err}
			return
		}
		task, err := w.opts.DB.VideoWork().Claim(jobCtx, w.opts.Now(), time.Minute)
		if err != nil || task == nil {
			outcome = result{err: err}
			return
		}
		err = w.process(jobCtx, *task)
		if err != nil {
			retryCtx, retryCancel := context.WithTimeout(context.WithoutCancel(jobCtx), time.Second)
			if errors.Is(err, ErrMetadata) {
				// Recheck the actual file after a failed probe before making its state
				// terminal; an intervening file replacement must not inherit the error.
				if _, checkErr := w.validate(retryCtx, *task); checkErr == nil {
					_, _ = w.opts.DB.VideoWork().FailUnsupported(retryCtx, *task, w.opts.Now())
				} else {
					_, _ = w.opts.DB.VideoWork().Retry(retryCtx, *task, failureCode(checkErr), w.opts.Now().Add(retryDelay(task.Attempts)), w.opts.Now())
				}
			} else {
				_, _ = w.opts.DB.VideoWork().Retry(retryCtx, *task, failureCode(err), w.opts.Now().Add(retryDelay(task.Attempts)), w.opts.Now())
			}
			retryCancel()
		}
		outcome = result{worked: true, err: err}
	}()
	defer cancel()
	select {
	case r := <-done:
		return r.worked, r.err
	case <-jobCtx.Done():
		return true, jobCtx.Err()
	}
}

func failureCode(err error) string {
	switch {
	case errors.Is(err, ErrStale):
		return "stale"
	case errors.Is(err, ErrCacheFull):
		return "cache_full"
	case errors.Is(err, ErrLowDisk):
		return "low_disk"
	case errors.Is(err, ErrToolUnavailable):
		return "tool_unavailable"
	case errors.Is(err, ErrMetadata):
		return "unsupported"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "processing_failed"
	}
}

func (w *Worker) validate(ctx context.Context, t store.VideoTask) (*source.Entry, error) {
	ok, err := w.opts.DB.VideoWork().Valid(ctx, t, w.opts.Now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrStale
	}
	entry := w.opts.Sources.Get(t.Video.SourceID)
	if entry == nil || !entry.Online() || entry.IdentityMismatch() || entry.Config.Root != t.Source.Source.RootPath || !entry.Extensions()[t.Video.Ext] {
		return nil, ErrStale
	}
	cfg := entry.Config.Identity
	probe := source.CheckIdentity(ctx, entry.FS, source.IdentityConfig{RequireMount: cfg.RequireMount, AllowLocal: cfg.AllowLocal, MarkerFile: cfg.MarkerFile})
	if !probe.OK() || probe.Identity == nil || t.Source.Source.IdentityBound == nil || !probe.Identity.Equal(*t.Source.Source.IdentityBound) {
		return nil, ErrStale
	}
	info, err := entry.FS.Stat(ctx, t.Video.RelPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != t.Video.SizeBytes || info.ModTime().Unix() != t.Video.MtimeUnix {
		return nil, ErrStale
	}
	return entry, nil
}

func openVideo(ctx context.Context, entry *source.Entry, t store.VideoTask) (*os.File, error) {
	reader, err := entry.FS.Open(ctx, t.Video.RelPath)
	if err != nil {
		return nil, err
	}
	f, ok := reader.(*os.File)
	if !ok {
		_ = reader.Close()
		return nil, ErrToolUnavailable
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != t.Video.SizeBytes || info.ModTime().Unix() != t.Video.MtimeUnix {
		_ = f.Close()
		return nil, ErrStale
	}
	return f, nil
}

func (w *Worker) process(ctx context.Context, t store.VideoTask) error {
	entry, err := w.validate(ctx, t)
	if err != nil {
		return err
	}
	f, err := openVideo(ctx, entry, t)
	if err != nil {
		return err
	}
	meta, err := w.opts.Processor.Probe(ctx, f)
	_ = f.Close()
	if err != nil {
		return err
	}
	entry, err = w.validate(ctx, t)
	if err != nil {
		return err
	}
	f, err = openVideo(ctx, entry, t)
	if err != nil {
		return err
	}
	cover, err := w.opts.Processor.Cover(ctx, f)
	_ = f.Close()
	if err != nil {
		return err
	}
	if _, err = w.validate(ctx, t); err != nil {
		return err
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(cover))
	if err != nil {
		return ErrMetadata
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 640 || cfg.Height > 640 {
		return ErrMetadata
	}
	if err = w.opts.Cache.Write(t.Token, cover); err != nil {
		return err
	}
	published, err := w.opts.DB.VideoWork().Publish(ctx, t, meta, int64(len(cover)), cfg.Width, cfg.Height, w.opts.Now())
	if err != nil || !published {
		_ = w.opts.Cache.Remove(t.Token)
		if err != nil {
			return err
		}
		return ErrStale
	}
	return nil
}

// reconcileCovers repairs local cache loss without reauthorizing media. The
// eventual claim and processing still pass all source/version checks.
func (w *Worker) reconcileCovers(ctx context.Context) error {
	var retained map[string]bool
	err := w.opts.Cache.Sweep(func() (map[string]bool, error) {
		var err error
		retained, err = w.opts.DB.VideoWork().RetainedTokens(ctx, w.opts.Now())
		return retained, err
	})
	if err != nil {
		return err
	}
	for token := range retained {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.opts.Cache.IfMissing(token, func() error {
			_, err := w.opts.DB.VideoWork().ForgetCover(ctx, token, w.opts.Now())
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// Retry delays grow to one hour; unavailable tools or offline storage are not
// evidence that the file itself is unsupported.
func retryDelay(attempt int) time.Duration {
	return min(time.Hour, 2*time.Minute*time.Duration(1<<min(5, max(0, attempt-1))))
}
