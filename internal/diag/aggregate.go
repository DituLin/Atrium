package diag

import (
	"context"
	"os"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/media"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/version"
)

// Presence reports which screens have a live session; the hub implements it.
type Presence interface {
	Online(screenID string) bool
}

// Deps are the collaborators the aggregator reads. Each is optional so a test
// or an early boot can render the sections it has.
type Deps struct {
	DB        *store.DB
	Sources   *source.Manager
	Media     *media.Pipeline
	Presence  Presence
	Errors    *Errors
	StartedAt time.Time
	Insecure  bool
	// Widgets renders the optional-widget section, which only the composer
	// knows how to describe.
	Widgets func(context.Context) map[string]any
	Now     func() time.Time
}

// Aggregator builds the diagnostics document.
type Aggregator struct{ deps Deps }

// New builds an aggregator.
func New(deps Deps) *Aggregator {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Aggregator{deps: deps}
}

// Errors exposes the ring so callers can wire it into the logger.
func (a *Aggregator) Errors() *Errors { return a.deps.Errors }

// Collect assembles the whole document. A failing section degrades to its zero
// value rather than failing the request: diagnostics must work when things are
// broken, which is the only time anyone reads them.
func (a *Aggregator) Collect(ctx context.Context) (Document, error) {
	now := a.deps.Now()
	doc := Document{
		Version:       version.String(),
		UptimeSeconds: int64(now.Sub(a.deps.StartedAt).Seconds()),
		Insecure:      a.deps.Insecure,
		Photos:        map[string]int64{},
		Pairings:      map[string]int64{},
		Widgets:       map[string]any{},
		Commands:      CommandsSection{Last24h: map[string]int64{}},
		RecentErrors:  []ErrorEntry{},
	}
	if a.deps.DB == nil {
		return doc, nil
	}
	doc.DB = a.dbSection(ctx)
	doc.Sources = a.sourceSections(ctx)
	a.photoSection(ctx, &doc)
	doc.Jobs = a.jobSection(ctx)
	doc.Cache = a.cacheSection(ctx)
	doc.Screens = a.screenSections(ctx)
	a.commandSection(ctx, &doc, now)
	a.pairingSection(ctx, &doc)
	if a.deps.Widgets != nil {
		doc.Widgets = a.deps.Widgets(ctx)
	}
	if a.deps.Errors != nil {
		doc.RecentErrors = a.deps.Errors.Recent()
	}
	return doc, nil
}

func (a *Aggregator) dbSection(ctx context.Context) DBSection {
	out := DBSection{PathRedacted: true}
	if n, err := a.deps.DB.AppliedMigrations(ctx); err == nil {
		out.Migrations = n
	}
	path := a.deps.DB.Path()
	if info, err := os.Stat(path); err == nil {
		out.SizeBytes = info.Size()
	}
	if info, err := os.Stat(path + "-wal"); err == nil {
		out.WALBytes = info.Size()
	}
	return out
}

func (a *Aggregator) sourceSections(ctx context.Context) []SourceSection {
	rows, err := a.deps.DB.Sources().List(ctx)
	if err != nil {
		return nil
	}
	out := make([]SourceSection, 0, len(rows))
	for i := range rows {
		s := rows[i]
		item := SourceSection{
			ID: s.ID, Health: string(s.Health), HealthDetail: s.HealthDetail,
			IdentityBound: s.IdentityBound != nil,
		}
		if s.LastSuccessAt != nil {
			v := s.LastSuccessAt.UTC().Format(time.RFC3339)
			item.LastSuccessAt = &v
		}
		if a.deps.Sources != nil {
			if e := a.deps.Sources.Get(s.ID); e != nil {
				item.StuckOps = e.Stats().StuckOps
			}
		}
		if runs, err := a.deps.DB.ScanRuns().List(ctx, s.ID, 1); err == nil && len(runs) > 0 {
			item.LastScan = scanSection(&runs[0])
		}
		out = append(out, item)
	}
	return out
}

func scanSection(run *domain.ScanRun) *ScanSection {
	out := &ScanSection{Status: string(run.Status), FilesSeen: run.FilesSeen, Errors: run.Errors}
	if run.FinishedAt != nil {
		out.DurationMS = run.FinishedAt.Sub(run.StartedAt).Milliseconds()
	}
	return out
}

func (a *Aggregator) photoSection(ctx context.Context, doc *Document) {
	counts, err := a.deps.DB.Photos().StatusCounts(ctx)
	if err != nil {
		return
	}
	for _, st := range []domain.PhotoStatus{
		domain.PhotoReady, domain.PhotoPending, domain.PhotoUnsupported,
		domain.PhotoRemoved, domain.PhotoExcluded,
	} {
		doc.Photos[string(st)] = counts[st]
	}
	previews, err := a.deps.DB.Photos().PreviewStatusCounts(ctx)
	if err != nil {
		return
	}
	doc.Photos["preview_failed"] = previews[domain.PreviewFailed]
	doc.Photos["preview_evicted"] = previews[domain.PreviewEvicted]
	doc.Photos["preview_pending"] = previews[domain.PreviewPending] + previews[domain.PreviewProcessing]
}

func (a *Aggregator) jobSection(ctx context.Context) JobsSection {
	out := JobsSection{}
	counts, err := a.deps.DB.Jobs().Counts(ctx)
	if err == nil {
		out.Queued = counts[domain.JobQueued]
		out.Running = counts[domain.JobRunning]
		out.Failed = counts[domain.JobFailed]
		out.Done = counts[domain.JobDone]
	}
	if oldest, err := a.deps.DB.Jobs().OldestQueuedAt(ctx); err == nil && oldest != nil {
		v := oldest.UTC().Format(time.RFC3339)
		out.OldestQueued = &v
	}
	return out
}

func (a *Aggregator) cacheSection(ctx context.Context) CacheSection {
	if a.deps.Media == nil {
		return CacheSection{}
	}
	st := a.deps.Media.CacheState(ctx)
	out := CacheSection{
		Bytes: st.Bytes, BudgetBytes: st.BudgetBytes, FreeDiskBytes: st.FreeDiskBytes,
	}
	if st.PausedReason != "" {
		v := st.PausedReason
		out.PausedReason = &v
	}
	return out
}

func (a *Aggregator) screenSections(ctx context.Context) []ScreenSection {
	rows, err := a.deps.DB.Screens().List(ctx)
	if err != nil {
		return nil
	}
	out := make([]ScreenSection, 0, len(rows))
	for i := range rows {
		s := rows[i]
		item := ScreenSection{
			ID: s.ID, Registered: s.Status == domain.ScreenActive,
			Route: s.CurrentRoute, AppliedSequence: s.AppliedSequence,
		}
		if a.deps.Presence != nil {
			item.Online = item.Registered && a.deps.Presence.Online(s.ID)
		}
		if s.LastSeenAt != nil {
			v := s.LastSeenAt.UTC().Format(time.RFC3339)
			item.LastSeenAt = &v
		}
		if n, err := a.deps.DB.Commands().CountOpen(ctx, s.ID); err == nil {
			item.OpenCommands = n
		}
		out = append(out, item)
	}
	return out
}

func (a *Aggregator) commandSection(ctx context.Context, doc *Document, now time.Time) {
	counts, err := a.deps.DB.Commands().CountByStatusSince(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return
	}
	for _, st := range []domain.CommandStatus{
		domain.CommandApplied, domain.CommandFailed, domain.CommandExpired,
		domain.CommandUnknown, domain.CommandAccepted,
	} {
		doc.Commands.Last24h[string(st)] = counts[st]
	}
}

func (a *Aggregator) pairingSection(ctx context.Context, doc *Document) {
	rows, err := a.deps.DB.Pairings().List(ctx, 100)
	if err != nil {
		return
	}
	for i := range rows {
		doc.Pairings[string(rows[i].Status)]++
	}
}
