package briefing

import (
	"context"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/family"
	"github.com/DituLin/Atrium/internal/widget"
)

// HouseReader supplies a public House projection without photo queries or NAS I/O.
type HouseReader interface {
	Compose(context.Context) *family.HouseResponse
}

// Composer combines public House observations and configured notice validity.
type Composer struct {
	cfg   *config.Config
	house HouseReader
}

// NewComposer reuses the independent House composer and loaded configuration.
func NewComposer(cfg *config.Config, house HouseReader) *Composer {
	return &Composer{cfg: cfg, house: house}
}

// Compose filters notice at the House generation instant, including at boundaries.
func (c *Composer) Compose(ctx context.Context) *OverviewResponse {
	h := c.house.Compose(ctx)
	reason := family.NotConfigured
	calendar := family.SourceSnapshot[struct{}]{SourceID: "calendar", SourceLabel: "家庭日历", Availability: family.NotConnected, Reason: &reason, Items: []struct{}{}}
	s := Sources{Core: h.Core, NAS: h.NAS, Profile: h.Profile, Environment: h.Environment, Notice: c.notice(h.GeneratedAt), Calendar: calendar}
	return &OverviewResponse{SchemaVersion: h.SchemaVersion, GeneratedAt: h.GeneratedAt, Home: h.Home, Sources: s, Entries: OrderEntries(s)}
}

func (c *Composer) notice(generatedAt string) family.SourceSnapshot[Notice] {
	reason := family.NotConfigured
	s := family.SourceSnapshot[Notice]{SourceID: "notice", SourceLabel: "家庭提示", Availability: family.NotConnected, Reason: &reason, Items: []Notice{}}
	if !c.cfg.Widgets.Notice.Enabled {
		return s
	}
	loaded := c.cfg.LoadedAt()
	// Defaults/Parse/manual configs have no successful load evidence. Production
	// config.Load supplies it; do not expose unobserved text or invent a timestamp.
	if loaded == nil {
		reason = family.NotObserved
		s.Availability = family.Loading
		return s
	}
	observed := loaded.Format(time.RFC3339Nano)
	s.ObservedAt = &observed
	s.Availability = family.Available
	s.Reason = nil
	now, err := time.Parse(time.RFC3339Nano, generatedAt)
	if err != nil {
		reason = family.ReadFailed
		s.Availability = family.Failed
		s.Reason = &reason
		s.ObservedAt = nil
		return s
	}
	if n := widget.CurrentNotice(c.cfg.Widgets.Notice, now); n != nil {
		s.Items = append(s.Items, Notice{ItemMeta: family.ItemMeta{ID: "notice", UpdatedAt: n.UpdatedAt, ValidFrom: n.ValidFrom, ValidUntil: n.ValidUntil}, Text: n.Text})
	}
	return s
}

// OrderEntries orders already projected source snapshots. Callers compute expiry
// and validity first. Stale observations never become current health alarms.
func OrderEntries(s Sources) []Entry {
	buckets := [3][]Entry{}
	for _, source := range s.NAS {
		priority := -1
		switch source.Availability {
		case family.Failed:
			priority = 0
		case family.Stale:
			priority = 1
		case family.Loading:
			priority = 2
		case family.Available:
			if len(source.Items) == 0 {
				priority = 2
			} else {
				switch source.Items[0].Health {
				case domain.HealthOffline, domain.HealthDegraded:
					priority = 0
				case domain.HealthOnline:
				default:
					priority = 2
				}
			}
		}
		entry := Entry{ID: "nas:" + source.SourceID + ":source_status", Kind: "source_status", Module: "nas", SourceID: source.SourceID}
		switch priority {
		case 0:
			buckets[0] = append(buckets[0], entry)
		case 1:
			buckets[1] = append(buckets[1], entry)
		case 2:
			buckets[2] = append(buckets[2], entry)
		}
	}
	entries := make([]Entry, 0)
	for _, bucket := range buckets {
		entries = append(entries, bucket...)
	}
	if s.Notice.Availability == family.Available || s.Notice.Availability == family.Stale {
		for _, item := range s.Notice.Items {
			id := item.ID
			entries = append(entries, Entry{ID: "notice:" + s.Notice.SourceID + ":" + id, Kind: "notice", Module: "notice", SourceID: s.Notice.SourceID, ItemID: &id})
		}
	}
	return entries
}
