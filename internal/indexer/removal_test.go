package indexer_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
)

func TestRemovalNeedsTwoCompletedScans(t *testing.T) {
	fx := newFixture(t, nil)
	fx.add("a.jpg", 10, mod)
	fx.add("b.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)

	// Make the photos eligible so "still ready" is a meaningful assertion.
	ctx := context.Background()
	for _, rel := range []string{"a.jpg", "b.jpg"} {
		require.NoError(t, fx.db.Photos().SetPreviewReady(ctx, fx.photo(t, rel).ID, time.Now()))
	}
	fx.fs.Remove("a.jpg")

	run := fx.scan(t, domain.ScanScheduled)
	assert.EqualValues(t, 1, run.FilesMissing)
	assert.EqualValues(t, 0, run.FilesRemoved)
	first := fx.photo(t, "a.jpg")
	assert.Equal(t, domain.PhotoReady, first.Status, "one missing scan is not enough")
	assert.Equal(t, 1, first.MissingGenerations)

	run = fx.scan(t, domain.ScanScheduled)
	assert.EqualValues(t, 1, run.FilesRemoved)
	second := fx.photo(t, "a.jpg")
	assert.Equal(t, domain.PhotoRemoved, second.Status)
	assert.NotNil(t, second.RemovedAt)
	assert.Equal(t, domain.PhotoReady, fx.photo(t, "b.jpg").Status, "the surviving file is untouched")
}

func TestFailedScanDoesNotAdvanceRemoval(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	fx.add("a.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)
	fx.fs.Remove("a.jpg")

	fx.scan(t, domain.ScanScheduled)
	require.Equal(t, 1, fx.photo(t, "a.jpg").MissingGenerations)

	// The share drops out: the scan cannot complete, so nothing may be judged.
	fx.fs.Missing = true
	fx.manager.Probe(ctx, fx.manager.Get(srcID))
	_, err := fx.sched.ScanNow(ctx, srcID, domain.ScanScheduled)
	require.Error(t, err)
	assert.Equal(t, 1, fx.photo(t, "a.jpg").MissingGenerations,
		"a scan that never completed must not count as a missing generation")
	assert.Equal(t, domain.PhotoPending, fx.photo(t, "a.jpg").Status)

	// Once the share is back, the second completed scan does the removal.
	fx.fs.Missing = false
	fx.manager.Probe(ctx, fx.manager.Get(srcID))
	fx.scan(t, domain.ScanScheduled)
	assert.Equal(t, domain.PhotoRemoved, fx.photo(t, "a.jpg").Status)
}

func TestIdentityMismatchBlocksScanAndRemoval(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	fx.add("a.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)
	fx.fs.Remove("a.jpg")

	// A different share appears under the same path; the empty listing must
	// never be read as "the whole library was deleted".
	fx.fs.SetVolume(sourceVolume("other"))
	fx.manager.Probe(ctx, fx.manager.Get(srcID))
	_, err := fx.sched.ScanNow(ctx, srcID, domain.ScanScheduled)
	require.Error(t, err)
	assert.Equal(t, 0, fx.photo(t, "a.jpg").MissingGenerations)

	_, err = fx.manager.Rebind(ctx, srcID)
	require.NoError(t, err)
	fx.scan(t, domain.ScanScheduled)
	assert.Equal(t, 1, fx.photo(t, "a.jpg").MissingGenerations)
}

func TestRevivedPathGetsANewDiscoveryDate(t *testing.T) {
	fx := newFixture(t, nil)
	fx.add("a.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)
	firstSeen := fx.photo(t, "a.jpg").FirstSeenAt
	assert.True(t, fx.photo(t, "a.jpg").IsBaseline)

	fx.fs.Remove("a.jpg")
	fx.scan(t, domain.ScanScheduled)
	fx.scan(t, domain.ScanScheduled)
	require.Equal(t, domain.PhotoRemoved, fx.photo(t, "a.jpg").Status)

	time.Sleep(2 * time.Millisecond)
	fx.add("a.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)

	revived := fx.photo(t, "a.jpg")
	assert.Equal(t, domain.PhotoPending, revived.Status)
	assert.False(t, revived.IsBaseline, "a reappearing file is a new discovery, not history")
	assert.True(t, revived.FirstSeenAt.After(firstSeen))
	assert.Equal(t, 0, revived.MissingGenerations)
	assert.Nil(t, revived.RemovedAt)
}

func TestBaselineIsOnlySetBeforeTheFirstScanCompletes(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	fx.add("history.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)
	assert.True(t, fx.photo(t, "history.jpg").IsBaseline)

	src, err := fx.db.Sources().Get(ctx, srcID)
	require.NoError(t, err)
	assert.NotNil(t, src.BaselineCompleted, "the first completed scan closes the baseline window")

	fx.add("fresh.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)
	assert.False(t, fx.photo(t, "fresh.jpg").IsBaseline, "later arrivals are genuine new photos")

	// The baseline timestamp is stable across further scans.
	fx.scan(t, domain.ScanScheduled)
	after, err := fx.db.Sources().Get(ctx, srcID)
	require.NoError(t, err)
	assert.Equal(t, src.BaselineCompleted.UTC(), after.BaselineCompleted.UTC())
}

func TestSourceRevokeHidesPhotosAndRestoreBringsThemBack(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	fx.add("a.jpg", 10, mod)
	fx.scan(t, domain.ScanScheduled)
	require.NoError(t, fx.db.Photos().SetPreviewReady(ctx, fx.photo(t, "a.jpg").ID, time.Now()))

	ready, err := fx.db.Photos().CountReady(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, ready)

	require.NoError(t, fx.db.Sources().Revoke(ctx, srcID, "operator", time.Now()))
	ready, err = fx.db.Photos().CountReady(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 0, ready, "revoking a source hides its photos without deleting the index")
	assert.Equal(t, domain.PhotoReady, fx.photo(t, "a.jpg").Status)

	// A revoked source is not scanned at all.
	_, err = fx.sched.ScanNow(ctx, srcID, domain.ScanScheduled)
	assert.Error(t, err)

	require.NoError(t, fx.db.Sources().Restore(ctx, srcID, time.Now()))
	ready, err = fx.db.Photos().CountReady(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, ready)
}
