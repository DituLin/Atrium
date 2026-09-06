package indexer

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/source"
)

// candidate is one file the walk accepted for indexing.
type candidate struct {
	Rel   string
	Ext   string
	Size  int64
	Mtime int64
}

// execute walks the tree, applies each candidate and then runs the stability
// rounds. It is separated from Scan so the failure bookkeeping lives in one
// place.
func (s *Scanner) execute(ctx context.Context, run *domain.ScanRun, generation int64, mode domain.ScanMode) error {
	exclusions, err := LoadExclusions(ctx, s.db, s.entry.ID)
	if err != nil {
		return err
	}
	allowed := s.entry.Extensions()

	seenPaths := make(map[string]struct{}, 512)
	b := newBatch(ctx, s)
	defer b.rollback()
	err = s.walk(ctx, run, exclusions, allowed, func(c candidate) error {
		seenPaths[c.Rel] = struct{}{}
		return b.run(func() error { return s.apply(ctx, run, c, generation, mode) })
	}, b.flush)
	if err != nil {
		return err
	}
	if err := b.flush(); err != nil {
		return err
	}
	return s.stabilityRounds(ctx, run, generation, mode, seenPaths)
}

// walk performs a breadth-first traversal. Breadth first keeps the working set
// to one directory listing per level, which matters on a share where each
// ReadDir is a network round trip.
// flush is called before the scan counters are written so the counter update
// never contends with the write lock an open index batch is holding.
func (s *Scanner) walk(ctx context.Context, run *domain.ScanRun, exclusions *ExclusionSet,
	allowed map[string]bool, visit func(candidate) error, flush func() error) error {
	queue := []string{""}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir := queue[0]
		queue = queue[1:]

		entries, err := s.entry.FS.ReadDir(ctx, dir)
		if err != nil {
			// A stuck or degraded share aborts the whole run: continuing would
			// produce a partial listing that the removal rule must never see.
			if errors.Is(err, source.ErrStuck) || errors.Is(err, source.ErrDegraded) {
				return err
			}
			if dir == "" {
				return err
			}
			// One unreadable subdirectory is counted and skipped so a single
			// permission problem cannot stop the whole library (FR-08).
			run.Errors++
			s.log.Debug("directory unreadable", "component", "indexer",
				"event", "readdir_failed", "source_id", s.entry.ID, "error", err.Error())
			continue
		}

		for _, entry := range entries {
			rel := source.JoinRel(dir, entry.Name())
			if entry.Type()&fs.ModeSymlink != 0 {
				// Symbolic links are never followed, in either direction.
				continue
			}
			if exclusions.Excluded(rel) {
				s.stability.Forget(rel)
				continue
			}
			if entry.IsDir() {
				queue = append(queue, rel)
				continue
			}
			ext := extensionOf(entry.Name())
			if !allowed[ext] {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				run.Errors++
				continue
			}
			run.FilesSeen++
			s.progress.Seen = run.FilesSeen
			if err := visit(candidate{
				Rel: rel, Ext: ext, Size: info.Size(), Mtime: info.ModTime().Unix(),
			}); err != nil {
				return err
			}
			if run.FilesSeen%ProgressFlushEvery == 0 {
				if err := flush(); err != nil {
					return err
				}
				if err := s.db.ScanRuns().UpdateCounters(ctx, run); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// stabilityRounds re-stats the candidates that were still settling at the end
// of the walk, up to stability_max_rounds. Anything unresolved carries over to
// the next scan rather than being indexed half-written.
func (s *Scanner) stabilityRounds(ctx context.Context, run *domain.ScanRun, generation int64,
	mode domain.ScanMode, seen map[string]struct{}) error {
	b := newBatch(ctx, s)
	defer b.rollback()
	for round := 0; round < s.maxRounds; round++ {
		pending := s.stability.Pending()
		if len(pending) == 0 {
			return b.flush()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.rules.Interval):
		}
		for _, rel := range pending {
			if _, ok := seen[rel]; !ok {
				// The path was not part of this walk (an older leftover).
				s.stability.Forget(rel)
				continue
			}
			info, err := s.entry.FS.Stat(ctx, rel)
			if err != nil {
				if errors.Is(err, source.ErrStuck) || errors.Is(err, source.ErrDegraded) {
					return err
				}
				s.stability.Forget(rel)
				continue
			}
			c := candidate{Rel: rel, Ext: extensionOf(rel), Size: info.Size(), Mtime: info.ModTime().Unix()}
			if err := b.run(func() error { return s.apply(ctx, run, c, generation, mode) }); err != nil {
				return err
			}
		}
		if err := b.flush(); err != nil {
			return err
		}
	}
	return b.flush()
}

// extensionOf returns the lower-case extension of a path without its dot.
func extensionOf(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		switch name[i] {
		case '.':
			return domain.NormalizeExt(name[i+1:])
		case '/':
			return ""
		}
	}
	return ""
}
