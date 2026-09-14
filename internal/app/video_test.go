package app

import (
	"context"
	"github.com/DituLin/Atrium/internal/domain"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/stretchr/testify/require"
)

func TestVideoRuntimeReservesTotalCacheAndBuildsWorker(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "photos", true: "mixed"}[enabled], func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Home.Timezone = "Asia/Singapore"
			cfg.Server.TLS.Mode = config.TLSOff
			cfg.Server.Listen = "127.0.0.1:0"
			cfg.Storage.DataDir = filepath.Join(t.TempDir(), "data")
			cfg.Storage.CacheBudgetBytes = 100 << 20
			ext := []string{"jpg"}
			if enabled {
				ext = append(ext, "mp4")
			}
			cfg.Sources = []config.Source{{ID: "mixed", Name: "Mixed", Root: t.TempDir(), IncludeExtensions: ext, Identity: config.Identity{AllowLocal: true}, MaxInflight: 2, IOTimeout: config.Duration(1000000000), Scan: config.Scan{Interval: config.Duration(60000000000), StabilityInterval: config.Duration(1000000000), StabilityChecks: 2, StabilityMaxRound: 4}}}
			var sampleID string
			probe, probeErr := exec.LookPath("ffprobe")
			ffmpeg, ffmpegErr := exec.LookPath("ffmpeg")
			if enabled && probeErr == nil && ffmpegErr == nil {
				cfg.Media.Video.FFprobe, cfg.Media.Video.FFmpeg = probe, ffmpeg
				output, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=32x32:r=1", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", filepath.Join(cfg.Sources[0].Root, "sample.mp4")).CombinedOutput()
				require.NoError(t, err, string(output))
			}
			rt, err := New(context.Background(), Options{Config: cfg, Logger: logging.Discard()})
			require.NoError(t, err)
			if enabled && probeErr == nil && ffmpegErr == nil {
				require.Equal(t, domain.HealthOnline, rt.sources.Probe(context.Background(), rt.sources.Get("mixed")).Health)
				info, err := os.Stat(filepath.Join(cfg.Sources[0].Root, "sample.mp4"))
				require.NoError(t, err)
				v, err := rt.db.Videos().Observe(context.Background(), domain.VideoObservation{SourceID: "mixed", RelPath: "sample.mp4", SizeBytes: info.Size(), MtimeUnix: info.ModTime().Unix(), Generation: 1}, time.Now())
				require.NoError(t, err)
				sampleID = v.ID
			}
			require.NoError(t, rt.Start(context.Background()))
			t.Cleanup(func() { require.NoError(t, rt.Shutdown()) })
			require.Equal(t, enabled, rt.videoWorker != nil)
			require.Equal(t, cfg.Storage.CacheBudgetBytes, rt.pipeline.CacheState(context.Background()).BudgetBytes)
			if enabled {
				require.EqualValues(t, 10<<20, rt.videoBudget)
				require.NotNil(t, rt.videoCache)
				if sampleID != "" {
					require.Eventually(t, func() bool {
						v, err := rt.db.Videos().Get(context.Background(), sampleID)
						return err == nil && v.Status == domain.VideoReady
					}, 10*time.Second, 20*time.Millisecond, "runtime must process queued video without manual RunOnce")
					state := rt.pipeline.CacheState(context.Background())
					require.Positive(t, state.Bytes)
				} else {
					t.Log("ffmpeg/ffprobe unavailable: real runtime processing not exercised")
				}

			}
		})
	}
}

func TestVideoRuntimeTrimsDisabledSourceCoversWhenBudgetShrinks(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.CacheBudgetBytes = 100
	rt := &Runtime{cfg: cfg, layout: NewLayout(t.TempDir())}
	root := filepath.Join(rt.layout.Cache(), "video-covers")
	require.NoError(t, os.MkdirAll(root, 0700))
	for range 2 {
		require.NoError(t, os.WriteFile(filepath.Join(root, domain.NewID()+".jpg"), []byte("123456"), 0600))
	}
	require.NoError(t, rt.buildVideo())
	require.Nil(t, rt.videoWorker)
	require.EqualValues(t, 10, rt.videoBudget)
	used, err := rt.videoCache.Bytes()
	require.NoError(t, err)
	require.LessOrEqual(t, used, rt.videoBudget)
}
