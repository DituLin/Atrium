package app

import (
	"context"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/backup"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/diag"
	"github.com/DituLin/Atrium/internal/screen"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/ws"
)

// buildRealtime wires the WebSocket hub and the command service. They are
// mutually dependent — the service delivers through the hub, the hub routes
// client messages to the service — so the handler is installed afterwards.
func (r *Runtime) buildRealtime(authenticator *auth.Authenticator) {
	r.hub = ws.NewHub(ws.Options{
		Auth:         authenticator,
		Bus:          r.bus,
		Logger:       r.log,
		Now:          r.now,
		OfflineAfter: r.cfg.Screens.OfflineAfter.D(),
	})
	r.commands = screen.NewService(screen.Options{
		DB:     r.db,
		Hub:    r.hub,
		Logger: r.log,
		Now:    r.now,
		TTL:    r.cfg.Screens.CommandTTL.D(),
	})
	r.hub.SetHandler(r.commands)
}

// buildOps wires diagnostics and backups.
func (r *Runtime) buildOps(errRing *diag.Errors) {
	if errRing == nil {
		errRing = diag.NewErrors()
	}
	r.diag = diag.New(diag.Deps{
		DB:        r.db,
		Sources:   r.sources,
		Media:     r.pipeline,
		Presence:  r.hub,
		Errors:    errRing,
		StartedAt: r.started,
		Insecure:  r.insecure,
		Widgets:   r.widgetDiagnostics,
		Now:       r.now,
	})
	r.backups = backup.New(backup.Options{
		Dir:        r.cfg.Backup.Dir,
		Keep:       r.cfg.Backup.Keep,
		ConfigPath: r.cfg.Path(),
		DB:         r.db.SQL(),
		Logger:     r.log,
		Now:        r.now,
	})
}

// widgetDiagnostics describes the optional widgets without exposing their
// payloads, which may contain a location label the operator chose.
func (r *Runtime) widgetDiagnostics(ctx context.Context) map[string]any {
	weather := map[string]any{
		"enabled":    r.cfg.Widgets.Weather.Enabled,
		"fetched_at": nil,
		"stale":      true,
		"error":      nil,
	}
	if r.cfg.Widgets.Weather.Enabled {
		if w, err := r.weather.Current(ctx, r.now()); err == nil && w != nil {
			weather["fetched_at"] = w.FetchedAt
			weather["stale"] = w.Stale
		} else if err != nil {
			weather["error"] = "unavailable"
		}
	}
	return map[string]any{
		"weather": weather,
		"notice":  map[string]any{"enabled": r.cfg.Widgets.Notice.Enabled},
	}
}

// runHub owns the heartbeat monitor and closes sessions at shutdown.
func (r *Runtime) runHub(ctx context.Context) { r.hub.Run(ctx) }

// runExpirer resolves commands whose deadline passed (design §4.1).
func (r *Runtime) runExpirer(ctx context.Context) { r.commands.RunExpirer(ctx) }

// runRetention deletes history past its window, once at startup and daily
// after that, so a long outage cannot leave a month of rows behind.
func (r *Runtime) runRetention(ctx context.Context) {
	r.retentionPass(ctx)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.retentionPass(ctx)
		}
	}
}

func (r *Runtime) retentionPass(ctx context.Context) {
	res, err := r.db.RunRetention(ctx, r.now())
	if err != nil {
		r.log.Warn("retention pass incomplete", "component", "store",
			"event", "retention_failed", "error", err.Error())
	}
	if res.Total() > 0 {
		r.log.Info("history pruned", "component", "store", "event", "retention",
			"commands", res.Commands, "scan_runs", res.ScanRuns,
			"audit_log", res.AuditLog, "jobs", res.Jobs, "pairings", res.Pairings)
	}
}

// compile-time proof that the store retention helper keeps its shape.
var _ = store.HistoryRetention

// runBackupScheduler takes the daily snapshot at backup.time in home time
// (design §6.10). It ticks every minute rather than sleeping until the target
// so a clock change or a laptop resume cannot skip a whole day.
func (r *Runtime) runBackupScheduler(ctx context.Context) {
	hour, minute, err := config.ParseDayTime(r.cfg.Backup.Time)
	if err != nil {
		r.log.Warn("backup schedule disabled", "component", "backup",
			"event", "bad_backup_time", "value", r.cfg.Backup.Time)
		return
	}
	loc, err := time.LoadLocation(r.cfg.Home.Timezone)
	if err != nil {
		loc = time.UTC
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var lastDay string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		local := r.now().In(loc)
		day := local.Format("2006-01-02")
		if day == lastDay || local.Hour() != hour || local.Minute() != minute {
			continue
		}
		lastDay = day
		if _, err := r.backups.Run(ctx); err != nil {
			r.log.Error("scheduled backup failed", "component", "backup",
				"event", "backup_failed", "code", "backup_failed", "error", err.Error())
		}
	}
}
