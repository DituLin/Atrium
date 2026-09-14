package indexer_test

import (
	"context"
	"testing"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestVideoDisabledExtensionIsNotMissing(t *testing.T) {
	fx := newFixture(t, mixedSource)
	ctx := context.Background()
	fx.add("a.mp4", 10, mod)
	fx.add("b.mov", 10, mod)
	fx.scan(t, domain.ScanScheduled)
	fx.manager.Get(srcID).Config.IncludeExtensions = []string{"jpg", "mov"}
	for range 3 {
		fx.scan(t, domain.ScanScheduled)
	}
	v, err := fx.db.Videos().GetByPath(ctx, srcID, "a.mp4")
	require.NoError(t, err)
	require.Zero(t, v.MissingGenerations)
	require.Equal(t, domain.VideoPending, v.Status)
	fx.manager.Get(srcID).Config.IncludeExtensions = []string{"jpg", "mov", "mp4"}
	fx.scan(t, domain.ScanScheduled)
	v, err = fx.db.Videos().Get(ctx, v.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, v.Revision)
}

func TestVideoUnstableFileKeepsPresenceWithoutPublishingNewRevision(t *testing.T) {
	fx := newFixture(t, func(s *config.Source) { mixedSource(s); s.Scan.StabilityChecks = 100; s.Scan.StabilityMaxRound = 1 })
	ctx := context.Background()
	v, err := fx.db.Videos().Observe(ctx, domain.VideoObservation{SourceID: srcID, RelPath: "growing.mp4", SizeBytes: 10, MtimeUnix: mod.Unix(), Generation: 1}, mod)
	require.NoError(t, err)
	fx.add("growing.mp4", 20, mod)
	fx.add("new.mov", 30, mod)
	for range 3 {
		fx.scan(t, domain.ScanScheduled)
	}
	v, err = fx.db.Videos().Get(ctx, v.ID)
	require.NoError(t, err)
	require.EqualValues(t, 10, v.SizeBytes)
	require.EqualValues(t, 1, v.Revision)
	require.Zero(t, v.MissingGenerations)
	_, err = fx.db.Videos().GetByPath(ctx, srcID, "new.mov")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func mixedSource(s *config.Source) { s.IncludeExtensions = append(s.IncludeExtensions, "mp4", "mov") }

func TestVideoMixedDirectorySeparatesPhotoJobs(t *testing.T) {
	fx := newFixture(t, mixedSource)
	ctx := context.Background()
	fx.add("a.jpg", 10, mod)
	fx.add("a.mp4", 20, mod)
	fx.add("nested/b.MOV", 30, mod)
	run := fx.scan(t, domain.ScanScheduled)
	require.EqualValues(t, 3, run.FilesSeen)
	for _, rel := range []string{"a.mp4", "nested/b.MOV"} {
		v, err := fx.db.Videos().GetByPath(ctx, srcID, rel)
		require.NoError(t, err)
		require.Equal(t, domain.VideoPending, v.Status)
		_, err = fx.db.Photos().GetByPath(ctx, srcID, rel)
		require.ErrorIs(t, err, domain.ErrNotFound)
	}
	var n int
	require.NoError(t, fx.db.SQL().QueryRowContext(ctx, "SELECT count(*) FROM jobs").Scan(&n))
	require.Equal(t, 1, n)
	fx.add("a.mp4", 40, mod.Add(1))
	fx.scan(t, domain.ScanScheduled)
	v, err := fx.db.Videos().GetByPath(ctx, srcID, "a.mp4")
	require.NoError(t, err)
	require.EqualValues(t, 2, v.Revision)
}

func TestVideoIncompleteListingNeverRemoves(t *testing.T) {
	fx := newFixture(t, mixedSource)
	ctx := context.Background()
	fx.add("private/a.mp4", 10, mod)
	fx.scan(t, domain.ScanScheduled)
	fx.fs.Denied = map[string]bool{"private": true}
	for range 3 {
		run := fx.scan(t, domain.ScanScheduled)
		require.Positive(t, run.Errors)
	}
	v, err := fx.db.Videos().GetByPath(ctx, srcID, "private/a.mp4")
	require.NoError(t, err)
	require.Zero(t, v.MissingGenerations)
	fx.fs.Denied = nil
	fx.fs.Remove("private/a.mp4")
	run := fx.scan(t, domain.ScanScheduled)
	require.EqualValues(t, 1, run.FilesMissing)
	require.Zero(t, run.FilesRemoved)
	v, err = fx.db.Videos().Get(ctx, v.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, v.MissingGenerations)
	run = fx.scan(t, domain.ScanScheduled)
	require.EqualValues(t, 1, run.FilesRemoved)
	v, err = fx.db.Videos().Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VideoRemoved, v.Status)
}

func TestVideoRespectsAllowlistAndExistingExclusions(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	fx.add("a.mp4", 10, mod)
	fx.scan(t, domain.ScanScheduled)
	_, err := fx.db.Videos().GetByPath(ctx, srcID, "a.mp4")
	require.ErrorIs(t, err, domain.ErrNotFound)
	fx.manager.Get(srcID).Config.IncludeExtensions = append(fx.manager.Get(srcID).Config.IncludeExtensions, "mp4")
	fx.scan(t, domain.ScanScheduled)
	require.NoError(t, fx.db.Exclusions().Add(ctx, &domain.Exclusion{SourceID: srcID, MatchKind: domain.MatchPath, Pattern: "a.mp4", CreatedAt: mod}))
	fx.scan(t, domain.ScanScheduled)
	v, err := fx.db.Videos().GetByPath(ctx, srcID, "a.mp4")
	require.NoError(t, err)
	require.Equal(t, domain.VideoExcluded, v.Status)
}
