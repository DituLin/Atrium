package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
)

type listBody struct {
	Items []struct {
		ID                 string  `json:"id"`
		SourceID           string  `json:"source_id"`
		CapturedAt         *string `json:"captured_at"`
		CapturedConfidence string  `json:"captured_confidence"`
		FirstSeenAt        string  `json:"first_seen_at"`
		IsBaseline         bool    `json:"is_baseline"`
		Preview            struct {
			Status string `json:"status"`
			Width  int    `json:"width"`
		} `json:"preview"`
		URLs struct {
			Preview string `json:"preview"`
			Thumb   string `json:"thumb"`
		} `json:"urls"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Meta       struct {
		Collection           string `json:"collection"`
		UnknownCapturedCount int64  `json:"unknown_captured_count"`
		BaselineOnly         bool   `json:"baseline_only"`
		Day                  string `json:"day"`
	} `json:"meta"`
}

func (p *photoHarness) list(t *testing.T, query, token string) listBody {
	t.Helper()
	resp := p.request(http.MethodGet, "/api/v1/photos"+query, "", bearer(token))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return decode[listBody](t, resp)
}

func TestPhotosRecentExcludesBaselineAndOrdersByDiscovery(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	base := h.clock.Now()

	h.seedPhoto(t, seedOptions{RelPath: "old.jpg", IsBaseline: true, FirstSeenAt: base.Add(-72 * time.Hour)})
	first := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", FirstSeenAt: base.Add(-2 * time.Hour)})
	second := h.seedPhoto(t, seedOptions{RelPath: "b.jpg", FirstSeenAt: base.Add(-time.Hour)})

	got := h.list(t, "?collection=recent", token)
	require.Len(t, got.Items, 2)
	assert.Equal(t, second.ID, got.Items[0].ID, "newest discovery first")
	assert.Equal(t, first.ID, got.Items[1].ID)
	assert.Nil(t, got.NextCursor)
	assert.Equal(t, "recent", got.Meta.Collection)
	assert.Contains(t, got.Items[0].URLs.Preview, "/api/v1/media/photos/"+second.ID)
	assert.NotContains(t, got.Items[0].URLs.Preview, "b.jpg", "no path ever reaches a screen")
}

func TestPhotosRecentBaselineOnlyEmptyState(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	h.seedPhoto(t, seedOptions{RelPath: "history.jpg", IsBaseline: true})

	got := h.list(t, "?collection=recent", token)
	assert.Empty(t, got.Items)
	assert.True(t, got.Meta.BaselineOnly, "the client shows 'nothing new since the import' rather than 'no photos'")
}

func TestPhotosKeysetPaginationVisitsEveryItemOnce(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	base := h.clock.Now()
	for i := 0; i < 7; i++ {
		h.seedPhoto(t, seedOptions{
			RelPath:     "p" + string(rune('a'+i)) + ".jpg",
			FirstSeenAt: base.Add(-time.Duration(i) * time.Minute),
		})
	}

	seen := map[string]bool{}
	query := "?collection=recent&limit=3"
	for pages := 0; pages < 10; pages++ {
		got := h.list(t, query, token)
		for _, item := range got.Items {
			assert.False(t, seen[item.ID], "a photo must not appear on two pages")
			seen[item.ID] = true
		}
		if got.NextCursor == nil {
			break
		}
		query = "?collection=recent&limit=3&cursor=" + *got.NextCursor
	}
	assert.Len(t, seen, 7)
}

func TestPhotosCapturedTodayUsesTheHomeDayWindow(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	// The fake clock is 2026-09-05T12:00Z, which is 2026-09-05 20:00 in
	// Singapore, so the home day runs 2026-09-04T16:00Z .. 2026-09-05T16:00Z.
	inside := time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)
	alsoInside := time.Date(2026, 9, 5, 15, 59, 0, 0, time.UTC)
	before := time.Date(2026, 9, 4, 15, 59, 0, 0, time.UTC)
	after := time.Date(2026, 9, 5, 16, 0, 0, 0, time.UTC)

	h.seedPhoto(t, seedOptions{RelPath: "start.jpg", CapturedAt: &inside, Confidence: domain.CapturedExact})
	h.seedPhoto(t, seedOptions{RelPath: "end.jpg", CapturedAt: &alsoInside, Confidence: domain.CapturedExact})
	h.seedPhoto(t, seedOptions{RelPath: "yesterday.jpg", CapturedAt: &before, Confidence: domain.CapturedExact})
	h.seedPhoto(t, seedOptions{RelPath: "tomorrow.jpg", CapturedAt: &after, Confidence: domain.CapturedExact})
	h.seedPhoto(t, seedOptions{RelPath: "undated.jpg"})

	got := h.list(t, "?collection=captured_today", token)
	require.Len(t, got.Items, 2, "the window is left-closed and right-open in the home timezone")
	assert.Equal(t, "2026-09-05", got.Meta.Day)
	assert.EqualValues(t, 1, got.Meta.UnknownCapturedCount, "undated photos are counted, not hidden silently")
	// Oldest first inside the day.
	assert.Less(t, *got.Items[0].CapturedAt, *got.Items[1].CapturedAt)
}

func TestPhotosAllSortsUndatedLast(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	newer := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	older := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	h.seedPhoto(t, seedOptions{RelPath: "older.jpg", CapturedAt: &older, Confidence: domain.CapturedExact})
	h.seedPhoto(t, seedOptions{RelPath: "undated.jpg"})
	h.seedPhoto(t, seedOptions{RelPath: "newer.jpg", CapturedAt: &newer, Confidence: domain.CapturedExact})

	got := h.list(t, "?collection=all", token)
	require.Len(t, got.Items, 3)
	assert.NotNil(t, got.Items[0].CapturedAt)
	assert.NotNil(t, got.Items[1].CapturedAt)
	assert.Nil(t, got.Items[2].CapturedAt, "photos with no capture time sort last")
}

func TestPhotosRandomRoundHasNoRepeatsAndEndsCleanly(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	for i := 0; i < 5; i++ {
		h.seedPhoto(t, seedOptions{RelPath: "r" + string(rune('a'+i)) + ".jpg"})
	}

	first := h.list(t, "?collection=random&limit=2", token)
	require.Len(t, first.Items, 2)
	require.NotNil(t, first.NextCursor)

	second := h.list(t, "?collection=random&limit=2&cursor="+*first.NextCursor, token)
	require.Len(t, second.Items, 2)
	require.NotNil(t, second.NextCursor)

	third := h.list(t, "?collection=random&limit=2&cursor="+*second.NextCursor, token)
	require.Len(t, third.Items, 1)
	assert.Nil(t, third.NextCursor, "next_cursor is null at the end of a round")

	seen := map[string]bool{}
	for _, page := range []listBody{first, second, third} {
		for _, item := range page.Items {
			assert.False(t, seen[item.ID], "a round must not repeat a photo")
			seen[item.ID] = true
		}
	}
	assert.Len(t, seen, 5)
}

func TestPhotosRandomIsStableForASeed(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	for i := 0; i < 6; i++ {
		h.seedPhoto(t, seedOptions{RelPath: "s" + string(rune('a'+i)) + ".jpg"})
	}
	first := h.list(t, "?collection=random&limit=3", token)
	require.NotNil(t, first.NextCursor)

	// Replaying the same cursor twice must give the same page.
	a := h.list(t, "?collection=random&limit=3&cursor="+*first.NextCursor, token)
	b := h.list(t, "?collection=random&limit=3&cursor="+*first.NextCursor, token)
	require.Len(t, a.Items, 3)
	for i := range a.Items {
		assert.Equal(t, a.Items[i].ID, b.Items[i].ID)
	}
}

func TestPhotosLimitIsClampedAndValidated(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	for i := 0; i < 3; i++ {
		h.seedPhoto(t, seedOptions{RelPath: "l" + string(rune('a'+i)) + ".jpg"})
	}
	assert.Len(t, h.list(t, "?collection=all&limit=1000", token).Items, 3)

	resp := h.request(http.MethodGet, "/api/v1/photos?limit=0", "", bearer(token))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, string(domain.CodeInvalidRequest), errorCode(t, resp))

	resp = h.request(http.MethodGet, "/api/v1/photos?collection=nope", "", bearer(token))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = h.request(http.MethodGet, "/api/v1/photos?cursor=not-base64!!", "", bearer(token))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPhotosOnlyReadyAndAuthorizedItemsAreListed(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	h.seedPhoto(t, seedOptions{RelPath: "ready.jpg"})
	h.seedPhoto(t, seedOptions{RelPath: "pending.jpg", Status: domain.PhotoPending})
	h.seedPhoto(t, seedOptions{RelPath: "removed.jpg", Status: domain.PhotoRemoved})
	h.seedPhoto(t, seedOptions{RelPath: "excluded.jpg", Status: domain.PhotoExcluded})

	got := h.list(t, "?collection=all", token)
	require.Len(t, got.Items, 1)
	assert.Equal(t, "ready", got.Items[0].Preview.Status)
}

func TestPhotoItemWithNeighbors(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	base := h.clock.Now()
	newest := h.seedPhoto(t, seedOptions{RelPath: "c.jpg", FirstSeenAt: base})
	middle := h.seedPhoto(t, seedOptions{RelPath: "b.jpg", FirstSeenAt: base.Add(-time.Hour)})
	oldest := h.seedPhoto(t, seedOptions{RelPath: "a.jpg", FirstSeenAt: base.Add(-2 * time.Hour)})

	resp := h.request(http.MethodGet, "/api/v1/photos/"+middle.ID+"?neighbors=recent", "", bearer(token))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decode[struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
		Neighbors struct {
			Collection string  `json:"collection"`
			PreviousID *string `json:"previous_id"`
			NextID     *string `json:"next_id"`
		} `json:"neighbors"`
	}](t, resp)
	assert.Equal(t, middle.ID, body.Item.ID)
	require.NotNil(t, body.Neighbors.PreviousID)
	require.NotNil(t, body.Neighbors.NextID)
	assert.Equal(t, newest.ID, *body.Neighbors.PreviousID)
	assert.Equal(t, oldest.ID, *body.Neighbors.NextID)
}

func TestPhotoItemHidesIneligibleAndUnknownIDs(t *testing.T) {
	h := newPhotoHarness(t)
	token := h.newAdminToken()
	removed := h.seedPhoto(t, seedOptions{RelPath: "gone.jpg", Status: domain.PhotoRemoved})

	resp := h.request(http.MethodGet, "/api/v1/photos/"+removed.ID, "", bearer(token))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = h.request(http.MethodGet, "/api/v1/photos/01JUNKNOWN", "", bearer(token))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
