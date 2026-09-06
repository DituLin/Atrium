package app

import (
	"context"
	"time"

	"github.com/DituLin/Atritum/internal/app/events"
	"github.com/DituLin/Atritum/internal/clock"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/indexer"
	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/media"
	"github.com/DituLin/Atritum/internal/source"
	"github.com/DituLin/Atritum/internal/widget"
)

// buildPipeline constructs the V0.2 subsystems: the source registry, the job
// queue and its workers, the media pipeline and the scan scheduler. They are
// wired here rather than in each other's constructors so the dependency
// direction stays one-way and every piece is testable on its own.
func (r *Runtime) buildPipeline(ctx context.Context, home *clock.Home) error {
	r.bus = events.NewBus()

	sources, err := source.NewManager(source.ManagerOptions{
		Config: r.cfg, DB: r.db, Logger: r.log, Bus: r.bus, Now: r.now,
	})
	if err != nil {
		return err
	}
	if err := sources.Load(ctx); err != nil {
		return err
	}
	r.sources = sources

	r.queue = jobs.NewQueue(r.db, r.now)
	r.cache = media.NewCache(r.layout.Cache())
	r.pool = jobs.NewPool(jobs.PoolOptions{
		DB: r.db, Logger: r.log, Workers: r.cfg.Media.Workers,
		JobTimeout: r.cfg.Media.DecodeTimeout.D(), Now: r.now,
	})

	pipeline, err := media.NewPipeline(media.Options{
		DB: r.db, Cache: r.cache, Queue: r.queue, Sources: r.sources,
		Home: home, Bus: r.bus, Logger: r.log,
		Media: r.cfg.Media, Storage: r.cfg.Storage, Now: r.now,
	})
	if err != nil {
		return err
	}
	r.pipeline = pipeline
	pipeline.Register(r.pool)

	r.index = indexer.NewScheduler(indexer.SchedulerOptions{
		Sources: r.sources, DB: r.db, Queue: r.queue, Cache: r.cache,
		Home: home, Bus: r.bus, Logger: r.log, Now: r.now,
	})
	if _, err := r.pool.Recover(ctx); err != nil {
		return err
	}
	return nil
}

// indexWidget adapts the scheduler's progress to the home snapshot shape.
func (r *Runtime) indexWidget() widget.Index {
	if r.index == nil {
		return widget.Index{State: string(domain.IndexIdle)}
	}
	p := r.index.Aggregate()
	out := widget.Index{
		State:    string(p.State),
		Progress: widget.IndexProgress{Seen: p.Seen, Indexed: p.Indexed},
	}
	if p.LastScanAt != nil {
		v := p.LastScanAt.UTC().Format(time.RFC3339)
		out.LastScanAt = &v
	}
	return out
}

// sourceStats adapts per-source I/O counters for the admin NAS view.
func (r *Runtime) sourceStats(id string) widget.SourceStats {
	if r.sources == nil {
		return widget.SourceStats{IdentityConfirmed: true}
	}
	entry := r.sources.Get(id)
	if entry == nil {
		return widget.SourceStats{IdentityConfirmed: true}
	}
	return widget.SourceStats{
		StuckOps:          entry.Stats().StuckOps,
		IdentityConfirmed: !entry.IdentityMismatch(),
	}
}

// Bus exposes the change bus so the V0.3 hub can subscribe.
func (r *Runtime) Bus() *events.Bus { return r.bus }
