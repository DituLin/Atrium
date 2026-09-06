package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/widget"
)

func TestHomeSnapshotBaselineShape(t *testing.T) {
	h := newHarness(t)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodGet, "/api/v1/home", "", bearer(screen))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	snap := decode[widget.Snapshot](t, resp)

	require.Equal(t, widget.SchemaVersion, snap.SchemaVersion)
	require.Equal(t, "Test home", snap.HomeName)

	// Clock widget: Asia/Singapore is UTC+8 and has no DST transition.
	require.Equal(t, "Asia/Singapore", snap.Clock.Timezone)
	require.Equal(t, 8*3600, snap.Clock.UTCOffsetSeconds)
	require.Nil(t, snap.Clock.NextOffsetChangeAt, "a zone without DST has no upcoming change")
	require.Equal(t, "2026-09-05T20:00:00+08:00", snap.Clock.ServerTime)

	// Photo widget: zero baseline until the indexer lands in V0.2.
	require.Equal(t, 30, snap.Photo.SlideshowIntervalSeconds)
	require.Zero(t, snap.Photo.Totals.Ready)
	require.Zero(t, snap.Photo.Totals.PendingPreview)
	require.Zero(t, snap.Photo.Totals.Unsupported)
	require.Zero(t, snap.Photo.NewToday)
	require.Zero(t, snap.Photo.CapturedToday)
	require.Zero(t, snap.Photo.UnknownCaptured)
	require.Equal(t, "none", snap.Photo.Baseline.Status)
	require.Equal(t, "idle", snap.Photo.Index.State)

	// No sources configured, so the NAS widget is an empty list, never null.
	require.NotNil(t, snap.NAS.Sources)
	require.Empty(t, snap.NAS.Sources)

	// Optional widgets are absent when disabled.
	require.Nil(t, snap.Weather)
	require.Nil(t, snap.Notice)
}

func TestHomeNASWidgetFromConfigWithUnknownHealth(t *testing.T) {
	h := newHarness(t, func(c *config.Config) {
		src := config.SourceDefaults()
		src.ID = "family_photos"
		src.Name = "Family photos"
		src.Root = "/Volumes/photos/family"
		c.Sources = append(c.Sources, src)
	})
	admin := h.newAdminToken()
	// Startup reconciliation is the app's job; the test performs it directly.
	_, err := widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
	require.NoError(t, err)

	screen := h.pairScreen(admin, "living_room_tv", "Living room")
	resp := h.request(http.MethodGet, "/api/v1/home", "", bearer(screen))
	snap := decode[widget.Snapshot](t, resp)

	require.Len(t, snap.NAS.Sources, 1)
	src := snap.NAS.Sources[0]
	require.Equal(t, "family_photos", src.ID)
	require.Equal(t, "Family photos", src.Name)
	require.Equal(t, "unknown", src.Health, "health is unknown until the prober runs in V0.2")
	require.Nil(t, src.ShareFreeBytes)
	require.Empty(t, src.HealthDetail, "screens never see health detail")

	// The NAS route mirrors the widget and adds detail for admins only.
	nasScreen := h.request(http.MethodGet, "/api/v1/nas/status", "", bearer(screen))
	require.Equal(t, http.StatusOK, nasScreen.StatusCode)
	require.Empty(t, decode[widget.NAS](t, nasScreen).Sources[0].HealthDetail)

	require.NoError(t, h.db.Sources().SetHealth(t.Context(), "family_photos",
		"degraded", "eacces_on_some_entries", false, h.clock.Now()))
	nasAdmin := h.request(http.MethodGet, "/api/v1/nas/status", "", bearer(admin))
	require.Equal(t, "eacces_on_some_entries", decode[widget.NAS](t, nasAdmin).Sources[0].HealthDetail)
}

func TestHomeClockReportsNextDSTTransition(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Home.Timezone = "Europe/Berlin" })
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "Living room")

	// 2026-09-05 12:00 UTC is CEST (UTC+2); the next change is in October.
	resp := h.request(http.MethodGet, "/api/v1/home", "", bearer(screen))
	snap := decode[widget.Snapshot](t, resp)
	require.Equal(t, 2*3600, snap.Clock.UTCOffsetSeconds)
	require.NotNil(t, snap.Clock.NextOffsetChangeAt)

	next, err := time.Parse(time.RFC3339, *snap.Clock.NextOffsetChangeAt)
	require.NoError(t, err)
	require.Equal(t, time.October, next.UTC().Month())
	require.Equal(t, 2026, next.UTC().Year())
}

func TestHomeNoticeWidgetWhenEnabled(t *testing.T) {
	h := newHarness(t, func(c *config.Config) {
		c.Widgets.Notice.Enabled = true
		c.Widgets.Notice.Text = "Guests arriving at 18:00"
	})
	screen := h.pairScreen(h.newAdminToken(), "living_room_tv", "Living room")

	snap := decode[widget.Snapshot](t, h.request(http.MethodGet, "/api/v1/home", "", bearer(screen)))
	require.NotNil(t, snap.Notice)
	require.Equal(t, "Guests arriving at 18:00", snap.Notice.Text)
}

func TestScreensRegistryListAndPresence(t *testing.T) {
	h := newHarness(t)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "Living room")

	list := decode[struct {
		Screens []map[string]any `json:"screens"`
	}](t, h.request(http.MethodGet, "/api/v1/screens", "", bearer(admin)))
	require.Len(t, list.Screens, 1)
	require.Equal(t, true, list.Screens[0]["registered"])
	require.Equal(t, false, list.Screens[0]["online"], "a screen that never called is registered but offline")

	// An authenticated request counts as presence.
	require.Equal(t, http.StatusOK, h.request(http.MethodGet, "/api/v1/home", "", bearer(screen)).StatusCode)
	one := decode[map[string]any](t, h.request(http.MethodGet, "/api/v1/screens/living_room_tv", "", bearer(admin)))
	require.Equal(t, true, one["online"])

	// Beyond offline_after the screen is registered but no longer online.
	h.clock.Advance(h.cfg.Screens.OfflineAfter.D() + time.Second)
	stale := decode[map[string]any](t, h.request(http.MethodGet, "/api/v1/screens/living_room_tv", "", bearer(admin)))
	require.Equal(t, true, stale["registered"])
	require.Equal(t, false, stale["online"])

	missing := h.request(http.MethodGet, "/api/v1/screens/nope", "", bearer(admin))
	require.Equal(t, http.StatusNotFound, missing.StatusCode)
}

func TestDiagnosticsExposesNoPaths(t *testing.T) {
	h := newHarness(t, func(c *config.Config) {
		src := config.SourceDefaults()
		src.ID = "family_photos"
		src.Root = "/Volumes/photos/family"
		c.Sources = append(c.Sources, src)
	})
	_, err := widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
	require.NoError(t, err)

	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodGet, "/api/v1/diagnostics", "", bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw := decode[map[string]any](t, resp)
	require.Equal(t, true, raw["db"].(map[string]any)["path_redacted"])
	require.Equal(t, false, raw["insecure"])
	require.NotEmpty(t, raw["version"])
	require.Len(t, raw["sources"], 1)
	require.Len(t, raw["screens"], 1)

	body := decodeRaw(t, h, "/api/v1/diagnostics", admin)
	require.NotContains(t, body, "/Volumes/photos/family", "diagnostics must never leak a NAS path")
	require.NotContains(t, body, "atr_adm_")
	require.NotContains(t, body, "token_hash")
}
