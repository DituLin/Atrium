package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/DituLin/Atrium/internal/media"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
)

// DefaultScanInterval is the cadence from design §10 when a source leaves it
// unset. Scanning is a minute-scale discovery mechanism, not a file watcher.
const DefaultScanInterval = 60 * time.Second

// ScanCooldown is the pause between a scan that overran its interval and the
// next start (design §6.2).
const ScanCooldown = 5 * time.Second

// InitialScanDelay is how long the first scan waits after startup. It is short
// so the baseline import begins promptly, but long enough for the health
// prober to bind the mount identity first: scanning an unidentified root is
// refused, and a refused first attempt would cost a whole interval.
const InitialScanDelay = 3 * time.Second

// ScanRequest is a manual scan asked for through the admin API.
type ScanRequest struct {
	Mode domain.ScanMode
	// Result receives the run that satisfied the request.
	Result chan<- ScanResult
}

// ScanResult reports what happened to a manual request.
type ScanResult struct {
	Run *domain.ScanRun
	// Merged is true when a scan was already running and the request joined it
	// instead of starting a second, overlapping pass.
	Merged bool
	Err    error
}

// Scheduler owns one serial scan loop per source.
type Scheduler struct {
	sources  *source.Manager
	scanners map[string]*Scanner
	log      *slog.Logger

	mu       sync.Mutex
	running  map[string]*domain.ScanRun
	waiters  map[string][]chan<- ScanResult
	pending  map[string]domain.ScanMode
	wake     map[string]chan struct{}
	interval map[string]time.Duration
}

// SchedulerOptions configures a Scheduler.
type SchedulerOptions struct {
	Sources *source.Manager
	DB      *store.DB
	Queue   *jobs.Queue
	Cache   *media.Cache
	Home    *clock.Home
	Bus     *events.Bus
	Logger  *slog.Logger
	Now     func() time.Time
}

// NewScheduler builds a scheduler with one scanner per configured source.
func NewScheduler(opts SchedulerOptions) *Scheduler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &Scheduler{
		sources: opts.Sources, log: opts.Logger,
		scanners: map[string]*Scanner{},
		running:  map[string]*domain.ScanRun{},
		waiters:  map[string][]chan<- ScanResult{},
		pending:  map[string]domain.ScanMode{},
		wake:     map[string]chan struct{}{},
		interval: map[string]time.Duration{},
	}
	for _, entry := range opts.Sources.All() {
		s.scanners[entry.ID] = NewScanner(ScannerOptions{
			Entry: entry, DB: opts.DB, Queue: opts.Queue, Cache: opts.Cache,
			Home: opts.Home, Bus: opts.Bus, Logger: opts.Logger, Now: opts.Now,
		})
		iv := entry.Config.Scan.Interval.D()
		if iv <= 0 {
			iv = DefaultScanInterval
		}
		s.interval[entry.ID] = iv
		s.wake[entry.ID] = make(chan struct{}, 1)
	}
	return s
}

// Progress returns the indexing state of every source.
func (s *Scheduler) Progress() map[string]Progress {
	out := make(map[string]Progress, len(s.scanners))
	for id, sc := range s.scanners {
		out[id] = sc.Progress()
	}
	return out
}

// Aggregate collapses per-source progress into the single home-snapshot view.
// Baseline import wins over a plain scan because it is the state the operator
// actually needs to see.
func (s *Scheduler) Aggregate() Progress {
	agg := Progress{State: domain.IndexIdle}
	for _, p := range s.Progress() {
		agg.Seen += p.Seen
		agg.Indexed += p.Indexed
		switch p.State {
		case domain.IndexBaselineImport:
			agg.State = domain.IndexBaselineImport
		case domain.IndexScanning:
			if agg.State != domain.IndexBaselineImport {
				agg.State = domain.IndexScanning
			}
		default:
		}
		if p.LastScanAt != nil && (agg.LastScanAt == nil || p.LastScanAt.After(*agg.LastScanAt)) {
			agg.LastScanAt = p.LastScanAt
		}
	}
	return agg
}

// Request asks for a manual scan. When a scan is already running the caller is
// attached to it and told the request merged, which is what the API reports
// instead of starting an overlapping pass (design §6.2 step 7).
func (s *Scheduler) Request(ctx context.Context, sourceID string, mode domain.ScanMode) (ScanResult, error) {
	if _, ok := s.scanners[sourceID]; !ok {
		return ScanResult{}, fmt.Errorf("source %q: %w", sourceID, domain.ErrNotFound)
	}
	result := make(chan ScanResult, 1)

	s.mu.Lock()
	if run := s.running[sourceID]; run != nil {
		merged := *run
		s.mu.Unlock()
		return ScanResult{Run: &merged, Merged: true}, nil
	}
	// A full request wins over a plain one already queued.
	if existing, ok := s.pending[sourceID]; !ok || mode == domain.ScanFull || existing != domain.ScanFull {
		s.pending[sourceID] = mode
	}
	s.waiters[sourceID] = append(s.waiters[sourceID], result)
	wake := s.wake[sourceID]
	s.mu.Unlock()

	select {
	case wake <- struct{}{}:
	default:
	}
	select {
	case res := <-result:
		return res, res.Err
	case <-ctx.Done():
		return ScanResult{}, ctx.Err()
	}
}

// RequestAsync triggers a scan without waiting for it to finish.
func (s *Scheduler) RequestAsync(sourceID string, mode domain.ScanMode) bool {
	if _, ok := s.scanners[sourceID]; !ok {
		return false
	}
	s.mu.Lock()
	s.pending[sourceID] = mode
	wake := s.wake[sourceID]
	s.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
	return true
}

// Run starts one loop per source and blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for id := range s.scanners {
		wg.Add(1)
		go func(sourceID string) {
			defer wg.Done()
			s.loop(ctx, sourceID)
		}(id)
	}
	wg.Wait()
}

// loop is the serial scan driver for one source: scans never overlap, and a
// scan that overran its interval is followed by a short cooldown rather than
// starting again immediately.
func (s *Scheduler) loop(ctx context.Context, sourceID string) {
	interval := s.interval[sourceID]
	first := InitialScanDelay
	if interval < first {
		first = interval
	}
	timer := time.NewTimer(first)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-s.wake[sourceID]:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}

		mode := s.takePending(sourceID)
		started := time.Now()
		s.runOnce(ctx, sourceID, mode)
		next := interval
		if elapsed := time.Since(started); elapsed >= interval {
			next = ScanCooldown
		}
		timer.Reset(next)
	}
}

// takePending consumes a manual request, defaulting to a scheduled scan.
func (s *Scheduler) takePending(sourceID string) domain.ScanMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	mode, ok := s.pending[sourceID]
	if !ok {
		return domain.ScanScheduled
	}
	delete(s.pending, sourceID)
	return mode
}

// runOnce performs a single scan, guarding against panics so one bad source
// cannot stop the others.
func (s *Scheduler) runOnce(ctx context.Context, sourceID string, mode domain.ScanMode) {
	defer func() {
		if rec := recover(); rec != nil {
			s.log.Error("scanner panicked", "component", "indexer", "event", "panic",
				"source_id", sourceID, "panic", rec)
			s.finish(sourceID, ScanResult{Err: fmt.Errorf("indexer: scanner panicked")})
		}
	}()

	entry := s.sources.Get(sourceID)
	if entry == nil {
		return
	}
	if !entry.Online() || entry.IdentityMismatch() {
		health, detail := entry.Health()
		s.log.Debug("scan skipped", "component", "indexer", "event", "scan_skipped",
			"source_id", sourceID, "health", string(health), "code", detail)
		s.finish(sourceID, ScanResult{Err: domain.Errorf(domain.CodeSourceOffline,
			"source %s is %s (%s)", sourceID, health, detail)})
		return
	}

	s.mu.Lock()
	s.running[sourceID] = &domain.ScanRun{SourceID: sourceID, Mode: mode, Status: domain.ScanRunning}
	s.mu.Unlock()

	run, err := s.scanners[sourceID].Scan(ctx, mode)

	s.mu.Lock()
	delete(s.running, sourceID)
	s.mu.Unlock()
	s.finish(sourceID, ScanResult{Run: run, Err: err})
}

// finish delivers the outcome to every caller waiting on a manual request.
func (s *Scheduler) finish(sourceID string, res ScanResult) {
	s.mu.Lock()
	waiters := s.waiters[sourceID]
	delete(s.waiters, sourceID)
	s.mu.Unlock()
	for _, w := range waiters {
		select {
		case w <- res:
		default:
		}
	}
}

// ScanNow runs one scan synchronously. It exists for tests and for the startup
// path, where waiting for the loop's first tick would delay the first import.
func (s *Scheduler) ScanNow(ctx context.Context, sourceID string, mode domain.ScanMode) (*domain.ScanRun, error) {
	sc, ok := s.scanners[sourceID]
	if !ok {
		return nil, fmt.Errorf("source %q: %w", sourceID, domain.ErrNotFound)
	}
	s.mu.Lock()
	if s.running[sourceID] != nil {
		run := *s.running[sourceID]
		s.mu.Unlock()
		return &run, nil
	}
	s.running[sourceID] = &domain.ScanRun{SourceID: sourceID, Mode: mode, Status: domain.ScanRunning}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.running, sourceID)
		s.mu.Unlock()
	}()
	run, err := sc.Scan(ctx, mode)
	if err != nil && errors.Is(err, context.Canceled) {
		return run, err
	}
	return run, err
}
