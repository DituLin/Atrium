package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLivePhotoClassificationClosesVideoRoutesAndPreservesTheStill(t *testing.T) {
	h := newPhotoHarness(t)
	h.manager.Get(photoSourceID).Config.IncludeExtensions = []string{"jpg", "mov"}
	screen := h.pairScreen(h.newAdminToken(), "live_photo_tv", "Live Photo TV")
	ctx := context.Background()
	now := h.clock.Now()
	photo := h.seedPhoto(t, seedOptions{RelPath: "live.jpg", WithFile: true})
	before, err := h.db.Photos().Get(ctx, photo.ID)
	require.NoError(t, err)

	// Model a companion that was already published as an ordinary video,
	// including a ticket issued before the new classification was known.
	live, err := h.db.Videos().Observe(ctx, domain.VideoObservation{
		SourceID: photoSourceID, RelPath: "live.mov", SizeBytes: 100, Generation: 1,
	}, now)
	require.NoError(t, err)
	task, err := h.db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, live.ID, task.Video.ID)
	shortMetadata := domain.VideoMetadata{
		Container: "mov", VideoCodec: "h264", Width: 32, Height: 32, DurationMS: 1500,
	}
	published, err := h.db.VideoWork().Publish(ctx, *task, shortMetadata, 100, 32, 32, now)
	require.NoError(t, err)
	require.True(t, published)
	ticketResponse := h.request(http.MethodGet, "/api/v1/media/videos/"+live.ID+"/ticket", "", bearer(screen))
	require.Equal(t, http.StatusOK, ticketResponse.StatusCode)
	oldTicket := decode[struct {
		Ticket string `json:"ticket"`
	}](t, ticketResponse).Ticket
	require.NotEmpty(t, oldTicket)

	retried, err := h.db.VideoWork().RequestRetry(ctx, live.ID, live.Revision, now)
	require.NoError(t, err)
	require.True(t, retried)
	task, err = h.db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, live.ID, task.Video.ID)
	published, err = h.db.VideoWork().PublishLivePhoto(ctx, *task, now)
	require.NoError(t, err)
	require.True(t, published)

	// A normal MOV with the same brief duration must retain its video entry.
	short, err := h.db.Videos().Observe(ctx, domain.VideoObservation{
		SourceID: photoSourceID, RelPath: "ordinary-short.mov", SizeBytes: 100, Generation: 1,
	}, now.Add(time.Second))
	require.NoError(t, err)
	task, err = h.db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, short.ID, task.Video.ID)
	published, err = h.db.VideoWork().Publish(ctx, *task, shortMetadata, 100, 32, 32, now)
	require.NoError(t, err)
	require.True(t, published)

	t.Run("list", func(t *testing.T) {
		response := h.request(http.MethodGet, "/api/v1/videos?limit=1", "", bearer(screen))
		require.Equal(t, http.StatusOK, response.StatusCode)
		page := decode[struct {
			Items []struct {
				ID       string               `json:"id"`
				Metadata domain.VideoMetadata `json:"metadata"`
			} `json:"items"`
			NextCursor *string `json:"next_cursor"`
		}](t, response)
		require.Len(t, page.Items, 1)
		assert.Equal(t, short.ID, page.Items[0].ID)
		assert.EqualValues(t, 1500, page.Items[0].Metadata.DurationMS)
		assert.Nil(t, page.NextCursor, "hidden companions must not produce an extra page")
	})

	for _, route := range []struct{ name, method, path string }{
		{"detail", http.MethodGet, "/api/v1/videos/" + live.ID},
		{"cover", http.MethodGet, "/api/v1/media/videos/" + live.ID + "/cover"},
		{"cover head", http.MethodHead, "/api/v1/media/videos/" + live.ID + "/cover"},
		{"ticket", http.MethodGet, "/api/v1/media/videos/" + live.ID + "/ticket"},
		{"content", http.MethodGet, "/api/v1/media/videos/" + live.ID + "/content"},
		{"content head", http.MethodHead, "/api/v1/media/videos/" + live.ID + "/content"},
	} {
		t.Run(route.name, func(t *testing.T) {
			response := h.request(route.method, route.path, "", bearer(screen))
			assert.Equal(t, http.StatusNotFound, response.StatusCode)
		})
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run("old ticket "+method, func(t *testing.T) {
			response := h.request(method, "/api/v1/media/videos/"+live.ID+"/content", "", func(r *http.Request) {
				r.Header.Set(auth.MediaTicketHeader, oldTicket)
			})
			assert.Equal(t, http.StatusNotFound, response.StatusCode)
		})
	}

	t.Run("ordinary short MOV", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, h.request(http.MethodGet, "/api/v1/videos/"+short.ID, "", bearer(screen)).StatusCode)
		assert.Equal(t, http.StatusOK, h.request(http.MethodGet, "/api/v1/media/videos/"+short.ID+"/ticket", "", bearer(screen)).StatusCode)
	})
	t.Run("still photo", func(t *testing.T) {
		after, err := h.db.Photos().Get(ctx, photo.ID)
		require.NoError(t, err)
		assert.Equal(t, before, after, "video classification must not change the still photo index")
		photos := h.list(t, "?collection=all", screen)
		require.Len(t, photos.Items, 1)
		assert.Equal(t, photo.ID, photos.Items[0].ID)
		assert.Equal(t, http.StatusOK, h.request(http.MethodGet, "/api/v1/media/photos/"+photo.ID+"?variant=preview", "", bearer(screen)).StatusCode)
	})
}
