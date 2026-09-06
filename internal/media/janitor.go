package media

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// JanitorInterval is the periodic sweep cadence from design §4.1.
const JanitorInterval = 10 * time.Minute

// EvictionTarget is the fraction of the budget the janitor evicts down to, so
// a single sweep frees enough room for a batch of new previews rather than
// re-running on every write.
const EvictionTarget = 0.90

// AccessTouchInterval throttles last_access_at writes to one per file per
// hour, which keeps slideshow traffic from turning into a write storm.
const AccessTouchInterval = time.Hour

// CacheState is what diagnostics report about the preview cache.
type CacheState struct {
	Bytes         int64  `json:"bytes"`
	BudgetBytes   int64  `json:"budget_bytes"`
	FreeDiskBytes int64  `json:"free_disk_bytes"`
	PausedReason  string `json:"paused_reason,omitempty"`
	Evicted       int64  `json:"evicted_total"`
}

// PausedLowDisk is the diagnostics reason when previews are on hold.
const PausedLowDisk = "low_disk"

// janitorState is the mutable part of the janitor.
type janitorState struct {
	mu      sync.Mutex
	running bool
	evicted int64
	touched map[string]time.Time
}

// Touch records a cache access, at most once per file per hour.
func (p *Pipeline) Touch(ctx context.Context, photoID string, variant domain.Variant) {
	now := p.now()
	key := photoID + "/" + string(variant)
	p.janitor.mu.Lock()
	if p.janitor.touched == nil {
		p.janitor.touched = map[string]time.Time{}
	}
	last, ok := p.janitor.touched[key]
	if ok && now.Sub(last) < AccessTouchInterval {
		p.janitor.mu.Unlock()
		return
	}
	p.janitor.touched[key] = now
	p.janitor.mu.Unlock()

	if err := p.opts.DB.Previews().Touch(ctx, photoID, variant, now); err != nil {
		p.opts.Logger.Debug("cache touch failed", "component", "media",
			"event", "touch_failed", "photo_id", photoID, "error", err.Error())
	}
}

// CacheState reports the current cache accounting.
func (p *Pipeline) CacheState(ctx context.Context) CacheState {
	state := CacheState{BudgetBytes: p.opts.Storage.CacheBudgetBytes}
	if used, err := p.opts.DB.Previews().TotalBytes(ctx); err == nil {
		state.Bytes = used
	}
	if free, _, err := p.opts.Disk.Free(p.opts.Cache.Root()); err == nil {
		state.FreeDiskBytes = free
		if p.opts.Storage.MinFreeBytes > 0 && free < p.opts.Storage.MinFreeBytes {
			state.PausedReason = PausedLowDisk
		}
	}
	p.janitor.mu.Lock()
	state.Evicted = p.janitor.evicted
	p.janitor.mu.Unlock()
	return state
}

// RunJanitor enforces the cache budget once. It is safe to call from several
// goroutines: concurrent invocations collapse into the one already running.
func (p *Pipeline) RunJanitor(ctx context.Context) {
	p.janitor.mu.Lock()
	if p.janitor.running {
		p.janitor.mu.Unlock()
		return
	}
	p.janitor.running = true
	p.janitor.mu.Unlock()
	defer func() {
		p.janitor.mu.Lock()
		p.janitor.running = false
		p.janitor.mu.Unlock()
	}()

	evicted, err := p.evict(ctx)
	if err != nil {
		p.opts.Logger.Warn("cache eviction failed", "component", "media",
			"event", "eviction_failed", "error", err.Error())
		return
	}
	if evicted > 0 {
		p.janitor.mu.Lock()
		p.janitor.evicted += evicted
		p.janitor.mu.Unlock()
		p.opts.Logger.Info("cache evicted", "component", "media",
			"event", "cache_evicted", "files", evicted)
		p.publish(domain.TopicPhotos)
	}
}

// evict removes least recently used files until the cache is under the target.
func (p *Pipeline) evict(ctx context.Context) (int64, error) {
	budget := p.opts.Storage.CacheBudgetBytes
	if budget <= 0 {
		return 0, nil
	}
	used, err := p.opts.DB.Previews().TotalBytes(ctx)
	if err != nil {
		return 0, err
	}
	if used <= budget {
		return 0, nil
	}
	target := int64(float64(budget) * EvictionTarget)

	var count int64
	previews := p.opts.DB.Previews()
	for used > target {
		batch, err := previews.LRU(ctx, 200)
		if err != nil {
			return count, err
		}
		if len(batch) == 0 {
			return count, nil
		}
		for i := range batch {
			if used <= target {
				break
			}
			f := batch[i]
			if err := p.opts.Cache.Remove(f.RelPath); err != nil {
				p.opts.Logger.Warn("cache file not removed", "component", "media",
					"event", "cache_remove_failed", "photo_id", f.PhotoID, "error", err.Error())
			}
			if err := previews.Delete(ctx, f.PhotoID, f.Variant); err != nil {
				return count, err
			}
			// The index survives eviction: the photo is still authorized, it
			// simply has no cached bytes right now (PRD §5.2).
			if err := p.opts.DB.Photos().SetPreviewEvicted(ctx, f.PhotoID, p.now()); err != nil &&
				!errors.Is(err, domain.ErrNotFound) {
				return count, err
			}
			used -= f.Bytes
			count++
		}
	}
	return count, nil
}

// RunJanitorLoop sweeps the cache periodically until ctx is cancelled.
func (p *Pipeline) RunJanitorLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = JanitorInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.RunJanitor(ctx)
		}
	}
}
