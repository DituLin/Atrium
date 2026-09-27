package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/video"
	"github.com/stretchr/testify/require"
)

func TestMediaTicketAuthorizesOnlyItsVideoStreamWhileTheScreenIsActive(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	root := t.TempDir()
	for _, name := range []string{"a.mp4", "b.mp4"} {
		output, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=32x32:r=1", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", filepath.Join(root, name)).CombinedOutput()
		require.NoError(t, err, string(output))
	}
	cache := video.NewCoverCache(t.TempDir(), 1<<20, 0, nil)
	var manager *source.Manager
	h := newHarnessWith(t, func(h *harness, deps *httpapi.Deps) {
		var err error
		manager, err = source.NewManager(source.ManagerOptions{Config: h.cfg, DB: h.db, Now: h.clock.Now})
		require.NoError(t, err)
		deps.Sources = manager
		deps.VideoCache = cache
	}, func(cfg *config.Config) {
		cfg.Sources = []config.Source{{ID: "videos", Name: "Videos", Root: root, IncludeExtensions: []string{"mp4"}, Identity: config.Identity{AllowLocal: true}, MaxInflight: 2, IOTimeout: config.Duration(time.Second), Scan: config.Scan{Interval: config.Duration(time.Minute), StabilityInterval: config.Duration(time.Second), StabilityChecks: 2, StabilityMaxRound: 4}}}
	})
	ctx := context.Background()
	now := h.clock.Now()
	require.NoError(t, h.db.Sources().Upsert(ctx, "videos", "Videos", root, now))
	require.Equal(t, domain.HealthOnline, manager.Probe(ctx, manager.Get("videos")).Health)
	ids := []string{}
	for _, name := range []string{"a.mp4", "b.mp4"} {
		info, err := os.Stat(filepath.Join(root, name))
		require.NoError(t, err)
		v, err := h.db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "videos", RelPath: name, SizeBytes: info.Size(), MtimeUnix: info.ModTime().Unix(), Generation: 1}, now)
		require.NoError(t, err)
		ids = append(ids, v.ID)
	}
	worker, err := video.NewWorker(video.WorkerOptions{DB: h.db, Sources: manager, Processor: video.Tools{FFprobe: probe, FFmpeg: ffmpeg}, Cache: cache, Now: h.clock.Now})
	require.NoError(t, err)
	for range ids {
		worked, err := worker.RunOnce(ctx)
		require.NoError(t, err)
		require.True(t, worked)
	}
	admin := h.newAdminToken()
	token := h.pairScreen(admin, "ticket_tv", "Ticket TV")
	ticketURL := "/api/v1/media/videos/" + ids[0] + "/ticket"
	require.Equal(t, 401, h.request("GET", ticketURL, "").StatusCode)
	require.Equal(t, 403, h.request("GET", ticketURL, "", bearer(admin)).StatusCode)

	resp := h.request("GET", ticketURL, "", bearer(token))
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	issued := decode[map[string]string](t, resp)
	require.Equal(t, auth.MediaTicketHeader, issued["header"])
	require.NotEmpty(t, issued["expires_at"])
	ticket := issued["ticket"]
	require.NotContains(t, ticket, "a.mp4")
	withTicket := func(value string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set(auth.MediaTicketHeader, value) }
	}

	original, err := os.ReadFile(filepath.Join(root, "a.mp4"))
	require.NoError(t, err)
	stream := h.request("GET", "/api/v1/media/videos/"+ids[0]+"/content", "", withTicket(ticket))
	require.Equal(t, 200, stream.StatusCode)
	body, err := io.ReadAll(stream.Body)
	require.NoError(t, err)
	require.Equal(t, original, body)

	// The ticket is not a screen credential: another video, other routes and
	// a tampered ticket are all refused.
	require.Equal(t, 403, h.request("GET", "/api/v1/media/videos/"+ids[1]+"/content", "", withTicket(ticket)).StatusCode)
	require.Equal(t, 401, h.request("GET", "/api/v1/media/videos/"+ids[0]+"/cover", "", withTicket(ticket)).StatusCode)
	require.Equal(t, 401, h.request("GET", "/api/v1/home", "", withTicket(ticket)).StatusCode)
	require.Equal(t, 401, h.request("GET", "/api/v1/media/videos/"+ids[0]+"/content", "", withTicket(ticket+"x")).StatusCode)
	require.Equal(t, 404, h.request("GET", "/api/v1/media/videos/missing/ticket", "", bearer(token)).StatusCode)

	require.Equal(t, 200, h.request("DELETE", "/api/v1/screens/ticket_tv", "", bearer(admin)).StatusCode)
	revoked := h.request("GET", "/api/v1/media/videos/"+ids[0]+"/content", "", withTicket(ticket))
	require.Equal(t, http.StatusGone, revoked.StatusCode)
	require.Equal(t, "screen_revoked", errorCode(t, revoked))
}
