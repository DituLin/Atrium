package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
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
	contentURL := "/api/v1/media/videos/" + v.ID + "/content"
	original, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, 401, h.request("GET", contentURL, "").StatusCode)
	for _, test := range []struct {
		rangeValue string
		status     int
		expected   []byte
	}{
		{"", 200, original}, {"bytes=0-15", 206, original[:16]},
		{"bytes=-10", 206, original[len(original)-10:]},
		{fmt.Sprintf("bytes=%d-", len(original)-10), 206, original[len(original)-10:]},
		{"bytes=999999999-", 416, nil}, {"bytes=0-1,4-5", 416, nil}, {"bytes=invalid", 416, nil},
	} {
		response := h.request("GET", contentURL, "", bearer(token), func(r *http.Request) {
			if test.rangeValue != "" {
				r.Header.Set("Range", test.rangeValue)
			}
		})
		require.Equal(t, test.status, response.StatusCode, test.rangeValue)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		if test.status == 200 || test.status == 206 {
			require.Equal(t, test.expected, body)
			require.Equal(t, "video/mp4", response.Header.Get("Content-Type"))
			require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
		}
	}
	contentHead := h.request("HEAD", contentURL, "", bearer(token))
	require.Equal(t, 200, contentHead.StatusCode)
	require.Equal(t, fmt.Sprint(len(original)), contentHead.Header.Get("Content-Length"))
	headBody, err := io.ReadAll(contentHead.Body)
	require.NoError(t, err)
	require.Empty(t, headBody)
	for _, condition := range []struct {
		header, value string
		status        int
	}{
		{"If-None-Match", contentHead.Header.Get("ETag"), 304},
		{"If-Range", contentHead.Header.Get("ETag"), 206},
		{"If-Range", `"obsolete"`, 200},
	} {
		response := h.request("GET", contentURL, "", bearer(token), func(r *http.Request) {
			r.Header.Set("Range", "bytes=0-15")
			r.Header.Set(condition.header, condition.value)
		})
		require.Equal(t, condition.status, response.StatusCode)
		_, err := io.Copy(io.Discard, response.Body)
		require.NoError(t, err)
	}

	probeCtx, stopProbe := context.WithCancel(ctx)
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		for probeCtx.Err() == nil {
			manager.Probe(probeCtx, manager.Get("videos"))
			time.Sleep(time.Millisecond)
		}
	}()
	concurrent := make(chan error, 2)
	for range 2 {
		go func() {
			for range 5 {
				request, err := http.NewRequestWithContext(ctx, "GET", h.server.URL+contentURL, nil)
				if err != nil {
					concurrent <- err
					return
				}
				bearer(token)(request)
				response, err := h.server.Client().Do(request)
				if err != nil {
					concurrent <- err
					return
				}
				body, readErr := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if readErr != nil {
					concurrent <- fmt.Errorf("parallel video read: %w", readErr)
					return
				}
				if response.StatusCode != 200 || !bytes.Equal(body, original) {
					concurrent <- fmt.Errorf("parallel video response: status %d, bytes %d", response.StatusCode, len(body))
					return
				}
			}
			concurrent <- nil
		}()
	}
	firstError, secondError := <-concurrent, <-concurrent
	stopProbe()
	<-probeDone
	require.NoError(t, firstError)
	require.NoError(t, secondError)
	blocked := &cancelVideoFS{FS: manager.Get("videos").FS, entered: make(chan struct{}), release: make(chan struct{})}
	manager.Get("videos").FS = blocked
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(blocked.release) }) }
	defer unblock()
	requestCtx, requestCancel := context.WithCancel(ctx)
	request, err := http.NewRequestWithContext(requestCtx, "GET", h.server.URL+contentURL, nil)
	require.NoError(t, err)
	bearer(token)(request)
	requestResult := make(chan error, 1)
	go func() {
		response, err := h.server.Client().Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		requestResult <- err
	}()
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("content request did not reach source guard")
	}
	requestCancel()
	select {
	case err := <-requestResult:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled HTTP request did not return")
	}
	require.True(t, blocked.active.Load(), "cancel must not pretend blocked source I/O has ended")
	unblock()
	require.Eventually(t, func() bool { return !blocked.active.Load() }, time.Second, time.Millisecond)
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
	require.Equal(t, 503, h.request("GET", contentURL, "", bearer(token)).StatusCode)
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
	// Grow only the temporary test file to exceed the client's unread receive
	// window, ensuring revocation happens while the response is in flight.
	require.NoError(t, os.Truncate(file, 16<<20))
	largeInfo, err := os.Stat(file)
	require.NoError(t, err)
	_, err = h.db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "videos", RelPath: "private-name.mp4", SizeBytes: largeInfo.Size(), MtimeUnix: largeInfo.ModTime().Unix(), Generation: 2}, now)
	require.NoError(t, err)
	worked, err = worker.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, worked)
	playing := h.request("GET", contentURL, "", bearer(token))
	require.Equal(t, 200, playing.StatusCode)
	require.NoError(t, h.db.Sources().Revoke(ctx, "videos", "test", now))
	partial, readErr := io.ReadAll(playing.Body)
	require.Error(t, readErr, "revocation during transmission must truncate the response")
	require.Less(t, len(partial), 16<<20)

	require.Equal(t, 404, h.request("GET", contentURL, "", bearer(token)).StatusCode)
	require.Equal(t, 404, h.request("GET", url, "", bearer(token)).StatusCode)
}

// This wrapper blocks the real authorized source's file Stat while preserving
// its actual file operations and the earlier real processor publication.
type cancelVideoFS struct {
	source.FS
	entered, release chan struct{}
	once             sync.Once
	active           atomic.Bool
}

func (f *cancelVideoFS) Stat(ctx context.Context, rel string) (fs.FileInfo, error) {
	if rel == "private-name.mp4" {
		f.active.Store(true)
		f.once.Do(func() { close(f.entered) })
		<-f.release
		defer f.active.Store(false)
	}
	return f.FS.Stat(ctx, rel)
}
