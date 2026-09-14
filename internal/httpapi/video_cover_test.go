package httpapi_test

import (
	"bytes"
	"context"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/video"
	"github.com/stretchr/testify/require"
)

func TestVideoCoverFromRealWorkerChecksAuthorizationAndCacheLoss(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	root := t.TempDir()
	file := filepath.Join(root, "private-name.mp4")
	output, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=32x32:r=1", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", file).CombinedOutput()
	require.NoError(t, err, string(output))
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
	info, err := os.Stat(file)
	require.NoError(t, err)
	v, err := h.db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "videos", RelPath: "private-name.mp4", SizeBytes: info.Size(), MtimeUnix: info.ModTime().Unix(), Generation: 1}, now)
	require.NoError(t, err)
	worker, err := video.NewWorker(video.WorkerOptions{DB: h.db, Sources: manager, Processor: video.Tools{FFprobe: probe, FFmpeg: ffmpeg}, Cache: cache, Now: h.clock.Now})
	require.NoError(t, err)
	worked, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, worked)
	token := h.pairScreen(h.newAdminToken(), "cover_tv", "Cover TV")
	url := "/api/v1/media/videos/" + v.ID + "/cover"
	require.Equal(t, 401, h.request("GET", url, "").StatusCode)
	resp := h.request("GET", url, "", bearer(token))
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"))
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_, err = jpeg.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	head := h.request("HEAD", url, "", bearer(token))
	require.Equal(t, 200, head.StatusCode)
	require.Equal(t, resp.Header.Get("Content-Length"), head.Header.Get("Content-Length"))
	headBytes, err := io.ReadAll(head.Body)
	require.NoError(t, err)
	require.Empty(t, headBytes)
	require.NoError(t, h.db.Sources().SetHealth(ctx, "videos", domain.HealthOffline, "offline", false, now))
	require.Equal(t, 200, h.request("GET", url, "", bearer(token)).StatusCode)
	retained, err := h.db.VideoWork().RetainedTokens(ctx, now)
	require.NoError(t, err)
	for cover := range retained {
		require.NoError(t, cache.Remove(cover))
	}
	require.Equal(t, 503, h.request("GET", url, "", bearer(token)).StatusCode)
	require.NoError(t, h.db.Sources().SetHealth(ctx, "videos", domain.HealthOnline, "", true, now))
	require.Equal(t, 202, h.request("GET", url, "", bearer(token)).StatusCode)
	worked, err = worker.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, worked)
	require.Equal(t, 200, h.request("GET", url, "", bearer(token)).StatusCode)
	current, err := h.db.VideoWork().RetainedTokens(ctx, now)
	require.NoError(t, err)
	for cover := range current {
		require.NoError(t, cache.Write(cover, []byte("corrupt")))
	}
	require.Equal(t, 202, h.request("GET", url, "", bearer(token)).StatusCode)
	worked, err = worker.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, worked)
	require.Equal(t, 200, h.request("GET", url, "", bearer(token)).StatusCode)
	require.NoError(t, h.db.Sources().Revoke(ctx, "videos", "test", now))
	require.Equal(t, 404, h.request("GET", url, "", bearer(token)).StatusCode)
}
