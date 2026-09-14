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
