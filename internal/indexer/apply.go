package indexer

import (
	"context"
	"errors"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/DituLin/Atrium/internal/media"
)

// apply reconciles one observed file with its index row.
func (s *Scanner) apply(ctx context.Context, run *domain.ScanRun, c candidate, generation int64, mode domain.ScanMode) error {
	repo := s.photos()
	existing, err := repo.GetByPath(ctx, s.entry.ID, c.Rel)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return s.applyNew(ctx, run, c, generation)
	case err != nil:
		return err
	}

	switch existing.Status {
	case domain.PhotoExcluded:
		// The operator revoked this file; it stays visible to the scan only so
		// it is not treated as missing.
		return repo.TouchSeen(ctx, existing.ID, generation, s.now())
	case domain.PhotoRemoved:
		return s.applyRevived(ctx, run, c, existing, generation)
	default:
	}

	unchanged := existing.SizeBytes == c.Size && existing.MtimeUnix == c.Mtime
	if unchanged && mode != domain.ScanFull {
		s.stability.Forget(c.Rel)
		return repo.TouchSeen(ctx, existing.ID, generation, s.now())
	}
	if unchanged {
		// A full scan re-derives everything, but an unchanged file still only
		// needs its jobs re-queued, not its row rewritten.
		if err := repo.TouchSeen(ctx, existing.ID, generation, s.now()); err != nil {
			return err
		}
		return s.enqueue(ctx, existing.ID)
	}
	if !s.stability.Observe(c.Rel, c.Size, c.Mtime, s.now(), s.rules) {
		// The file is still changing; keep the old row alive meanwhile.
		return repo.TouchSeen(ctx, existing.ID, generation, s.now())
	}

	run.FilesChanged++
	if err := repo.MarkChanged(ctx, existing.ID, c.Size, c.Mtime, generation, s.now()); err != nil {
		return err
	}
	if err := s.previews().DeleteForPhoto(ctx, existing.ID); err != nil {
		return err
	}
	s.removeCacheFiles(existing.ID)
	return s.enqueue(ctx, existing.ID)
}

// applyNew adds a path the index has never seen.
func (s *Scanner) applyNew(ctx context.Context, run *domain.ScanRun, c candidate, generation int64) error {
	if !s.stability.Observe(c.Rel, c.Size, c.Mtime, s.now(), s.rules) {
		return nil
	}
	src, err := s.db.Sources().Get(ctx, s.entry.ID)
	if err != nil {
		return err
	}
	now := s.now()
	photo := &domain.Photo{
		SourceID: s.entry.ID, RelPath: c.Rel, Ext: c.Ext,
		SizeBytes: c.Size, MtimeUnix: c.Mtime,
		Status:      domain.PhotoPending,
		FirstSeenAt: now, LastSeenAt: now, LastSeenGeneration: generation,
		// The first complete import is a baseline: it must not read as "the
		// whole family archive was added today" (PRD §5.2).
		IsBaseline: src.BaselineCompleted == nil,
		CreatedAt:  now, UpdatedAt: now,
	}
	if err := s.photos().Insert(ctx, photo); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil
		}
		return err
	}
	run.FilesNew++
	s.progress.Indexed = run.FilesNew
	return s.enqueue(ctx, photo.ID)
}

// applyRevived brings a previously removed path back. It is a genuine
// discovery, so first_seen_at is reset and the baseline flag cleared.
func (s *Scanner) applyRevived(ctx context.Context, run *domain.ScanRun, c candidate,
	existing *domain.Photo, generation int64) error {
	if !s.stability.Observe(c.Rel, c.Size, c.Mtime, s.now(), s.rules) {
		return nil
	}
	if err := s.photos().Revive(ctx, existing.ID, c.Size, c.Mtime, generation, s.now()); err != nil {
		return err
	}
	run.FilesNew++
	s.progress.Indexed = run.FilesNew
	return s.enqueue(ctx, existing.ID)
}

// enqueue schedules metadata extraction, which chains the preview build.
func (s *Scanner) enqueue(ctx context.Context, photoID string) error {
	return s.queueIn().EnqueuePhoto(ctx, domain.JobExtractMeta, photoID, s.entry.ID, jobs.PriorityExtractMeta)
}

// removeCacheFiles deletes both cached variants of a photo, ignoring absence.
func (s *Scanner) removeCacheFiles(photoID string) {
	if s.cache == nil {
		return
	}
	for _, v := range []domain.Variant{domain.VariantPreview, domain.VariantThumb} {
		rel, err := media.CacheRelPath(v, photoID)
		if err != nil {
			continue
		}
		if err := s.cache.Remove(rel); err != nil {
			s.log.Debug("cache file not removed", "component", "indexer",
				"event", "cache_remove_failed", "photo_id", photoID, "error", err.Error())
		}
	}
}

// finalize commits a completed run: the generation advances, missing counters
// move and anything missing twice becomes removed (design §6.2 step 5).
func (s *Scanner) finalize(ctx context.Context, run *domain.ScanRun, generation int64) error {
	if !s.entry.Online() || s.entry.IdentityMismatch() {
		// The identity changed while the walk was running: the listing can no
		// longer be trusted to decide what is missing.
		run.Status = domain.ScanAborted
		_ = s.db.ScanRuns().Finish(ctx, run.ID, domain.ScanAborted, "identity_changed", s.now())
		return domain.Errorf(domain.CodeIdentityMismatch, "source identity changed during the scan")
	}
	now := s.now()
	missing, err := s.db.Photos().IncrementMissing(ctx, s.entry.ID, generation, now)
	if err != nil {
		return err
	}
	run.FilesMissing = missing

	removed, err := s.db.Photos().PromoteRemoved(ctx, s.entry.ID, now)
	if err != nil {
		return err
	}
	for _, id := range removed {
		if err := s.db.Previews().DeleteForPhoto(ctx, id); err != nil {
			return err
		}
		s.removeCacheFiles(id)
	}
	run.FilesRemoved = int64(len(removed))

	unsupported, err := s.db.Photos().CountBySourceStatus(ctx, s.entry.ID, domain.PhotoUnsupported)
	if err != nil {
		return err
	}
	run.FilesUnsupported = unsupported

	if err := s.db.Sources().CompleteScan(ctx, s.entry.ID, generation, now); err != nil {
		return err
	}
	run.Status = domain.ScanCompleted
	if err := s.db.ScanRuns().UpdateCounters(ctx, run); err != nil {
		return err
	}
	if err := s.db.ScanRuns().Finish(ctx, run.ID, domain.ScanCompleted, "", now); err != nil {
		return err
	}
	s.log.Info("scan completed", "component", "indexer", "event", "scan_completed",
		"source_id", s.entry.ID, "generation", generation,
		"seen", run.FilesSeen, "new", run.FilesNew, "changed", run.FilesChanged,
		"missing", run.FilesMissing, "removed", run.FilesRemoved, "errors", run.Errors)
	if run.FilesNew > 0 || run.FilesChanged > 0 || run.FilesRemoved > 0 {
		s.publish(domain.TopicPhotos, domain.TopicHome)
	} else {
		s.publish(domain.TopicHome)
	}
	return nil
}
