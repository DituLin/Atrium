package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/DituLin/Atritum/internal/media"
)

// Serve starts the listener and blocks until ctx is cancelled, then shuts down
// gracefully within ShutdownTimeout (design §4.3).
func (r *Runtime) Serve(ctx context.Context) error {
	if err := r.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return r.Shutdown()
}

// ServeWithSignals runs the server until SIGINT or SIGTERM arrives.
func (r *Runtime) ServeWithSignals(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return r.Serve(ctx)
}

// Start binds the listener and serves in the background.
func (r *Runtime) Start(ctx context.Context) error {
	tlsCfg, err := r.tlsConfig()
	if err != nil {
		return err
	}
	ln := r.takeListener()
	if ln == nil {
		var lerr error
		ln, lerr = net.Listen("tcp", r.cfg.Server.Listen)
		if lerr != nil {
			return fmt.Errorf("app: listen on %s: %w", r.cfg.Server.Listen, lerr)
		}
	}
	if tlsCfg != nil {
		ln = tls.NewListener(ln, tlsCfg)
	}
	addr := ln.Addr().String()
	r.setListener(ln)

	bgCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.stopBg = cancel
	go r.runBackground(bgCtx)

	errCh := make(chan error, 1)
	go func() {
		err := r.server.HTTP().Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	r.serveErr = errCh

	r.log.Info("listening",
		"component", "app", "event", "listening",
		"addr", addr,
		"tls", tlsCfg != nil,
		"data_dir_redacted", true,
	)
	return nil
}

// Addr returns the bound address; it is only valid after Start.
func (r *Runtime) Addr() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listener == nil {
		return ""
	}
	return r.listener.Addr().String()
}

// takeListener removes and returns the pre-supplied listener, if any.
func (r *Runtime) takeListener() net.Listener {
	r.mu.Lock()
	defer r.mu.Unlock()
	ln := r.listener
	r.listener = nil
	return ln
}

func (r *Runtime) setListener(ln net.Listener) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listener = ln
}

// Shutdown stops accepting connections, stops the background goroutines,
// checkpoints the WAL and closes the database.
func (r *Runtime) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	var errs []error
	if err := r.server.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("app: http shutdown: %w", err))
	}
	if r.stopBg != nil {
		r.stopBg()
		select {
		case <-r.bgDone:
		case <-ctx.Done():
			errs = append(errs, errors.New("app: background workers did not stop in time"))
		}
	}
	if r.serveErr != nil {
		select {
		case err := <-r.serveErr:
			if err != nil {
				errs = append(errs, fmt.Errorf("app: serve: %w", err))
			}
		case <-time.After(time.Second):
		}
	}
	// Jobs left running belong back in the queue (design §4.3).
	if _, err := r.db.Jobs().ReleaseRunning(ctx, r.now()); err != nil {
		errs = append(errs, err)
	}
	if err := r.db.Checkpoint(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := r.db.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := r.lock.Release(); err != nil {
		errs = append(errs, err)
	}
	r.log.Info("stopped", "component", "app", "event", "shutdown")
	return errors.Join(errs...)
}

// runBackground owns every periodic goroutine: pairing expiry, the optional
// weather refresh, the source health probers, the scan schedulers, the media
// workers and the cache janitor. V0.3 adds the heartbeat monitor and the
// command expirer.
func (r *Runtime) runBackground(ctx context.Context) {
	defer close(r.bgDone)

	var workers sync.WaitGroup
	start := func(name string, fn func(context.Context)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() {
				if rec := recover(); rec != nil {
					r.log.Error("background worker panicked",
						"component", name, "event", "panic", "panic", rec)
				}
			}()
			fn(ctx)
		}()
	}
	if r.sources != nil {
		start("source", r.sources.Run)
	}
	if r.index != nil {
		start("indexer", r.index.Run)
	}
	if r.pool != nil {
		start("jobs", r.pool.Run)
	}
	if r.pipeline != nil {
		start("media", func(c context.Context) { r.pipeline.RunJanitorLoop(c, media.JanitorInterval) })
	}
	if r.hub != nil {
		start("ws", r.runHub)
	}
	if r.commands != nil {
		start("screen", r.runExpirer)
	}
	start("retention", r.runRetention)
	if r.backups != nil && r.cfg.Backup.Enabled && r.backups.Enabled() {
		start("backup", r.runBackupScheduler)
	}
	defer workers.Wait()

	pairingTicker := time.NewTicker(time.Minute)
	defer pairingTicker.Stop()

	weatherInterval := r.weather.RefreshInterval()
	var weatherTicker *time.Ticker
	var weatherC <-chan time.Time
	if weatherInterval > 0 {
		weatherTicker = time.NewTicker(weatherInterval)
		defer weatherTicker.Stop()
		weatherC = weatherTicker.C
		go r.safely(ctx, "weather", func() {
			_ = r.weather.Refresh(ctx, r.now())
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-pairingTicker.C:
			r.safely(ctx, "pairing_retention", func() {
				if _, err := r.db.Pairings().ExpireOverdue(ctx, r.now()); err != nil {
					r.log.Warn("pairing expiry failed", "component", "app", "error", err.Error())
				}
				if _, err := r.db.Pairings().DeleteOlderThan(ctx, r.now().Add(-24*time.Hour)); err != nil {
					r.log.Warn("pairing prune failed", "component", "app", "error", err.Error())
				}
			})
		case <-weatherC:
			r.safely(ctx, "weather", func() {
				if err := r.weather.Refresh(ctx, r.now()); err != nil {
					r.log.Warn("weather refresh failed", "component", "widget", "error", err.Error())
				}
			})
		}
	}
}

// safely runs fn and turns a panic into a logged error, keeping the runtime
// alive as required by design §4.1.
func (r *Runtime) safely(_ context.Context, component string, fn func()) {
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("background worker panicked",
				"component", component, "event", "panic", "panic", rec)
		}
	}()
	fn()
}
