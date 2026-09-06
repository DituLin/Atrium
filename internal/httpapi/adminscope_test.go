package httpapi_test

import (
	"context"
	"github.com/DituLin/Atrium/internal/widget"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
)

func TestSourcesListIsAdminOnlyAndHidesRootPath(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodGet, "/api/v1/sources", "", bearer(screen))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	raw := decodeRaw(t, h.harness, "/api/v1/sources", admin)
	assert.NotContains(t, raw, "/mnt/photos", "root_path never leaves the server")
	assert.Contains(t, raw, `"identity_bound":true`)
	assert.Contains(t, raw, `"health":"online"`)
}

func TestNASStatusHidesDetailFromScreens(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "Living room")

	h.fakeFS.Missing = true
	h.manager.Probe(context.Background(), h.manager.Get(photoSourceID))

	screenBody := decodeRaw(t, h.harness, "/api/v1/nas/status", screen)
	assert.Contains(t, screenBody, `"health":"offline"`)
	assert.NotContains(t, screenBody, "health_detail")
	assert.NotContains(t, screenBody, "stuck_ops")

	// The admin view carries the diagnostic detail the operator needs.
	h.composer.InvalidatePhotoCache()
	adminBody := decodeRaw(t, h.harness, "/api/v1/nas/status", admin)
	assert.Contains(t, adminBody, `"health_detail":"root_missing"`)
	assert.Contains(t, adminBody, "identity_confirmed")
}

func TestHomePhotoWidgetReportsLibraryState(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	h.seedPhoto(t, seedOptions{RelPath: "a.jpg", WithFile: true})
	h.seedPhoto(t, seedOptions{RelPath: "b.jpg", Status: domain.PhotoPending, Preview: domain.PreviewPending})
	h.seedPhoto(t, seedOptions{RelPath: "c.jpg", Status: domain.PhotoUnsupported})

	body := decode[widget.Snapshot](t, h.request(http.MethodGet, "/api/v1/home", "", bearer(admin)))

	assert.EqualValues(t, 1, body.Photo.Totals.Ready)
	assert.EqualValues(t, 1, body.Photo.Totals.PendingPreview)
	assert.EqualValues(t, 1, body.Photo.Totals.Unsupported)
	assert.EqualValues(t, 1, body.Photo.NewToday)
	assert.EqualValues(t, 1, body.Photo.UnknownCaptured)
	assert.Equal(t, "idle", body.Photo.Index.State)
}

func TestPhotoAdminRouteIsTheOnlyPlaceAPathAppears(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "Living room")
	photo := h.seedPhoto(t, seedOptions{RelPath: "album/holiday.jpg", WithFile: true})

	public := decodeRaw(t, h.harness, "/api/v1/photos/"+photo.ID, screen)
	assert.NotContains(t, public, "album/holiday.jpg")

	adminBody := decodeRaw(t, h.harness, "/api/v1/photos/"+photo.ID+"/admin", admin)
	assert.Contains(t, adminBody, `"rel_path":"album/holiday.jpg"`)

	// The route is admin-only.
	resp := h.request(http.MethodGet, "/api/v1/photos/"+photo.ID+"/admin", "", bearer(screen))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestExcludePhotoHidesItImmediatelyAndPersistsTheRule(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	ctx := context.Background()
	photo := h.seedPhoto(t, seedOptions{RelPath: "album/private.jpg", WithFile: true})

	resp := h.request(http.MethodPost, "/api/v1/photos/"+photo.ID+"/exclude", `{"reason":"operator"}`, bearer(admin))
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	assert.Equal(t, http.StatusNotFound,
		h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(admin)).StatusCode)

	rules, err := h.db.Exclusions().List(ctx, photoSourceID)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, domain.MatchPath, rules[0].MatchKind)
	assert.Equal(t, "album/private.jpg", rules[0].Pattern)

	// Including it again drops the rule and re-queues the pipeline.
	resp = h.request(http.MethodPost, "/api/v1/photos/"+photo.ID+"/include", "", bearer(admin))
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	rules, err = h.db.Exclusions().List(ctx, photoSourceID)
	require.NoError(t, err)
	assert.Empty(t, rules)
	open, err := h.db.Jobs().HasOpen(ctx, domain.JobExtractMeta, photo.ID)
	require.NoError(t, err)
	assert.True(t, open)
}

func TestExclusionPrefixHidesEverythingBeneathIt(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	inside := h.seedPhoto(t, seedOptions{RelPath: "private/a.jpg", WithFile: true})
	nearMiss := h.seedPhoto(t, seedOptions{RelPath: "privateer/a.jpg", WithFile: true})

	resp := h.request(http.MethodPost, "/api/v1/exclusions",
		`{"source_id":"`+photoSourceID+`","prefix":"private/","reason":"operator"}`, bearer(admin))
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	assert.Equal(t, http.StatusNotFound,
		h.request(http.MethodGet, mediaPath(inside.ID, "preview"), "", bearer(admin)).StatusCode)
	assert.Equal(t, http.StatusOK,
		h.request(http.MethodGet, mediaPath(nearMiss.ID, "preview"), "", bearer(admin)).StatusCode,
		"a prefix must match on path segments, not on characters")

	list := decode[struct {
		Exclusions []struct {
			ID      string `json:"id"`
			Pattern string `json:"pattern"`
		} `json:"exclusions"`
	}](t, h.request(http.MethodGet, "/api/v1/exclusions", "", bearer(admin)))
	require.Len(t, list.Exclusions, 1)
	assert.Equal(t, "private", list.Exclusions[0].Pattern)

	del := h.request(http.MethodDelete, "/api/v1/exclusions/"+list.Exclusions[0].ID, "", bearer(admin))
	assert.Equal(t, http.StatusNoContent, del.StatusCode)
}

func TestExclusionRejectsTraversalPatterns(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	resp := h.request(http.MethodPost, "/api/v1/exclusions",
		`{"source_id":"`+photoSourceID+`","prefix":"../etc"}`, bearer(admin))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = h.request(http.MethodPost, "/api/v1/exclusions", `{"source_id":"nope","prefix":"a"}`, bearer(admin))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSourceRevokeAndRestore(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", WithFile: true})

	resp := h.request(http.MethodPost, "/api/v1/sources/"+photoSourceID+"/revoke", "", bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, http.StatusNotFound,
		h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(admin)).StatusCode)

	resp = h.request(http.MethodPost, "/api/v1/sources/"+photoSourceID+"/restore", "", bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, http.StatusOK,
		h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(admin)).StatusCode)
}

func TestSourceRebindClearsIdentityMismatch(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	ctx := context.Background()

	h.fakeFS.SetVolume(sourceVolume())
	h.manager.Probe(ctx, h.manager.Get(photoSourceID))
	require.True(t, h.manager.Get(photoSourceID).IdentityMismatch())

	resp := h.request(http.MethodPost, "/api/v1/sources/"+photoSourceID+"/rebind-identity", "", bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.False(t, h.manager.Get(photoSourceID).IdentityMismatch())
}

func TestPhotoRetryRequeuesThePipeline(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	ctx := context.Background()
	photo := h.seedPhoto(t, seedOptions{
		RelPath: "bad.jpg", Status: domain.PhotoUnsupported, Preview: domain.PreviewFailed,
	})

	resp := h.request(http.MethodPost, "/api/v1/photos/"+photo.ID+"/retry", "", bearer(admin))
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	got, err := h.db.Photos().Get(ctx, photo.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.PhotoPending, got.Status)
	assert.Equal(t, domain.PreviewPending, got.PreviewStatus)
	assert.Zero(t, got.PreviewAttempts)
	open, err := h.db.Jobs().HasOpen(ctx, domain.JobExtractMeta, photo.ID)
	require.NoError(t, err)
	assert.True(t, open)
}

func TestAdminMutationsAreAudited(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{RelPath: "a.jpg"})

	h.request(http.MethodPost, "/api/v1/photos/"+photo.ID+"/exclude", "", bearer(admin))
	h.request(http.MethodPost, "/api/v1/sources/"+photoSourceID+"/revoke", "", bearer(admin))

	entries, err := h.db.Audit().List(context.Background(), 50)
	require.NoError(t, err)
	actions := map[string]bool{}
	for _, e := range entries {
		actions[e.Action] = true
	}
	assert.True(t, actions["photo.exclude"])
	assert.True(t, actions["source.revoke"])
}

func TestScansListAndGet(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	ctx := context.Background()
	run := &domain.ScanRun{SourceID: photoSourceID, Mode: domain.ScanScheduled}
	require.NoError(t, h.db.ScanRuns().Start(ctx, run))
	require.NoError(t, h.db.ScanRuns().Finish(ctx, run.ID, domain.ScanCompleted, "", h.clock.Now()))

	list := decode[struct {
		Scans []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"scans"`
	}](t, h.request(http.MethodGet, "/api/v1/scans?source_id="+photoSourceID, "", bearer(admin)))
	require.Len(t, list.Scans, 1)
	assert.Equal(t, "completed", list.Scans[0].Status)

	resp := h.request(http.MethodGet, "/api/v1/scans/"+run.ID, "", bearer(admin))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, http.StatusNotFound,
		h.request(http.MethodGet, "/api/v1/scans/unknown", "", bearer(admin)).StatusCode)
}
