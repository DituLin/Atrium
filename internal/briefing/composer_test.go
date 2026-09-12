package briefing_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/briefing"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/family"
	"github.com/stretchr/testify/require"
)

type houseReader struct {
	snapshot *family.HouseResponse
	calls    int
}

func (h *houseReader) Compose(context.Context) *family.HouseResponse { h.calls++; return h.snapshot }
func loadedConfig(t *testing.T) *config.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte("home:\n  timezone: Asia/Singapore\nserver:\n  public_url: https://localhost:8443\nwidgets:\n  notice:\n    enabled: true\n    text: Test notice\n"), 0600))
	cfg, err := config.Load(p)
	require.NoError(t, err)
	return cfg
}
func TestOverviewNoticeUsesHouseInstantAndLoadObservation(t *testing.T) {
	cfg := loadedConfig(t)
	cfg.Widgets.Notice.ValidFrom = "2026-09-12T18:00:00+08:00"
	cfg.Widgets.Notice.ValidUntil = "2026-09-12T10:01:00Z"
	house := &houseReader{snapshot: &family.HouseResponse{SchemaVersion: 1, GeneratedAt: cfg.Widgets.Notice.ValidFrom, NAS: []family.SourceSnapshot[family.NASHealth]{}}}
	c := briefing.NewComposer(cfg, house)
	first := c.Compose(t.Context())
	require.Equal(t, 1, house.calls)
	require.Equal(t, house.snapshot.GeneratedAt, first.GeneratedAt)
	require.Len(t, first.Sources.Notice.Items, 1)
	require.Nil(t, first.Sources.Notice.Items[0].UpdatedAt)
	observed, err := time.Parse(time.RFC3339Nano, *first.Sources.Notice.ObservedAt)
	require.NoError(t, err)
	require.True(t, observed.Equal(*cfg.LoadedAt()))
	require.Nil(t, first.Sources.Notice.ExpiresAt)
	require.Equal(t, []briefing.Entry{{ID: "notice:notice:notice", Kind: "notice", Module: "notice", SourceID: "notice", ItemID: str("notice")}}, first.Entries)
	house.snapshot.GeneratedAt = "2026-09-12T10:00:30Z"
	require.Equal(t, first.Entries, c.Compose(t.Context()).Entries)
	for _, at := range []string{"2026-09-12T09:59:59Z", cfg.Widgets.Notice.ValidUntil} {
		house.snapshot.GeneratedAt = at
		got := c.Compose(t.Context())
		require.Equal(t, family.Available, got.Sources.Notice.Availability)
		require.Empty(t, got.Sources.Notice.Items)
		require.Empty(t, got.Entries)
		require.Equal(t, first.Sources.Notice.ObservedAt, got.Sources.Notice.ObservedAt)
	}
}
func str(s string) *string { return &s }
func TestOverviewMissingDisabledAndEmptyNotice(t *testing.T) {
	for _, mode := range []string{"disabled", "unobserved", "empty", "active"} {
		t.Run(mode, func(t *testing.T) {
			cfg := loadedConfig(t)
			if mode == "disabled" {
				cfg.Widgets.Notice.Enabled = false
			}
			if mode == "unobserved" {
				cfg = config.Defaults()
				cfg.Widgets.Notice = config.Notice{Enabled: true, Text: "unobserved private text"}
			}
			if mode == "empty" {
				cfg.Widgets.Notice.Text = " "
			}
			h := &houseReader{snapshot: &family.HouseResponse{SchemaVersion: 1, GeneratedAt: "2026-09-12T10:00:00Z", NAS: []family.SourceSnapshot[family.NASHealth]{}}}
			got := briefing.NewComposer(cfg, h).Compose(t.Context())
			require.Equal(t, family.NotConnected, got.Sources.Calendar.Availability)
			require.Equal(t, family.NotConfigured, *got.Sources.Calendar.Reason)
			require.NotNil(t, got.Sources.Calendar.Items)
			require.Empty(t, got.Sources.Calendar.Items)
			require.Nil(t, got.Sources.Calendar.ObservedAt)
			switch mode {
			case "disabled":
				require.Equal(t, family.NotConnected, got.Sources.Notice.Availability)
				require.Equal(t, family.NotConfigured, *got.Sources.Notice.Reason)
				require.Nil(t, got.Sources.Notice.ObservedAt)
			case "unobserved":
				require.Equal(t, family.Loading, got.Sources.Notice.Availability)
				require.Equal(t, family.NotObserved, *got.Sources.Notice.Reason)
				require.Nil(t, got.Sources.Notice.ObservedAt)
			default:
				require.Equal(t, family.Available, got.Sources.Notice.Availability)
			}
			if mode != "active" {
				require.Empty(t, got.Sources.Notice.Items)
				require.NotNil(t, got.Entries)
				require.Empty(t, got.Entries)
			}
		})
	}
}
func TestOverviewStatusOrderingAndStableReferences(t *testing.T) {
	sources := briefing.Sources{NAS: []family.SourceSnapshot[family.NASHealth]{}, Notice: family.SourceSnapshot[briefing.Notice]{SourceID: "notice", Availability: family.Available, Items: []briefing.Notice{{ItemMeta: family.ItemMeta{ID: "notice"}, Text: "hello"}}}}
	for _, tc := range []struct {
		id string
		a  family.Availability
		h  domain.Health
	}{{"unknown", family.Available, domain.HealthUnknown}, {"old-offline", family.Stale, domain.HealthOffline}, {"offline", family.Available, domain.HealthOffline}, {"failed", family.Failed, ""}, {"degraded", family.Available, domain.HealthDegraded}, {"loading", family.Loading, ""}, {"online", family.Available, domain.HealthOnline}, {"removed", family.NotConnected, ""}} {
		sources.NAS = append(sources.NAS, family.SourceSnapshot[family.NASHealth]{SourceID: tc.id, Availability: tc.a, Items: []family.NASHealth{{Health: tc.h}}})
	}
	got := briefing.OrderEntries(sources)
	ids := []string{}
	for _, e := range got {
		ids = append(ids, e.SourceID)
		if e.Kind == "source_status" {
			require.Nil(t, e.ItemID)
			require.Equal(t, "nas:"+e.SourceID+":source_status", e.ID)
		}
	}
	require.Equal(t, []string{"offline", "failed", "degraded", "old-offline", "unknown", "loading", "notice"}, ids)
	sources.Notice.Availability = family.Failed
	require.Len(t, briefing.OrderEntries(sources), 6)
}

// These snapshots have already had expiry/validity applied. The TV projector
// consumes the same cases after its clock/network projection before ordering.
func TestSharedOverviewOrderingScenarios(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/overview-ordering.json")
	require.NoError(t, err)
	var cases []struct {
		Name    string           `json:"name"`
		Sources briefing.Sources `json:"sources"`
		Entries []briefing.Entry `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(raw, &cases))
	require.NotEmpty(t, cases)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) { require.Equal(t, tc.Entries, briefing.OrderEntries(tc.Sources)) })
	}
}
