package httpapi_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
)

type memoriesBody struct {
	Items []struct {
		ID           string `json:"id"`
		SourceID     string `json:"source_id"`
		ResumeCursor string `json:"resume_cursor"`
		URLs         struct {
			Preview string `json:"preview"`
		} `json:"urls"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Meta       struct {
		Collection string `json:"collection"`
		Day        string `json:"day"`
		ScreenID   string `json:"screen_id"`
		Total      int    `json:"total"`
	} `json:"meta"`
}

func memoryScreen(t *testing.T, h *photoHarness, id string) string {
	t.Helper()
	return h.pairScreen(h.newAdminToken(), id, "Synthetic screen")
}

func seedMemories(t *testing.T, h *photoHarness, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		h.seedPhoto(t, seedOptions{RelPath: fmt.Sprintf("memory-%03d.jpg", i)})
	}
}

func getMemories(t *testing.T, h *photoHarness, token, query string) memoriesBody {
	t.Helper()
	resp := h.request(http.MethodGet, "/api/v1/photos/memories"+query, "", bearer(token))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	return decode[memoriesBody](t, resp)
}

func memoryIDs(body memoriesBody) []string {
	ids := make([]string, 0, len(body.Items))
	for _, item := range body.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestMemoriesSameDayScreenIsStableAndPagesExactlyOnce(t *testing.T) {
	h := newPhotoHarness(t)
	token := memoryScreen(t, h, "memory_tv")
	seedMemories(t, h, 9)
	whole := getMemories(t, h, token, "?limit=100")
	assert.Equal(t, memoryIDs(whole), memoryIDs(getMemories(t, h, token, "?limit=100")))
	assert.Equal(t, "memories", whole.Meta.Collection)
	assert.Equal(t, "2026-09-05", whole.Meta.Day)
	assert.Equal(t, "memory_tv", whole.Meta.ScreenID)
	assert.Equal(t, 9, whole.Meta.Total)
	require.Len(t, whole.Items, 9)
	for _, item := range whole.Items {
		assert.NotEmpty(t, item.ResumeCursor)
		assert.Equal(t, "/api/v1/media/photos/"+item.ID+"?variant=preview", item.URLs.Preview)
	}
	var paged []string
	query := "?limit=2"
	for page := 0; page < 10; page++ {
		got := getMemories(t, h, token, query)
		paged = append(paged, memoryIDs(got)...)
		if got.NextCursor == nil {
			break
		}
		query = "?limit=2&cursor=" + url.QueryEscape(*got.NextCursor)
	}
	assert.Equal(t, memoryIDs(whole), paged)
}

func TestMemoriesNewHomeDayRestartsOldCursorWithANewOrder(t *testing.T) {
	h := newPhotoHarness(t)
	token := memoryScreen(t, h, "memory_tv")
	seedMemories(t, h, 20)
	yesterday := getMemories(t, h, token, "?limit=100")
	resume := yesterday.Items[8].ResumeCursor
	// Midnight Singapore occurs while the UTC date is still September 5.
	h.clock.Set(time.Date(2026, 9, 5, 16, 0, 0, 0, time.UTC))
	today := getMemories(t, h, token, "?limit=100")
	resumed := getMemories(t, h, token, "?limit=100&cursor="+url.QueryEscape(resume))
	assert.Equal(t, "2026-09-06", today.Meta.Day)
	assert.NotEqual(t, memoryIDs(yesterday), memoryIDs(today))
	assert.Equal(t, memoryIDs(today), memoryIDs(resumed))
	assert.Equal(t, today.Meta, resumed.Meta)
}

func TestMemoriesScreensHaveIndependentOrderAndRejectEachOthersCursor(t *testing.T) {
	h := newPhotoHarness(t)
	firstToken := memoryScreen(t, h, "memory_tv_a")
	secondToken := memoryScreen(t, h, "memory_tv_b")
	seedMemories(t, h, 20)
	first := getMemories(t, h, firstToken, "?limit=100")
	second := getMemories(t, h, secondToken, "?limit=100")
	assert.NotEqual(t, memoryIDs(first), memoryIDs(second))
	resp := h.request(http.MethodGet, "/api/v1/photos/memories?cursor="+url.QueryEscape(first.Items[0].ResumeCursor), "", bearer(secondToken))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, string(domain.CodeInvalidRequest), errorCode(t, resp))
}

func TestMemoriesResumeIncludesCurrentPhotoAndSkipsRemovedAnchor(t *testing.T) {
	h := newPhotoHarness(t)
	token := memoryScreen(t, h, "memory_tv")
	seedMemories(t, h, 9)
	whole := getMemories(t, h, token, "?limit=100")
	anchor := whole.Items[4]
	query := "?limit=100&cursor=" + url.QueryEscape(anchor.ResumeCursor)
	assert.Equal(t, memoryIDs(whole)[4:], memoryIDs(getMemories(t, h, token, query)))
	require.NoError(t, h.db.Photos().SetStatus(context.Background(), anchor.ID, domain.PhotoRemoved, h.clock.Now()))
	resumed := getMemories(t, h, token, query)
	assert.Equal(t, memoryIDs(whole)[5:], memoryIDs(resumed))
	assert.Equal(t, 8, resumed.Meta.Total)
}

func TestMemoriesNewPhotosDoNotReorderExistingPhotosOrShiftCursor(t *testing.T) {
	h := newPhotoHarness(t)
	token := memoryScreen(t, h, "memory_tv")
	seedMemories(t, h, 12)
	before := getMemories(t, h, token, "?limit=100")
	firstPage := getMemories(t, h, token, "?limit=4")
	require.NotNil(t, firstPage.NextCursor)
	h.seedPhoto(t, seedOptions{RelPath: "new-arrival.jpg"})
	after := getMemories(t, h, token, "?limit=100")
	existing := map[string]bool{}
	for _, id := range memoryIDs(before) {
		existing[id] = true
	}
	var afterExisting []string
	for _, id := range memoryIDs(after) {
		if existing[id] {
			afterExisting = append(afterExisting, id)
		}
	}
	assert.Equal(t, memoryIDs(before), afterExisting)
	continued := getMemories(t, h, token, "?limit=100&cursor="+url.QueryEscape(*firstPage.NextCursor))
	var continuedExisting []string
	for _, id := range memoryIDs(continued) {
		if existing[id] {
			continuedExisting = append(continuedExisting, id)
		}
	}
	assert.Equal(t, memoryIDs(before)[4:], continuedExisting)
}

func TestMemoriesMixesActiveSourcesAndImmediatelyExcludesRevokedOrUnreadyPhotos(t *testing.T) {
	h := newPhotoHarness(t)
	token := memoryScreen(t, h, "memory_tv")
	ctx := context.Background()
	require.NoError(t, h.db.Sources().Upsert(ctx, "second_album", "Second synthetic album", "/synthetic/second", h.clock.Now()))
	for i := 0; i < 4; i++ {
		original := h.seedPhoto(t, seedOptions{RelPath: fmt.Sprintf("source-%d.jpg", i)})
		second := *original
		second.ID, second.SourceID = "", "second_album"
		require.NoError(t, h.db.Photos().Insert(ctx, &second))
	}
	h.seedPhoto(t, seedOptions{RelPath: "pending-preview.jpg", Preview: domain.PreviewPending})
	h.seedPhoto(t, seedOptions{RelPath: "pending-photo.jpg", Status: domain.PhotoPending})
	h.seedPhoto(t, seedOptions{RelPath: "excluded.jpg", Status: domain.PhotoExcluded})
	before := getMemories(t, h, token, "?limit=100")
	assert.Equal(t, 8, before.Meta.Total)
	require.Len(t, before.Items, 8)
	sources := map[string]int{}
	for _, item := range before.Items {
		sources[item.SourceID]++
	}
	assert.Equal(t, map[string]int{photoSourceID: 4, "second_album": 4}, sources)
	require.NoError(t, h.db.Sources().Revoke(ctx, "second_album", "Synthetic revocation", h.clock.Now()))
	// A saved cursor never restores access to a source that was revoked.
	for _, item := range before.Items {
		resumed := getMemories(t, h, token, "?limit=100&cursor="+url.QueryEscape(item.ResumeCursor))
		assert.Equal(t, 4, resumed.Meta.Total)
		for _, visible := range resumed.Items {
			assert.Equal(t, photoSourceID, visible.SourceID)
		}
	}
	after := getMemories(t, h, token, "?limit=100")
	var expected []string
	for _, item := range before.Items {
		if item.SourceID == photoSourceID {
			expected = append(expected, item.ID)
		}
	}
	assert.Equal(t, expected, memoryIDs(after))
}

func TestMemoriesEmptyAndSinglePhotoHaveFiniteResponses(t *testing.T) {
	h := newPhotoHarness(t)
	token := memoryScreen(t, h, "memory_tv")
	empty := getMemories(t, h, token, "")
	assert.NotNil(t, empty.Items)
	assert.Empty(t, empty.Items)
	assert.Zero(t, empty.Meta.Total)
	assert.Nil(t, empty.NextCursor)
	photo := h.seedPhoto(t, seedOptions{RelPath: "single.jpg"})
	single := getMemories(t, h, token, "?limit=1")
	require.Len(t, single.Items, 1)
	assert.Equal(t, photo.ID, single.Items[0].ID)
	assert.Nil(t, single.NextCursor)
	assert.Equal(t, memoryIDs(single), memoryIDs(getMemories(t, h, token, "?cursor="+url.QueryEscape(single.Items[0].ResumeCursor))))
}

func TestMemoriesRequiresLiveScreenCredential(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	token := h.pairScreen(admin, "memory_tv", "Synthetic screen")
	seedMemories(t, h, 1)
	for _, auth := range []func(*http.Request){func(*http.Request) {}, bearer("invalid"), bearer(admin)} {
		resp := h.request(http.MethodGet, "/api/v1/photos/memories", "", auth)
		assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, resp.StatusCode)
	}
	require.NoError(t, h.db.Screens().Revoke(context.Background(), "memory_tv", h.clock.Now()))
	resp := h.request(http.MethodGet, "/api/v1/photos/memories", "", bearer(token))
	assert.Equal(t, http.StatusGone, resp.StatusCode)
	assert.Equal(t, string(domain.CodeScreenRevoked), errorCode(t, resp))
}

func TestMemoriesStrictlyValidatesQueryAndCursor(t *testing.T) {
	h := newPhotoHarness(t)
	token := memoryScreen(t, h, "memory_tv")
	seedMemories(t, h, 1)
	valid := map[string]any{"v": 1, "day": "2026-09-05", "screen": "memory_tv", "anchor": domain.NewID(), "inclusive": true}
	encode := func(value any) string {
		blob, err := json.Marshal(value)
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(blob)
	}
	queries := []string{"?limit=0", "?limit=-1", "?limit=101", "?limit=1.5", "?limit=abc", "?limit=", "?limit=1&limit=2", "?screen=other", "?cursor=not-base64!!", "?cursor=" + strings.Repeat("A", 2049), "?cursor=" + encode(nil), "?cursor=" + encode(map[string]any{}), "?cursor=x&cursor=y"}
	for _, field := range []string{"v", "day", "screen", "anchor", "inclusive"} {
		missing := map[string]any{}
		for key, value := range valid {
			if key != field {
				missing[key] = value
			}
		}
		queries = append(queries, "?cursor="+encode(missing))
	}
	for _, change := range []map[string]any{{"v": 2}, {"day": "2026-02-30"}, {"day": "2026-9-5"}, {"screen": "other_tv"}, {"anchor": "../photo"}, {"anchor": strings.Repeat("a", 129)}, {"inclusive": "true"}, {"extra": "value"}} {
		invalid := map[string]any{}
		for key, value := range valid {
			invalid[key] = value
		}
		for key, value := range change {
			invalid[key] = value
		}
		queries = append(queries, "?cursor="+encode(invalid))
	}
	queries = append(queries, "?cursor="+base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"v":1,"day":"2026-09-05","screen":"memory_tv","anchor":"photo","inclusive":true}`)))
	for _, query := range queries {
		t.Run(query[:min(len(query), 90)], func(t *testing.T) {
			resp := h.request(http.MethodGet, "/api/v1/photos/memories"+query, "", bearer(token))
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			assert.Equal(t, string(domain.CodeInvalidRequest), errorCode(t, resp))
		})
	}
}
