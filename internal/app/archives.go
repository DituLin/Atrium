package app

import (
	"context"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/archiveworker"
	"github.com/DituLin/Atrium/internal/domain"
	"os"
	"time"
)

func (r *Runtime) runArchives(ctx context.Context) {
	h, e := r.db.LocalHousehold(ctx)
	if e != nil {
		return
	}
	c := r.cfg.Archives
	w := archiveworker.Worker{Tasks: r.db.ArchiveTasksWithClock(clockFunc(r.now)), Blobs: r.archiveBlobs, Household: h.ID, Lease: time.Minute, Native: archive.NativeParser{HelperPath: c.NativeHelperPath}, Adapter: archive.OpenClawUnderstandingAdapter{NodePath: c.NodePath, OpenClawPath: c.OpenClawPath, PluginPath: c.WorkerPluginPath, ProviderPath: c.ProviderPath, APIKey: os.Getenv("DEEPSEEK_API_KEY")}}
	var lastEpoch, lastGeneration, lastIndex int64
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			var epoch, generation, index int64
			if e := r.db.SQL().QueryRowContext(ctx, `SELECT epoch,generation,index_revision FROM archive_search_state WHERE household_id=?`, h.ID).Scan(&epoch, &generation, &index); e == nil && (epoch != lastEpoch || generation != lastGeneration || index != lastIndex) {
				lastEpoch, lastGeneration, lastIndex = epoch, generation, index
				r.bus.Publish(domain.TopicArchives)
			}
			if _, e := r.db.ArchiveTasks().IndexArchives(ctx, h.ID, 32); e != nil && ctx.Err() == nil {
				r.log.Warn("archive search indexing deferred", "component", "archives")
			}
			if _, e := r.db.ProjectPendingLegacyArchives(ctx, 32); e != nil && ctx.Err() == nil {
				r.log.Warn("archive legacy projection failed", "component", "archives")
			}
			if _, e := w.RunOne(ctx); e != nil && ctx.Err() == nil {
				r.log.Warn("archive processing deferred", "component", "archives")
			}
		}
	}
}
