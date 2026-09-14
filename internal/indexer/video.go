package indexer

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
)

func (s *Scanner) videos() *store.Videos {
	if s.tx != nil {
		return s.db.Videos().WithTx(s.tx)
	}
	return s.db.Videos()
}

func (s *Scanner) applyVideo(ctx context.Context, run *domain.ScanRun, c candidate, generation int64) error {
	repo := s.videos()
	old, err := repo.GetByPath(ctx, s.entry.ID, c.Rel)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	same := old != nil && old.SizeBytes == c.Size && old.MtimeUnix == c.Mtime && old.Status != domain.VideoRemoved
	o := domain.VideoObservation{SourceID: s.entry.ID, RelPath: c.Rel, SizeBytes: c.Size, MtimeUnix: c.Mtime, Generation: generation}
	if same || (old != nil && old.Status == domain.VideoExcluded) {
		s.stability.Forget(c.Rel)
	} else if !s.stability.Observe(c.Rel, c.Size, c.Mtime, s.now(), s.rules) {
		if old == nil || old.Status == domain.VideoRemoved {
			return nil
		}
		// Preserve presence while the new file settles. The reader must validate
		// the stored file tuple before serving this older revision.
		o.SizeBytes, o.MtimeUnix = old.SizeBytes, old.MtimeUnix
		_, err = repo.Observe(ctx, o, s.now())
		return err
	}
	current, err := repo.Observe(ctx, o, s.now())
	if err != nil {
		return err
	}
	if old == nil || old.Status == domain.VideoRemoved {
		run.FilesNew++
		s.progress.Indexed = run.FilesNew
	} else if current.Revision != old.Revision {
		run.FilesChanged++
	}
	// Video processing claims pending revisions separately, never photo jobs.
	return nil
}

func (s *Scanner) completeMediaScan(ctx context.Context, generation int64, now time.Time, run *domain.ScanRun) error {
	allowed := s.entry.Extensions()
	var extensions []string
	for _, ext := range []string{"mp4", "mov"} {
		if allowed[ext] {
			extensions = append(extensions, ext)
		}
	}
	var missing, removed int64
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		// A skipped directory cannot establish absence. Keep video removal state
		// untouched on partial scans, while retaining any observed new files.
		if run.Errors == 0 && len(extensions) > 0 {
			if err := s.db.Videos().WithTx(tx).CompleteScanExtensions(ctx, s.entry.ID, generation, extensions, now); err != nil {
				return err
			}
			var err error
			missing, removed, err = s.db.Videos().WithTx(tx).RemovalCounts(ctx, s.entry.ID, generation)
			if err != nil {
				return err
			}
		}
		return s.db.Sources().CompleteScanInTx(ctx, tx, s.entry.ID, generation, now)
	})
	if err == nil {
		run.FilesMissing += missing
		run.FilesRemoved += removed
	}
	return err
}
