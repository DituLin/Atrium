package indexer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/DituLin/Atrium/internal/media"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
)

// ProgressFlushEvery is how many files pass before scan counters are written
// back to the database (design §6.2 step 8).
const ProgressFlushEvery = 200

// Scanner runs scans for one source. It is used serially by the scheduler, so
// it needs no internal locking beyond the stability set.
type Scanner struct {
	entry     *source.Entry
	db        *store.DB
	queue     *jobs.Queue
	cache     *media.Cache
	home      *clock.Home
	bus       *events.Bus
	log       *slog.Logger
	now       func() time.Time
	stability *StabilitySet
	rules     Rules
	maxRounds int
	// tx is the open index batch, if any; the repository accessors bind to it.
	tx *sql.Tx
	// progress is published to the home snapshot while a scan runs.
	progress Progress
}

// ScannerOptions configures a Scanner.
type ScannerOptions struct {
	Entry  *source.Entry
	DB     *store.DB
	Queue  *jobs.Queue
	Cache  *media.Cache
	Home   *clock.Home
	Bus    *events.Bus
	Logger *slog.Logger
	Now    func() time.Time
}

// NewScanner builds a scanner for one source.
func NewScanner(opts ScannerOptions) *Scanner {
	cfg := opts.Entry.Config.Scan
	checks := cfg.StabilityChecks
	if checks <= 0 {
		checks = 2
	}
	interval := cfg.StabilityInterval.D()
	if interval <= 0 {
		interval = 5 * time.Second
	}
	rounds := cfg.StabilityMaxRound
	if rounds <= 0 {
		rounds = 3
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Scanner{
		entry: opts.Entry, db: opts.DB, queue: opts.Queue, cache: opts.Cache,
		home: opts.Home, bus: opts.Bus, log: log, now: now,
		stability: NewStabilitySet(),
		rules:     Rules{Checks: checks, Interval: interval},
		maxRounds: rounds,
	}
}

// Progress is the live state of indexing for one source.
type Progress struct {
	State      domain.IndexState `json:"state"`
	Seen       int64             `json:"seen"`
	Indexed    int64             `json:"indexed"`
	RunID      string            `json:"-"`
	LastScanAt *time.Time        `json:"-"`
}

// Progress returns a snapshot of the current scan state.
func (s *Scanner) Progress() Progress { return s.progress }

// Scan performs one complete pass. It returns the scan run row so callers can
// report counters; an error means the run was recorded as failed or aborted.
func (s *Scanner) Scan(ctx context.Context, mode domain.ScanMode) (*domain.ScanRun, error) {
	if !s.entry.Online() {
		return nil, fmt.Errorf("indexer: source %s is not online", s.entry.ID)
	}
	if s.entry.IdentityMismatch() {
		// Nothing is scanned and nothing is ever removed while the mount
		// behind the root is not the one that was bound (design §6.1).
		return nil, domain.Errorf(domain.CodeIdentityMismatch,
			"source %s has an unconfirmed mount identity", s.entry.ID)
	}
	if mode == domain.ScanFull {
		s.stability.Reset()
	}

	src, err := s.db.Sources().Get(ctx, s.entry.ID)
	if err != nil {
		return nil, err
	}
	if src.Status != domain.SourceActive {
		return nil, fmt.Errorf("indexer: source %s is revoked", s.entry.ID)
	}

	generation := src.ScanGeneration + 1
	start := s.now()
	run := &domain.ScanRun{SourceID: s.entry.ID, Mode: mode, Status: domain.ScanRunning, StartedAt: start}
	if err := s.db.ScanRuns().Start(ctx, run); err != nil {
		return nil, err
	}
	if err := s.db.Sources().StartScan(ctx, s.entry.ID, start); err != nil {
		return nil, err
	}
	s.setState(src, run.ID)

	err = s.execute(ctx, run, generation, mode)
	if err != nil {
		status := domain.ScanFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, source.ErrStuck) || errors.Is(err, source.ErrDegraded) {
			status = domain.ScanAborted
		}
		run.Status = status
		_ = s.db.ScanRuns().UpdateCounters(ctx, run)
		_ = s.db.ScanRuns().Finish(context.WithoutCancel(ctx), run.ID, status, errCode(err), s.now())
		s.log.Warn("scan did not complete", "component", "indexer", "event", "scan_incomplete",
			"source_id", s.entry.ID, "status", string(status), "code", errCode(err))
		s.clearState()
		// A run that did not complete never advances the generation, so no
		// file can be judged missing because of it (FR-08).
		return run, err
	}

	if err := s.finalize(ctx, run, generation); err != nil {
		return run, err
	}
	s.clearState()
	return run, nil
}

// errCode renders a scan failure for the note column without leaking a path.
func errCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, source.ErrStuck):
		return "stuck_io"
	case errors.Is(err, source.ErrDegraded):
		return "degraded"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, fs.ErrNotExist):
		return "root_missing"
	case errors.Is(err, fs.ErrPermission):
		return "permission_denied"
	default:
		return "scan_error"
	}
}

func (s *Scanner) setState(src *domain.Source, runID string) {
	state := domain.IndexScanning
	if src.BaselineCompleted == nil {
		state = domain.IndexBaselineImport
	}
	s.progress = Progress{State: state, RunID: runID, LastScanAt: src.LastScanCompletedA}
}

func (s *Scanner) clearState() {
	last := s.now()
	s.progress = Progress{State: domain.IndexIdle, Seen: s.progress.Seen, Indexed: s.progress.Indexed, LastScanAt: &last}
}

func (s *Scanner) publish(topics ...domain.Topic) {
	if s.bus != nil {
		s.bus.Publish(topics...)
	}
}
