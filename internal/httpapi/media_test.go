package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/domain"
)

func mediaPath(id, variant string) string {
	return "/api/v1/media/photos/" + id + "?variant=" + variant
}

func TestMediaServesPreviewBytesWithETag(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", WithFile: true})

	resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"))
	assert.Equal(t, `"`+photo.Fingerprint+`"`, resp.Header.Get("ETag"))
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotEmpty(t, body)

	// The conditional request short-circuits.
	etag := resp.Header.Get("ETag")
	again := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token),
		func(r *http.Request) { r.Header.Set("If-None-Match", etag) })
	assert.Equal(t, http.StatusNotModified, again.StatusCode)
}

func TestMediaTouchesAccessTimeForLRU(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", WithFile: true})

	before, err := h.db.Previews().Get(context.Background(), photo.ID, domain.VariantPreview)
	require.NoError(t, err)
	resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.False(t, before.LastAccessAt.IsZero())
}

func TestMediaAcceptsOnlyKnownVariants(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", WithFile: true})

	resp := h.request(http.MethodGet, mediaPath(photo.ID, "original"), "", bearer(token))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, string(domain.CodeInvalidRequest), errorCode(t, resp))

	// The route is ID-only: a path can never be smuggled in as the ID.
	resp = h.request(http.MethodGet, "/api/v1/media/photos/..%2F..%2Fetc%2Fpasswd", "", bearer(token))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestMediaReturns202WhilePreviewIsPending(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{
		RelPath: "a.jpg", Status: domain.PhotoPending, Preview: domain.PreviewPending,
	})

	resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token))
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, "5", resp.Header.Get("Retry-After"))
	body := decode[map[string]any](t, resp)
	assert.Equal(t, "preview_processing", body["status"])
}

func TestMediaEvictedPreviewIsRebuiltOnDemand(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", Preview: domain.PreviewEvicted})

	resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token))
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	ctx := context.Background()
	open, err := h.db.Jobs().HasOpen(ctx, domain.JobBuildPreview, photo.ID)
	require.NoError(t, err)
	assert.True(t, open, "an evicted preview is rebuilt on demand, the index is kept")

	// A second request must not queue a duplicate.
	h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token))
	counts, err := h.db.Jobs().Counts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, counts[domain.JobQueued])
}

func TestMediaReturns503WhenSourceOfflineAndNothingCached(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", Preview: domain.PreviewEvicted})

	h.fakeFS.Missing = true
	h.manager.Probe(context.Background(), h.manager.Get(photoSourceID))

	resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token))
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, string(domain.CodePreviewUnavailable), errorCode(t, resp))
}

func TestMediaReturns404ForRemovedExcludedAndRevoked(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	ctx := context.Background()

	removed := h.seedPhoto(t, seedOptions{RelPath: "removed.jpg", Status: domain.PhotoRemoved, WithFile: true})
	excluded := h.seedPhoto(t, seedOptions{RelPath: "excluded.jpg", Status: domain.PhotoExcluded, WithFile: true})
	live := h.seedPhoto(t, seedOptions{RelPath: "live.jpg", WithFile: true})

	for _, id := range []string{removed.ID, excluded.ID} {
		resp := h.request(http.MethodGet, mediaPath(id, "preview"), "", bearer(token))
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	}
	require.Equal(t, http.StatusOK,
		h.request(http.MethodGet, mediaPath(live.ID, "preview"), "", bearer(token)).StatusCode)

	require.NoError(t, h.db.Sources().Revoke(ctx, photoSourceID, "test", time.Now()))
	resp := h.request(http.MethodGet, mediaPath(live.ID, "preview"), "", bearer(token))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode,
		"revoking a source stops the media route immediately")
}

func TestMediaFailedPreviewIsNotFound(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{
		RelPath: "bad.jpg", Status: domain.PhotoUnsupported, Preview: domain.PreviewFailed,
	})
	resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestMediaIsReachableWithAScreenCookie(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	screenToken := h.pairScreen(admin, "living_room_tv", "Living room")
	photo := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", WithFile: true})

	resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "",
		cookie(screenToken), origin(testOrigin))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// The web client probes a preview with `Range: bytes=0-0` before committing an
// <img> element, so it never paints a broken image. `http.ServeContent`
// answers a satisfiable range with 206 and a single byte.
func TestMediaAnswersARangedProbeForAReadyPreview(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", WithFile: true})

	resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token),
		func(r *http.Request) { r.Header.Set("Range", "bytes=0-0") })
	require.Contains(t, []int{http.StatusOK, http.StatusPartialContent}, resp.StatusCode,
		"a ready preview must answer a probe with 200 or 206")
	assert.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"))
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if resp.StatusCode == http.StatusPartialContent {
		assert.Len(t, body, 1, "a one-byte range returns one byte")
		assert.NotEmpty(t, resp.Header.Get("Content-Range"))
	}
}

// The same probe against a preview that does not exist yet must not look like
// a broken image either: it is a 202 the client can retry.
func TestMediaRangedProbeOnPendingPreviewIsAccepted(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	photo := h.seedPhoto(t, seedOptions{RelPath: "b.jpg", Preview: domain.PreviewPending})

	resp := h.request(http.MethodGet, mediaPath(photo.ID, "preview"), "", bearer(token),
		func(r *http.Request) { r.Header.Set("Range", "bytes=0-0") })
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, "5", resp.Header.Get("Retry-After"))
}
