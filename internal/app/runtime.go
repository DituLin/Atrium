package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/auth/tlsgen"
	"github.com/DituLin/Atrium/internal/backup"
	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/diag"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/indexer"
	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/DituLin/Atrium/internal/media"
	"github.com/DituLin/Atrium/internal/screen"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/version"
	"github.com/DituLin/Atrium/internal/webui"
	"github.com/DituLin/Atrium/internal/widget"
	"github.com/DituLin/Atrium/internal/ws"
)

// ShutdownTimeout is the graceful stop budget from design §4.1.
const ShutdownTimeout = 10 * time.Second

// Options configures a Runtime.
type Options struct {
	Config *config.Config
	Logger *slog.Logger
	// Listener overrides the network listener; tests pass a random port.
	Listener net.Listener
	// Now injects a clock for tests.
	Now func() time.Time
	// Errors is the diagnostics ring the caller already wired into Logger.
	Errors *diag.Errors
}

// Runtime owns the server, the store and every background goroutine.
type Runtime struct {
	cfg     *config.Config
	log     *slog.Logger
	layout  Layout
	db      *store.DB
	api     *httpapi.API
	server  *httpapi.Server
	weather *widget.WeatherService

	// V0.3 and H1 subsystems.
	hub      *ws.Hub
	commands *screen.Service
	diag     *diag.Aggregator
	backups  *backup.Service
	lock     *backup.Lock

	// V0.2 subsystems.
	bus      *events.Bus
	sources  *source.Manager
	queue    *jobs.Queue
	pool     *jobs.Pool
	cache    *media.Cache
	pipeline *media.Pipeline
	index    *indexer.Scheduler

	// mu guards listener, which Start writes and Addr reads concurrently.
	mu       sync.Mutex
	listener net.Listener

	started  time.Time
	now      func() time.Time
	insecure bool
	stopBg   context.CancelFunc
	bgDone   chan struct{}
	serveErr chan error
}

// New builds a runtime: it opens the data directory and the database, applies
// migrations and performs the startup reconciliation of design §4.2.
func New(ctx context.Context, opts Options) (*Runtime, error) {
	cfg := opts.Config
	if cfg == nil {
		return nil, errors.New("app: config is required")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	layout := NewLayout(cfg.Storage.DataDir)
	if err := layout.Ensure(); err != nil {
		return nil, err
	}
	// The advisory lock proves a single writer per data directory and is what
	// `atrium restore` checks before replacing the database (design §6.10).
	lock, err := backup.Acquire(layout.Root)
	if err != nil {
		return nil, err
	}
	db, err := store.Open(ctx, layout.DB())
	if err != nil {
		_ = lock.Release()
		return nil, err
	}
	applied, err := db.Migrate(ctx)
	if err != nil {
		_ = db.Close()
		_ = lock.Release()
		return nil, err
	}

	home, err := clock.NewHome(cfg.Home.Timezone, clockFunc(now))
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	r := &Runtime{
		cfg:      cfg,
		log:      log,
		layout:   layout,
		db:       db,
		started:  now(),
		now:      now,
		insecure: cfg.Server.TLS.Mode == config.TLSOff,
		listener: opts.Listener,
		bgDone:   make(chan struct{}),
		lock:     lock,
	}
	if err := r.startupReconcile(ctx, applied); err != nil {
		_ = db.Close()
		return nil, err
	}

	r.weather = widget.NewWeatherService(cfg.Widgets.Weather, db, nil)
	if err := r.buildPipeline(ctx, home); err != nil {
		_ = db.Close()
		return nil, err
	}
	authenticator := auth.NewAuthenticator(db, auth.NewOriginPolicy(cfg.DefaultAllowedOrigins()), now)
	r.buildRealtime(authenticator)
	r.buildOps(opts.Errors)
	ui, err := r.uiHandler()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	snapshot := widget.NewComposer(cfg, db, home, r.weather).
		WithIndex(r.indexWidget).
		WithSourceStats(r.sourceStats)
	r.api = httpapi.New(httpapi.Deps{
		Config:    cfg,
		DB:        db,
		Logger:    log,
		Auth:      authenticator,
		Pairing:   auth.NewPairingService(db, now),
		Home:      home,
		Snapshot:  snapshot,
		Now:       now,
		Insecure:  r.insecure,
		StartedAt: r.started,
		UI:        ui,
		Sources:   r.sources,
		Index:     r.index,
		Queue:     r.queue,
		Cache:     r.cache,
		Media:     r.pipeline,
		Bus:       r.bus,

		Commands:     r.commands,
		Sessions:     r.hub,
		WS:           r.hub,
		Diagnostics:  r.diag,
		Backup:       r.backups,
		Admin:        auth.NewAdminService(db),
		TokenFileDir: layout.Root,
	})
	r.server = httpapi.NewServer(cfg.Server.Listen, r.api.Handler(), log)
	return r, nil
}

// DB exposes the store, used by CLI commands that share a runtime.
func (r *Runtime) DB() *store.DB { return r.db }

// Handler exposes the routed HTTP handler for tests.
func (r *Runtime) Handler() http.Handler { return r.api.Handler() }

// Layout exposes the data directory layout.
func (r *Runtime) Layout() Layout { return r.layout }

func (r *Runtime) uiHandler() (http.Handler, error) {
	dist, err := webui.Sub(webui.Dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("app: open embedded UI: %w", err)
	}
	return webui.New(webui.Options{FS: dist}), nil
}

// startupReconcile applies steps 4 to 6 of design §4.2.
func (r *Runtime) startupReconcile(ctx context.Context, migrations int) error {
	now := r.now()

	revoked, err := widget.ReconcileSources(ctx, r.db, r.cfg, now)
	if err != nil {
		return err
	}
	for _, id := range revoked {
		r.log.Warn("source revoked", "component", "app", "event", "source_removed_from_config", "source_id", id)
	}

	unknown, err := r.db.Commands().MarkAllAcceptedUnknown(ctx, "server_restart", now)
	if err != nil {
		return err
	}
	requeued, err := r.db.Jobs().ReleaseRunning(ctx, now)
	if err != nil {
		return err
	}

	// A timezone change invalidates every captured_day; the recompute job runs
	// with the indexer in V0.2, so the setting is updated and the job queued.
	settings := r.db.Settings()
	previous, err := settings.GetDefault(ctx, store.SettingHomeTimezone, "")
	if err != nil {
		return err
	}
	if previous != r.cfg.Home.Timezone {
		if previous != "" {
			r.log.Info("home timezone changed", "component", "app",
				"event", "timezone_changed", "from", previous, "to", r.cfg.Home.Timezone)
			if err := r.queueRecomputeDay(ctx, now); err != nil {
				return err
			}
		}
		if err := settings.Set(ctx, store.SettingHomeTimezone, r.cfg.Home.Timezone); err != nil {
			return err
		}
	}

	r.log.Info("startup",
		"component", "app", "event", "startup",
		"version", version.String(),
		"listen", r.cfg.Server.Listen,
		"tls_mode", string(r.cfg.Server.TLS.Mode),
		"migrations_applied", migrations,
		"sources", len(r.cfg.Sources),
		"commands_marked_unknown", unknown,
		"jobs_requeued", requeued,
	)
	if r.insecure {
		r.log.Warn("running without TLS", "component", "app", "event", "insecure_mode")
	}
	return nil
}

func (r *Runtime) queueRecomputeDay(ctx context.Context, now time.Time) error {
	job := &domain.Job{Kind: domain.JobRecomputeDay, Priority: 1, NextRunAt: now, CreatedAt: now}
	err := r.db.Jobs().Enqueue(ctx, job)
	if errors.Is(err, domain.ErrConflict) {
		return nil
	}
	return err
}

// tlsConfig builds the TLS configuration for the configured mode.
func (r *Runtime) tlsConfig() (*tls.Config, error) {
	switch r.cfg.Server.TLS.Mode {
	case config.TLSOff:
		return nil, nil
	case config.TLSFile:
		cert, err := tls.LoadX509KeyPair(r.cfg.Server.TLS.CertFile, r.cfg.Server.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("app: load TLS key pair: %w", err)
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
	default:
		res, err := tlsgen.Ensure(tlsgen.Params{
			Dir:       r.layout.TLS(),
			PublicURL: r.cfg.Server.PublicURL,
			ExtraSANs: r.cfg.Server.ExtraSANs,
			Now:       r.now(),
		})
		if err != nil {
			return nil, err
		}
		cert, err := tls.LoadX509KeyPair(res.ServerCertPath, res.ServerKeyPath)
		if err != nil {
			return nil, fmt.Errorf("app: load generated key pair: %w", err)
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
	}
}

func clockFunc(now func() time.Time) clock.Clock { return clockAdapter{now} }

type clockAdapter struct{ now func() time.Time }

func (c clockAdapter) Now() time.Time { return c.now() }
