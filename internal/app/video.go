package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/video"
)

// buildVideo reserves an existing cache partition, including when video source
// extensions were disabled but local covers still occupy disk.
func (r *Runtime) buildVideo() error {
	enabled := false
	for _, s := range r.cfg.Sources {
		for _, ext := range s.IncludeExtensions {
			switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".") {
			case "mp4", "mov":
				enabled = true
			}
		}
	}
	budget := min(int64(512<<20), max(int64(1), r.cfg.Storage.CacheBudgetBytes/10))
	cache := video.NewCoverCache(filepath.Join(r.layout.Cache(), "video-covers"), budget, r.cfg.Storage.MinFreeBytes, nil)
	used, err := cache.Bytes()
	if err != nil {
		return fmt.Errorf("app: inspect video cache: %w", err)
	}
	if !enabled && used == 0 {
		return nil
	}
	if budget >= r.cfg.Storage.CacheBudgetBytes {
		return fmt.Errorf("app: cache budget too small for video partition")
	}
	if err := cache.TrimToBudget(); err != nil {
		return fmt.Errorf("app: reduce video cache: %w", err)
	}
	cache.SetSharedBudget(r.cfg.Storage.CacheBudgetBytes, func() (int64, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		return r.db.Previews().TotalBytes(ctx)
	})
	r.videoBudget, r.videoCache = budget, cache
	if !enabled {
		return nil
	}
	probe, ffmpeg := r.cfg.Media.Video.FFprobe, r.cfg.Media.Video.FFmpeg
	if probe == "" {
		probe = "ffprobe"
	}
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	r.videoWorker, err = video.NewWorker(video.WorkerOptions{
		DB: r.db, Sources: r.sources, Processor: video.Tools{FFprobe: probe, FFmpeg: ffmpeg}, Cache: cache, Now: r.now,
	})
	return err
}
