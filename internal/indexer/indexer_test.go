package indexer_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/indexer"
)

var mod = time.Unix(1_760_000_000, 0)

func TestScanIndexesSupportedFilesOnly(t *testing.T) {
	fx := newFixture(t, nil)
	fx.add("album/a.jpg", 100, mod)
	fx.add("album/sub/b.PNG", 200, mod)
	fx.add("album/notes.txt", 10, mod)
	fx.add("album/movie.mov", 10, mod)
	fx.fs.AddSymlink("album/link.jpg")

	run := fx.scan(t, domain.ScanScheduled)
	assert.EqualValues(t, 2, run.FilesSeen)
	assert.EqualValues(t, 2, run.FilesNew)
	assert.Equal(t, domain.ScanCompleted, run.Status)

	a := fx.photo(t, "album/a.jpg")
	assert.Equal(t, domain.PhotoPending, a.Status)
	assert.Equal(t, "jpg", a.Ext)
	assert.EqualValues(t, 100, a.SizeBytes)
	assert.True(t, a.IsBaseline, "the first complete import is the baseline")

	b := fx.photo(t, "album/sub/b.PNG")
	assert.Equal(t, "png", b.Ext, "extensions are normalised")

	_, err := fx.db.Photos().GetByPath(context.Background(), srcID, "album/link.jpg")
	assert.ErrorIs(t, err, domain.ErrNotFound, "symlinks are never followed or indexed")
}

func TestScanEnqueuesMetadataJobs(t *testing.T) {
	fx := newFixture(t, nil)
	fx.add("a.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)

	photo := fx.photo(t, "a.jpg")
	open, err := fx.db.Jobs().HasOpen(context.Background(), domain.JobExtractMeta, photo.ID)
	require.NoError(t, err)
	assert.True(t, open)
}

func TestStabilityGateCarriesUnresolvedCandidatesAcrossScans(t *testing.T) {
	fx := newFixture(t, func(s *config.Source) {
		s.Scan.StabilityChecks = 3
		s.Scan.StabilityInterval = config.Duration(10 * time.Millisecond)
		s.Scan.StabilityMaxRound = 1
	})
	fx.add("growing.jpg", 100, mod)

	// One walk plus one in-scan round yields two spaced sightings, one short
	// of the three this source demands.
	run := fx.scan(t, domain.ScanScheduled)
	assert.EqualValues(t, 1, run.FilesSeen)
	assert.EqualValues(t, 0, run.FilesNew)
	_, err := fx.db.Photos().GetByPath(context.Background(), srcID, "growing.jpg")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	// The next scan finishes the count, because the candidate carried over.
	run = fx.scan(t, domain.ScanScheduled)
	assert.EqualValues(t, 1, run.FilesNew)
	assert.Equal(t, domain.PhotoPending, fx.photo(t, "growing.jpg").Status)
}

func TestStabilitySetRestartsWhenTheFileKeepsChanging(t *testing.T) {
	set := indexer.NewStabilitySet()
	rules := indexer.Rules{Checks: 2, Interval: 10 * time.Millisecond}
	base := time.Unix(1000, 0)

	assert.False(t, set.Observe("a.jpg", 100, 1, base, rules))
	// A sighting inside the interval does not count as an independent check.
	assert.False(t, set.Observe("a.jpg", 100, 1, base.Add(time.Millisecond), rules))
	// The file grew: the countdown restarts rather than completing.
	assert.False(t, set.Observe("a.jpg", 300, 1, base.Add(20*time.Millisecond), rules))
	assert.Equal(t, 1, set.Len())
	// Two spaced, identical sightings finally clear the gate.
	assert.True(t, set.Observe("a.jpg", 300, 1, base.Add(40*time.Millisecond), rules))
	assert.Equal(t, 0, set.Len())

	// A single required check means no gate at all.
	assert.True(t, set.Observe("b.jpg", 1, 1, base, indexer.Rules{Checks: 1}))
}

func TestStabilityRoundsResolveWithinOneScan(t *testing.T) {
	fx := newFixture(t, func(s *config.Source) {
		s.Scan.StabilityChecks = 2
		s.Scan.StabilityInterval = config.Duration(5 * time.Millisecond)
		s.Scan.StabilityMaxRound = 3
	})
	fx.add("settled.jpg", 100, mod)

	run := fx.scan(t, domain.ScanScheduled)
	assert.EqualValues(t, 1, run.FilesNew, "the in-scan rounds re-stat and index a settled file")
}

func TestChangedFileIsReindexedAndPreviewInvalidated(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	fx.add("a.jpg", 100, mod)
	fx.scan(t, domain.ScanScheduled)

	photo := fx.photo(t, "a.jpg")
	require.NoError(t, fx.db.Photos().SetPreviewReady(ctx, photo.ID, time.Now()))
	require.NoError(t, fx.db.Previews().Put(ctx, &domain.PreviewFile{
		PhotoID: photo.ID, Variant: domain.VariantPreview, RelPath: "preview/x.jpg", Bytes: 10,
	}))

	fx.add("a.jpg", 400, mod.Add(time.Hour))
	run := fx.scan(t, domain.ScanScheduled)
	assert.EqualValues(t, 1, run.FilesChanged)

	got := fx.photo(t, "a.jpg")
	assert.Equal(t, domain.PhotoPending, got.Status)
	assert.Equal(t, domain.MetaPending, got.MetaStatus)
	assert.Equal(t, domain.PreviewPending, got.PreviewStatus)
	assert.EqualValues(t, 400, got.SizeBytes)
	assert.Empty(t, got.Fingerprint, "the fingerprint belongs to the previous version")

	_, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestExcludedPrefixIsSkippedAndSurvivesFullRescan(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	fx.add("public/a.jpg", 10, mod)
	fx.add("private/secret.jpg", 10, mod)

	require.NoError(t, fx.db.Exclusions().Add(ctx, &domain.Exclusion{
		SourceID: srcID, MatchKind: domain.MatchPrefix, Pattern: "private", Reason: "operator",
	}))

	run := fx.scan(t, domain.ScanScheduled)
	assert.EqualValues(t, 1, run.FilesSeen)
	_, err := fx.db.Photos().GetByPath(ctx, srcID, "private/secret.jpg")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	full := fx.scan(t, domain.ScanFull)
	assert.EqualValues(t, 1, full.FilesSeen, "exclusions survive a full rescan")
}

func TestProgressCountersAreExposedDuringAndAfterAScan(t *testing.T) {
	fx := newFixture(t, nil)
	for i := 0; i < 5; i++ {
		fx.add("album/"+string(rune('a'+i))+".jpg", 10, mod)
	}
	assert.Equal(t, domain.IndexIdle, fx.sched.Aggregate().State)

	fx.scan(t, domain.ScanScheduled)
	agg := fx.sched.Aggregate()
	assert.Equal(t, domain.IndexIdle, agg.State)
	assert.EqualValues(t, 5, agg.Seen)
	assert.EqualValues(t, 5, agg.Indexed)
	assert.NotNil(t, agg.LastScanAt)
}

func TestScanRefusesOfflineSourceWithoutTouchingTheIndex(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	fx.add("a.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)

	fx.fs.Missing = true
	fx.manager.Probe(ctx, fx.manager.Get(srcID))
	_, err := fx.sched.ScanNow(ctx, srcID, domain.ScanScheduled)
	require.Error(t, err)

	assert.Equal(t, domain.PhotoPending, fx.photo(t, "a.jpg").Status)
	assert.EqualValues(t, 0, fx.photo(t, "a.jpg").MissingGenerations)
}

func TestExclusionSetMatching(t *testing.T) {
	set := indexer.NewExclusionSet([]domain.Exclusion{
		{MatchKind: domain.MatchPrefix, Pattern: "private/"},
		{MatchKind: domain.MatchPath, Pattern: "album/one.jpg"},
	})
	assert.True(t, set.Excluded("private/a/b.jpg"))
	assert.True(t, set.Excluded("private"))
	assert.True(t, set.Excluded("album/one.jpg"))
	assert.False(t, set.Excluded("privateer/a.jpg"))
	assert.False(t, set.Excluded("album/two.jpg"))
	assert.False(t, indexer.NewExclusionSet(nil).Excluded("anything"))
}
