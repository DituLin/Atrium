package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/DituLin/Atritum/internal/app/events"
	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
)

// ProbeInterval is the health check cadence from design §4.1.
const ProbeInterval = 15 * time.Second

// Entry is the runtime state of one configured source.
type Entry struct {
	ID       string
	Name     string
	Config   config.Source
	FS       FS
	Identity IdentityConfig

	mu       sync.RWMutex
	health   domain.Health
	detail   string
	bound    *domain.Identity
	lastOK   time.Time
	lastAt   time.Time
	mismatch bool
}

// Health returns the last observed health and its detail code.
func (e *Entry) Health() (domain.Health, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.health, e.detail
}

// Online reports whether the source may be scanned and read.
func (e *Entry) Online() bool {
	h, _ := e.Health()
	return h == domain.HealthOnline
}

// IdentityMismatch reports whether the bound identity stopped matching. While
// true nothing is scanned and no photo is ever marked removed (design §6.1).
func (e *Entry) IdentityMismatch() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.mismatch
}

// Bound returns the identity bound on the first successful probe.
func (e *Entry) Bound() *domain.Identity {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.bound
}

// Stats returns the I/O counters when the entry uses an OSFS.
func (e *Entry) Stats() Stats {
	if osfs, ok := e.FS.(*OSFS); ok {
		return osfs.Stats()
	}
	return Stats{}
}

// Extensions returns the configured lower-case extension allowlist.
func (e *Entry) Extensions() map[string]bool {
	out := make(map[string]bool, len(e.Config.IncludeExtensions))
	for _, ext := range e.Config.IncludeExtensions {
		out[domain.NormalizeExt(ext)] = true
	}
	return out
}

func (e *Entry) set(p Probe, mismatch bool, now time.Time) (changed bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	changed = e.health != p.Health || e.detail != p.Detail || e.mismatch != mismatch
	e.health = p.Health
	e.detail = p.Detail
	e.mismatch = mismatch
	e.lastAt = now
	if p.OK() && !mismatch {
		e.lastOK = now
	}
	return changed
}

func (e *Entry) setBound(ident *domain.Identity) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bound = ident
}

// Manager owns one Entry per configured source and runs their health probers.
type Manager struct {
	db     *store.DB
	log    *slog.Logger
	bus    *events.Bus
	now    func() time.Time
	order  []string
	byID   map[string]*Entry
	period time.Duration
}

// ManagerOptions configures a Manager.
type ManagerOptions struct {
	Config *config.Config
	DB     *store.DB
	Logger *slog.Logger
	Bus    *events.Bus
	Now    func() time.Time
	// ProbeInterval overrides the default cadence; tests use a short one.
	ProbeInterval time.Duration
	// NewFS overrides filesystem construction so tests can inject FakeFS.
	NewFS func(config.Source) (FS, error)
}

// NewManager builds the runtime source registry from the configuration.
func NewManager(opts ManagerOptions) (*Manager, error) {
	if opts.Config == nil || opts.DB == nil {
		return nil, errors.New("source: config and db are required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ProbeInterval <= 0 {
		opts.ProbeInterval = ProbeInterval
	}
	if opts.NewFS == nil {
		opts.NewFS = defaultNewFS
	}
	m := &Manager{
		db: opts.DB, log: opts.Logger, bus: opts.Bus, now: opts.Now,
		byID: make(map[string]*Entry, len(opts.Config.Sources)), period: opts.ProbeInterval,
	}
	for _, sc := range opts.Config.Sources {
		fsys, err := opts.NewFS(sc)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", sc.ID, err)
		}
		m.byID[sc.ID] = &Entry{
			ID: sc.ID, Name: sc.Name, Config: sc, FS: fsys,
			Identity: IdentityConfig{
				RequireMount: sc.Identity.RequireMount,
				AllowLocal:   sc.Identity.AllowLocal,
				MarkerFile:   sc.Identity.MarkerFile,
			},
			health: domain.HealthUnknown,
			detail: DetailNeverProbed,
		}
		m.order = append(m.order, sc.ID)
	}
	sort.Strings(m.order)
	return m, nil
}

func defaultNewFS(sc config.Source) (FS, error) {
	return NewOSFS(OSFSOptions{
		Root:        sc.Root,
		IOTimeout:   sc.IOTimeout.D(),
		MaxInflight: sc.MaxInflight,
	})
}

// Get returns one entry, or nil when the ID is not configured.
func (m *Manager) Get(id string) *Entry { return m.byID[id] }

// All returns every entry in stable ID order.
func (m *Manager) All() []*Entry {
	out := make([]*Entry, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.byID[id])
	}
	return out
}

// Load reads the persisted identity bindings so a restart does not re-bind a
// source that has been swapped for a different mount while the server was down.
func (m *Manager) Load(ctx context.Context) error {
	rows, err := m.db.Sources().List(ctx)
	if err != nil {
		return err
	}
	for i := range rows {
		entry := m.byID[rows[i].ID]
		if entry == nil {
			continue
		}
		entry.setBound(rows[i].IdentityBound)
	}
	return nil
}

// Probe checks one source and persists the outcome. It returns the health that
// was recorded so callers can log or act on a transition.
func (m *Manager) Probe(ctx context.Context, e *Entry) Probe {
	now := m.now()
	p := CheckIdentity(ctx, e.FS, e.Identity)

	mismatch := false
	if p.OK() && p.Identity != nil {
		switch bound := e.Bound(); {
		case bound == nil:
			// First success binds; from now on the mount must stay the same.
			if err := m.db.Sources().BindIdentity(ctx, e.ID, *p.Identity, now); err != nil {
				m.log.Warn("identity binding failed", "component", "source",
					"event", "identity_bind_failed", "source_id", e.ID, "error", err.Error())
			} else {
				e.setBound(p.Identity)
				m.log.Info("source identity bound", "component", "source",
					"event", "identity_bound", "source_id", e.ID, "fstype", p.Identity.FSType)
			}
		case !bound.Equal(*p.Identity):
			mismatch = true
			p.Health = domain.HealthUnknown
			p.Detail = DetailIdentityMismatch
		}
	}

	if osfs, ok := e.FS.(*OSFS); ok {
		if p.OK() && !mismatch {
			osfs.ClearDegraded()
		} else if osfs.Degraded() && p.Health == domain.HealthOnline {
			p.Health = domain.HealthDegraded
			p.Detail = DetailStuckIO
		}
	}

	changed := e.set(p, mismatch, now)
	success := p.OK() && !mismatch
	if err := m.db.Sources().SetHealth(ctx, e.ID, p.Health, p.Detail, success, now); err != nil &&
		!errors.Is(err, domain.ErrNotFound) {
		m.log.Warn("health update failed", "component", "source",
			"event", "health_write_failed", "source_id", e.ID, "error", err.Error())
	}
	m.recordShareStats(ctx, e, p, now)

	if changed {
		m.log.Info("source health changed", "component", "source", "event", "health_changed",
			"source_id", e.ID, "health", string(p.Health), "code", p.Detail)
		if m.bus != nil {
			m.bus.Publish(domain.TopicNAS, domain.TopicHome)
		}
	}
	return p
}

// recordShareStats stores capacity only when it is meaningful: a network
// filesystem reporting non-zero values (FR-12). Anything else stays hidden
// rather than being presented as the NAS's physical capacity.
func (m *Manager) recordShareStats(ctx context.Context, e *Entry, p Probe, now time.Time) {
	if !p.OK() || !IsNetworkFilesystem(p.Volume.FSType) {
		return
	}
	if p.Volume.TotalBytes <= 0 || p.Volume.FreeBytes < 0 {
		return
	}
	if err := m.db.Sources().SetShareStats(ctx, e.ID, p.Volume.TotalBytes, p.Volume.FreeBytes, now); err != nil &&
		!errors.Is(err, domain.ErrNotFound) {
		m.log.Warn("share stats update failed", "component", "source",
			"event", "share_stats_failed", "source_id", e.ID, "error", err.Error())
	}
}

// Rebind accepts the mount currently behind a source as its new identity,
// clearing an identity_mismatch (admin action, design §6.1).
func (m *Manager) Rebind(ctx context.Context, id string) (Probe, error) {
	e := m.Get(id)
	if e == nil {
		return Probe{}, fmt.Errorf("source %q: %w", id, domain.ErrNotFound)
	}
	p := CheckIdentity(ctx, e.FS, e.Identity)
	if !p.OK() || p.Identity == nil {
		return p, domain.Errorf(domain.CodeSourceOffline,
			"source %s is not readable right now (%s)", id, p.Detail)
	}
	now := m.now()
	if err := m.db.Sources().BindIdentity(ctx, id, *p.Identity, now); err != nil {
		return p, err
	}
	e.setBound(p.Identity)
	e.set(p, false, now)
	if err := m.db.Sources().SetHealth(ctx, id, p.Health, p.Detail, true, now); err != nil {
		return p, err
	}
	m.log.Info("source identity rebound", "component", "source",
		"event", "identity_rebound", "source_id", id)
	if m.bus != nil {
		m.bus.Publish(domain.TopicNAS, domain.TopicHome)
	}
	return p, nil
}

// Run probes every source on the configured cadence until ctx is cancelled.
// Each source gets its own goroutine so one hung mount cannot delay another.
func (m *Manager) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, e := range m.All() {
		wg.Add(1)
		go func(entry *Entry) {
			defer wg.Done()
			m.runOne(ctx, entry)
		}(e)
	}
	wg.Wait()
}

func (m *Manager) runOne(ctx context.Context, e *Entry) {
	ticker := time.NewTicker(m.period)
	defer ticker.Stop()
	m.safeProbe(ctx, e)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.safeProbe(ctx, e)
		}
	}
}

// safeProbe contains a panic so one broken source cannot take the process down.
func (m *Manager) safeProbe(ctx context.Context, e *Entry) {
	defer func() {
		if rec := recover(); rec != nil {
			m.log.Error("source prober panicked", "component", "source",
				"event", "panic", "source_id", e.ID, "panic", rec)
		}
	}()
	m.Probe(ctx, e)
}

// FS returns the filesystem of a configured source.
func (m *Manager) FS(id string) (FS, bool) {
	e := m.Get(id)
	if e == nil {
		return nil, false
	}
	return e.FS, true
}

// Online reports whether a source is currently readable and identified.
func (m *Manager) Online(id string) bool {
	e := m.Get(id)
	return e != nil && e.Online() && !e.IdentityMismatch()
}
