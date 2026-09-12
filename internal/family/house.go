package family

import (
	"context"
	"time"

	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
)

// ObservationTTL applies independently to Core responses and NAS checks.
const ObservationTTL = time.Minute

// SourceReader reads persisted observations and current authorization only.
// The House request never probes a share or reads photo statistics.
type SourceReader interface {
	List(context.Context) ([]domain.Source, error)
}

// HouseComposer builds public House snapshots from persisted source observations.
type HouseComposer struct {
	cfg     *config.Config
	sources SourceReader
	home    *clock.Home
}

// NewHouseComposer connects the configured home, source reader and clock.
func NewHouseComposer(cfg *config.Config, sources SourceReader, home *clock.Home) *HouseComposer {
	return &HouseComposer{cfg: cfg, sources: sources, home: home}
}

// Compose returns independent module snapshots. A failed authorization read
// clears NAS content; it must not retain items from a prior successful read.
func (c *HouseComposer) Compose(ctx context.Context) *HouseResponse {
	now := c.home.Now()
	observed := c.formatTime(&now)
	expires := now.Add(ObservationTTL)
	response := &HouseResponse{
		SchemaVersion: 1, GeneratedAt: *observed,
		Home:        HouseHome{Name: c.cfg.Home.Name, Timezone: c.cfg.Home.Timezone},
		Core:        SourceSnapshot[CoreResponse]{SourceID: "core", SourceLabel: "Core", ObservedAt: observed, ExpiresAt: c.formatTime(&expires), Availability: Available, Items: []CoreResponse{{ItemMeta: ItemMeta{ID: "response"}, Responding: true}}},
		NAS:         make([]SourceSnapshot[NASHealth], 0),
		Profile:     unconnected("profile", "房屋资料", NotProvided),
		Environment: unconnected("environment", "环境数据", NotSupported),
	}
	rows, err := c.sources.List(ctx)
	if err != nil {
		for _, configured := range c.cfg.Sources {
			reason := ReadFailed
			response.NAS = append(response.NAS, SourceSnapshot[NASHealth]{SourceID: configured.ID, SourceLabel: configured.Name, Availability: Failed, Reason: &reason, Items: []NASHealth{}})
		}
		return response
	}
	active := make(map[string]domain.Source, len(rows))
	for _, row := range rows {
		if row.Status == domain.SourceActive {
			active[row.ID] = row
		}
	}
	for _, configured := range c.cfg.Sources {
		row, ok := active[configured.ID]
		if !ok {
			continue
		}
		snapshot := SourceSnapshot[NASHealth]{SourceID: configured.ID, SourceLabel: configured.Name, Items: []NASHealth{}}
		if row.LastCheckAt == nil {
			reason := NotObserved
			snapshot.Availability, snapshot.Reason = Loading, &reason
		} else {
			expires := row.LastCheckAt.Add(ObservationTTL)
			snapshot.ObservedAt, snapshot.ExpiresAt = c.formatTime(row.LastCheckAt), c.formatTime(&expires)
			snapshot.Availability = Available
			if !now.Before(expires) {
				reason := Expired
				snapshot.Availability, snapshot.Reason = Stale, &reason
			}
			health := row.Health
			if health == "" {
				health = domain.HealthUnknown
			}
			snapshot.Items = []NASHealth{{ItemMeta: ItemMeta{ID: "health"}, Health: health, LastSuccessAt: c.formatTime(row.LastSuccessAt)}}
		}
		response.NAS = append(response.NAS, snapshot)
	}
	return response
}

func unconnected(id, label string, reason Reason) SourceSnapshot[struct{}] {
	return SourceSnapshot[struct{}]{SourceID: id, SourceLabel: label, Availability: NotConnected, Reason: &reason, Items: []struct{}{}}
}

func (c *HouseComposer) formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := t.In(c.home.Location()).Format(time.RFC3339Nano)
	return &formatted
}
