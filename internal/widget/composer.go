package widget

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/DituLin/Atritum/internal/clock"
	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
)

// SchemaVersion is the home snapshot contract version.
const SchemaVersion = 1

// Composer builds the home snapshot from cheap database reads. It performs no
// NAS I/O, so a hung share cannot slow the dashboard (design §6.7).
type Composer struct {
	cfg     *config.Config
	db      *store.DB
	home    *clock.Home
	weather *WeatherService

	// index reports live scan progress; nil before the indexer is wired.
	index func() Index
	// sourceStats reports per-source I/O counters for the admin view.
	sourceStats func(id string) SourceStats

	// photoCache holds the last computed photo widget. The design budget for
	// GET /home is 200 ms P95, so the counters are recomputed at most every
	// two seconds rather than on every request (design §13).
	mu          sync.Mutex
	photoCache  *Photo
	photoCached time.Time
}

// SourceStats are the admin-only I/O counters of one source.
type SourceStats struct {
	StuckOps          int64
	IdentityConfirmed bool
}

// PhotoCacheTTL is how long the photo widget counters are reused.
const PhotoCacheTTL = 2 * time.Second

// WithIndex attaches the live index progress provider.
func (c *Composer) WithIndex(fn func() Index) *Composer {
	c.index = fn
	return c
}

// WithSourceStats attaches the per-source I/O counter provider.
func (c *Composer) WithSourceStats(fn func(id string) SourceStats) *Composer {
	c.sourceStats = fn
	return c
}

// InvalidatePhotoCache forces the next snapshot to recount, used when a worker
// knows the totals just changed.
func (c *Composer) InvalidatePhotoCache() {
	c.mu.Lock()
	c.photoCache = nil
	c.mu.Unlock()
}

// NewComposer builds the snapshot composer.
func NewComposer(cfg *config.Config, db *store.DB, home *clock.Home, weather *WeatherService) *Composer {
	return &Composer{cfg: cfg, db: db, home: home, weather: weather}
}

// Compose returns the dashboard snapshot. admin adds fields screens must not
// see, such as health detail.
func (c *Composer) Compose(ctx context.Context, admin bool) (*Snapshot, error) {
	now := c.home.Now()
	loc := c.home.Location()

	photo, err := c.photoWidget(ctx, now)
	if err != nil {
		return nil, err
	}
	nas, err := c.nasWidget(ctx, admin, loc)
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{
		SchemaVersion: SchemaVersion,
		HomeName:      c.cfg.Home.Name,
		ServerTime:    formatTime(now, loc),
		Clock:         c.clockWidget(now, loc),
		Photo:         *photo,
		NAS:           *nas,
	}
	if c.weather != nil {
		if wx, err := c.weather.Current(ctx, now); err == nil && wx != nil {
			snap.Weather = wx
		}
	}
	if c.cfg.Widgets.Notice.Enabled && c.cfg.Widgets.Notice.Text != "" {
		snap.Notice = &Notice{Text: c.cfg.Widgets.Notice.Text, UpdatedAt: formatTime(now, loc)}
	}
	return snap, nil
}

func (c *Composer) clockWidget(now time.Time, loc *time.Location) Clock {
	w := Clock{
		Timezone:         c.cfg.Home.Timezone,
		ServerTime:       formatTime(now, loc),
		UTCOffsetSeconds: c.home.OffsetAt(now),
	}
	if next := c.home.NextTransition(now); next != nil {
		w.NextOffsetChangeAt = formatTimePtr(next, loc)
	}
	return w
}

func (c *Composer) photoWidget(ctx context.Context, now time.Time) (*Photo, error) {
	c.mu.Lock()
	if c.photoCache != nil && now.Sub(c.photoCached) < PhotoCacheTTL {
		cached := *c.photoCache
		c.mu.Unlock()
		if c.index != nil {
			cached.Index = c.index()
		}
		return &cached, nil
	}
	c.mu.Unlock()

	photos := c.db.Photos()
	statuses, err := photos.StatusCounts(ctx)
	if err != nil {
		return nil, err
	}
	previews, err := photos.PreviewStatusCounts(ctx)
	if err != nil {
		return nil, err
	}
	ready, err := photos.CountReady(ctx)
	if err != nil {
		return nil, err
	}
	dayStart, _ := c.home.DayBounds(now)
	newToday, err := photos.CountFirstSeenSince(ctx, dayStart)
	if err != nil {
		return nil, err
	}
	capturedToday, err := photos.CountCapturedDay(ctx, c.home.HomeDay(now))
	if err != nil {
		return nil, err
	}
	unknown, err := photos.CountUnknownCaptured(ctx)
	if err != nil {
		return nil, err
	}

	sources, err := c.db.Sources().List(ctx)
	if err != nil {
		return nil, err
	}
	baseline := baselineState(sources)

	widget := &Photo{
		SlideshowIntervalSeconds: int(c.cfg.Screens.SlideshowInterval.D().Seconds()),
		Totals: PhotoTotals{
			Ready:          ready,
			PendingPreview: previews[domain.PreviewPending] + previews[domain.PreviewProcessing],
			Unsupported:    statuses[domain.PhotoUnsupported],
		},
		NewToday:        newToday,
		CapturedToday:   capturedToday,
		UnknownCaptured: unknown,
		Baseline:        baseline,
		Index:           Index{State: string(domain.IndexIdle)},
	}
	widget.Index.Progress.PendingPreview = widget.Totals.PendingPreview
	if c.index != nil {
		widget.Index = c.index()
		widget.Index.Progress.PendingPreview = widget.Totals.PendingPreview
	}

	c.mu.Lock()
	snapshot := *widget
	c.photoCache = &snapshot
	c.photoCached = now
	c.mu.Unlock()
	return widget, nil
}

// baselineState derives the import state from the configured sources.
func baselineState(sources []domain.Source) Baseline {
	var active, completed int
	var newest *time.Time
	for i := range sources {
		s := sources[i]
		if s.Status != domain.SourceActive {
			continue
		}
		active++
		if s.BaselineCompleted != nil {
			completed++
			if newest == nil || s.BaselineCompleted.After(*newest) {
				newest = s.BaselineCompleted
			}
		}
	}
	switch {
	case active == 0:
		return Baseline{Status: "none"}
	case completed == active:
		b := Baseline{Status: "done"}
		if newest != nil {
			s := newest.UTC().Format(time.RFC3339)
			b.CompletedAt = &s
		}
		return b
	default:
		return Baseline{Status: "importing"}
	}
}

func (c *Composer) nasWidget(ctx context.Context, admin bool, loc *time.Location) (*NAS, error) {
	rows, err := c.db.Sources().List(ctx)
	if err != nil {
		return nil, err
	}
	out := NAS{Sources: make([]NASSource, 0, len(rows))}
	for i := range rows {
		s := rows[i]
		if s.Status != domain.SourceActive {
			continue
		}
		item := NASSource{
			ID:              s.ID,
			Name:            s.Name,
			Health:          string(s.Health),
			LastCheckAt:     formatTimePtr(s.LastCheckAt, loc),
			LastSuccessAt:   formatTimePtr(s.LastSuccessAt, loc),
			ShareFreeBytes:  s.ShareFreeBytes,
			ShareTotalBytes: s.ShareTotalBytes,
		}
		if item.Health == "" {
			item.Health = string(domain.HealthUnknown)
		}
		if admin {
			item.HealthDetail = s.HealthDetail
			if c.sourceStats != nil {
				stats := c.sourceStats(s.ID)
				item.StuckOps = stats.StuckOps
				confirmed := stats.IdentityConfirmed
				item.IdentityConfirmed = &confirmed
			}
		}
		out.Sources = append(out.Sources, item)
	}
	return &out, nil
}

// ReconcileSources upserts every configured source and revokes the ones that
// disappeared from the configuration (design §4.2 step 4).
func ReconcileSources(ctx context.Context, db *store.DB, cfg *config.Config, now time.Time) ([]string, error) {
	repo := db.Sources()
	ids := make([]string, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		if err := repo.Upsert(ctx, s.ID, s.Name, s.Root, now); err != nil {
			return nil, fmt.Errorf("widget: reconcile source %q: %w", s.ID, err)
		}
		ids = append(ids, s.ID)
	}
	revoked, err := repo.RevokeMissing(ctx, ids, "removed_from_config", now)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	return revoked, nil
}
