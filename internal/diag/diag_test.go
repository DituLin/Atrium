package diag_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/diag"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
	"github.com/DituLin/Atritum/internal/store/testutil"
)

type fakePresence struct{ online map[string]bool }

func (f fakePresence) Online(id string) bool { return f.online[id] }

func seedFixture(t *testing.T) (*store.DB, time.Time) {
	t.Helper()
	db := testutil.NewDB(t)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	ctx := t.Context()

	require.NoError(t, db.Sources().Upsert(ctx, "family_photos", "Family", "/Volumes/photos/family", now))
	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{
		ID: "living_room_tv", Name: "Living room", TokenHash: "atr_scr_hash_value",
		Status: domain.ScreenActive, CreatedAt: now, ApprovedAt: now,
	}))
	ph := &domain.Photo{
		SourceID: "family_photos", RelPath: "2026/private/holiday.jpg", Ext: "jpg",
		SizeBytes: 100, MtimeUnix: now.Unix(), Status: domain.PhotoReady,
		FirstSeenAt: now, LastSeenAt: now, LastSeenGeneration: 1,
		MetaStatus: domain.MetaReady, PreviewStatus: domain.PreviewReady,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Photos().Insert(ctx, ph))
	return db, now
}

func TestDocumentHasEverySection(t *testing.T) {
	db, now := seedFixture(t)
	agg := diag.New(diag.Deps{
		DB: db, Errors: diag.NewErrors(), StartedAt: now.Add(-time.Hour),
		Presence: fakePresence{online: map[string]bool{"living_room_tv": true}},
		Now:      func() time.Time { return now },
		Widgets: func(context.Context) map[string]any {
			return map[string]any{"weather": map[string]any{"enabled": false}}
		},
	})
	doc, err := agg.Collect(t.Context())
	require.NoError(t, err)

	assert.NotEmpty(t, doc.Version)
	assert.EqualValues(t, 3600, doc.UptimeSeconds)
	assert.True(t, doc.DB.PathRedacted)
	assert.Positive(t, doc.DB.Migrations)
	require.Len(t, doc.Sources, 1)
	assert.Equal(t, "family_photos", doc.Sources[0].ID)
	assert.EqualValues(t, 1, doc.Photos["ready"])
	assert.Contains(t, doc.Photos, "preview_failed")
	assert.Contains(t, doc.Photos, "preview_evicted")
	require.Len(t, doc.Screens, 1)
	assert.True(t, doc.Screens[0].Online, "presence comes from the session hub")
	assert.True(t, doc.Screens[0].Registered)
	assert.Contains(t, doc.Commands.Last24h, "applied")
	assert.Contains(t, doc.Commands.Last24h, "unknown")
	assert.Contains(t, doc.Widgets, "weather")
	assert.NotNil(t, doc.RecentErrors)
	assert.NotNil(t, doc.Jobs)
	assert.NotNil(t, doc.Cache)
}

// The diagnostics document is what an operator pastes into a bug report, so it
// must never carry a credential, a NAS path or a photo's location (design §15).
func TestDocumentLeaksNoSecretsOrPaths(t *testing.T) {
	db, now := seedFixture(t)
	ring := diag.NewErrors()
	log := slog.New(ring)
	log.With("component", "media").Error("preview failed",
		"code", "decode_failed", "photo_id", "01JPHOTO", "rel_path", "2026/private/holiday.jpg")

	agg := diag.New(diag.Deps{DB: db, Errors: ring, StartedAt: now, Now: func() time.Time { return now }})
	doc, err := agg.Collect(t.Context())
	require.NoError(t, err)

	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	body := string(raw)
	for _, forbidden := range []string{
		"atr_scr_", "atr_adm_", "Authorization", "token_hash",
		"/Volumes/photos/family", "holiday.jpg", "rel_path", "root_path",
	} {
		assert.NotContains(t, body, forbidden, "diagnostics must not contain %q", forbidden)
	}
	// The failure itself is still reported, by code and by ID.
	require.Len(t, doc.RecentErrors, 1)
	assert.Equal(t, "media", doc.RecentErrors[0].Component)
	assert.Equal(t, "decode_failed", doc.RecentErrors[0].Code)
	assert.Equal(t, "01JPHOTO", doc.RecentErrors[0].PhotoID)
}

func TestErrorRingKeepsTheNewestHundred(t *testing.T) {
	ring := diag.NewErrors()
	log := slog.New(ring).With("component", "indexer")
	for i := 0; i < diag.RingSize+20; i++ {
		log.Error("scan failed", "code", "scan_error", "source_id", "family_photos")
	}
	recent := ring.Recent()
	assert.Len(t, recent, diag.RingSize)
	assert.Equal(t, "indexer", recent[0].Component)
	assert.Equal(t, "scan_error", recent[0].Code)
	assert.Equal(t, "family_photos", recent[0].SourceID)
}

func TestErrorRingIgnoresInfoAndBelow(t *testing.T) {
	ring := diag.NewErrors()
	log := slog.New(ring).With("component", "app")
	log.Info("listening", "addr", "0.0.0.0:8443")
	log.Debug("cache touch", "photo_id", "01J")
	log.Warn("source offline", "code", "root_missing", "source_id", "family_photos")

	recent := ring.Recent()
	require.Len(t, recent, 1, "only warnings and errors are operational failures")
	assert.Equal(t, "root_missing", recent[0].Code)
}

// A record logged without an explicit code still identifies itself.
func TestErrorRingFallsBackToTheEventName(t *testing.T) {
	ring := diag.NewErrors()
	slog.New(ring).With("component", "ws").Error("write failed", "event", "write_failed")
	recent := ring.Recent()
	require.Len(t, recent, 1)
	assert.Equal(t, "write_failed", recent[0].Code)
	assert.False(t, strings.Contains(recent[0].Code, " "))
}

func TestAggregatorWithoutCollaboratorsStillRenders(t *testing.T) {
	agg := diag.New(diag.Deps{Now: time.Now})
	doc, err := agg.Collect(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, doc.Version)
	assert.Empty(t, doc.Screens)
}
