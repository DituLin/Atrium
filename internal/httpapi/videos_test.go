package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestVideosListGetPairingPaginationAndRevocation(t *testing.T) {
	h := newPhotoHarness(t)
	h.manager.Get(photoSourceID).Config.IncludeExtensions = []string{"jpg", "mp4"}
	admin := h.newAdminToken()
	token := h.pairScreen(admin, "video_tv", "Video TV")
	ctx := context.Background()
	ids := []string{}
	for i, name := range []string{"private-name-a.mp4", "private-name-b.mp4"} {
		v, err := h.db.Videos().Observe(ctx, domain.VideoObservation{SourceID: photoSourceID, RelPath: name, SizeBytes: 1, Generation: 1}, h.clock.Now().Add(time.Duration(i)*time.Second))
		require.NoError(t, err)
		ids = append(ids, v.ID)
	}
	require.Equal(t, http.StatusUnauthorized, h.request("GET", "/api/v1/videos", "").StatusCode)
	resp := h.request("GET", "/api/v1/videos?limit=1", "", bearer(token))
	require.Equal(t, 200, resp.StatusCode)
	type page struct {
		Items []struct {
			ID string `json:"id"`
		}
		NextCursor *string `json:"next_cursor"`
	}
	first := decode[page](t, resp)
	require.Len(t, first.Items, 1)
	require.Equal(t, ids[1], first.Items[0].ID)
	require.NotNil(t, first.NextCursor)
	resp = h.request("GET", "/api/v1/videos?limit=1&cursor="+*first.NextCursor, "", bearer(token))
	second := decode[page](t, resp)
	require.Len(t, second.Items, 1)
	require.Equal(t, ids[0], second.Items[0].ID)
	require.Nil(t, second.NextCursor)
	resp = h.request("GET", "/api/v1/videos/"+ids[0], "", bearer(token))
	require.Equal(t, 200, resp.StatusCode)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-name")
	require.NotContains(t, string(raw), "/mnt/")
	require.NoError(t, h.db.Exclusions().Add(ctx, &domain.Exclusion{SourceID: photoSourceID, MatchKind: domain.MatchPath, Pattern: "private-name-a.mp4", CreatedAt: h.clock.Now()}))
	require.Equal(t, 404, h.request("GET", "/api/v1/videos/"+ids[0], "", bearer(token)).StatusCode)
	require.NoError(t, h.db.Sources().Revoke(ctx, photoSourceID, "test", h.clock.Now()))
	require.Equal(t, 404, h.request("GET", "/api/v1/videos/"+ids[1], "", bearer(token)).StatusCode)
	require.Empty(t, decode[page](t, h.request("GET", "/api/v1/videos", "", bearer(token))).Items)
}

func TestVideoRetryRequiresAdminAndCurrentRevision(t *testing.T) {
	h := newPhotoHarness(t)
	h.manager.Get(photoSourceID).Config.IncludeExtensions = []string{"jpg", "mp4"}
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "retry_tv", "Retry TV")
	ctx := context.Background()
	now := h.clock.Now()
	v, err := h.db.Videos().Observe(ctx, domain.VideoObservation{SourceID: photoSourceID, RelPath: "retry.mp4", SizeBytes: 1, Generation: 1}, now)
	require.NoError(t, err)
	task, err := h.db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	ok, err := h.db.VideoWork().FailUnsupported(ctx, *task, now)
	require.NoError(t, err)
	require.True(t, ok)
	url := "/api/v1/videos/" + v.ID + "/retry"
	require.Equal(t, 401, h.request("POST", url, `{"revision":1}`).StatusCode)
	require.Equal(t, 403, h.request("POST", url, `{"revision":1}`, bearer(screen)).StatusCode)
	require.Equal(t, 400, h.request("POST", url, `{}`, bearer(admin)).StatusCode)
	response := h.request("POST", url, `{"revision":1}`, bearer(admin))
	require.Equal(t, 202, response.StatusCode)
	body := decode[struct {
		Revision int64  `json:"revision"`
		Status   string `json:"status"`
	}](t, response)
	require.EqualValues(t, 2, body.Revision)
	require.Equal(t, "pending", body.Status)
	require.Equal(t, 409, h.request("POST", url, `{"revision":1}`, bearer(admin)).StatusCode)
	require.NoError(t, h.db.Exclusions().Add(ctx, &domain.Exclusion{SourceID: photoSourceID, MatchKind: domain.MatchPath, Pattern: "retry.mp4", CreatedAt: now}))
	require.Equal(t, 404, h.request("POST", url, `{"revision":2}`, bearer(admin)).StatusCode)
}
