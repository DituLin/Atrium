package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestVideoRevisionRejectsStaleMetadata(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	obs := domain.VideoObservation{SourceID: "mixed", RelPath: "旅行/a.MOV", SizeBytes: 100, MtimeUnix: 1, Generation: 1}
	v, err := db.Videos().Observe(ctx, obs, now)
	require.NoError(t, err)
	require.EqualValues(t, 1, v.Revision)
	require.Equal(t, "mov", v.Ext)
	meta := domain.VideoMetadata{Container: "mov", VideoCodec: "h264", AudioCodec: "aac", Width: 1080, Height: 1920, DurationMS: 11000, Rotation: -90}
	ok, err := db.Videos().SetMetadata(ctx, v.ID, v.Revision, meta, now)
	require.NoError(t, err)
	require.True(t, ok)
	same, err := db.Videos().Observe(ctx, obs, now.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, v.ID, same.ID)
	require.Equal(t, domain.VideoReady, same.Status)
	obs.SizeBytes = 200
	obs.Generation = 2
	changed, err := db.Videos().Observe(ctx, obs, now.Add(time.Minute))
	require.NoError(t, err)
	require.EqualValues(t, 2, changed.Revision)
	require.Equal(t, domain.VideoPending, changed.Status)
	require.Zero(t, changed.Metadata.DurationMS)
	require.Empty(t, changed.Metadata.VideoCodec)
	ok, err = db.Videos().SetMetadata(ctx, v.ID, 1, meta, now)
	require.NoError(t, err)
	require.False(t, ok)
	var count int
	require.NoError(t, db.SQL().QueryRowContext(ctx, "SELECT count(*) FROM photos").Scan(&count))
	require.Zero(t, count)
}

func TestVideoMissingGenerationsAndRevival(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	obs := domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: 100, MtimeUnix: 1, Generation: 1}
	v, err := db.Videos().Observe(ctx, obs, now)
	require.NoError(t, err)
	require.NoError(t, db.Videos().CompleteScan(ctx, "mixed", 2, now))
	require.NoError(t, db.Videos().CompleteScan(ctx, "mixed", 2, now))
	v, err = db.Videos().Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VideoPending, v.Status)
	require.EqualValues(t, 1, v.MissingGenerations)
	require.NoError(t, db.Videos().CompleteScan(ctx, "mixed", 3, now))
	v, err = db.Videos().Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VideoRemoved, v.Status)
	obs.Generation = 2
	_, err = db.Videos().Observe(ctx, obs, now)
	require.ErrorIs(t, err, domain.ErrConflict, "late observation must not revive a removed file")
	obs.Generation = 4
	revived, err := db.Videos().Observe(ctx, obs, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, v.ID, revived.ID)
	require.Equal(t, domain.VideoPending, revived.Status)
	require.Greater(t, revived.Revision, v.Revision)
	require.Zero(t, revived.MissingGenerations)
}

func TestVideoRevokedSourceCannotPublish(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	v, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: 1, Generation: 1}, now)
	require.NoError(t, err)
	require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
	ok, err := db.Videos().SetMetadata(ctx, v.ID, v.Revision, domain.VideoMetadata{Container: "mp4", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}, now)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestVideoCompleteScanRejectsLateInsertAndRevival(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	o := domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: 1, Generation: 1}
	_, err := db.Videos().Observe(ctx, o, now)
	require.NoError(t, err)
	for g := int64(2); g <= 5; g++ {
		require.NoError(t, db.Videos().CompleteScan(ctx, "mixed", g, now))
	}
	for _, rel := range []string{"a.mp4", "never-observed.mov"} {
		o.RelPath = rel
		o.Generation = 4
		_, err = db.Videos().Observe(ctx, o, now)
		require.ErrorIs(t, err, domain.ErrConflict)
	}
	o.Generation = 6
	_, err = db.Videos().Observe(ctx, o, now)
	require.NoError(t, err)
}

func TestVideoExclusionAndRollback(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	obs := domain.VideoObservation{SourceID: "mixed", RelPath: "private/a.mp4", SizeBytes: 1, MtimeUnix: 1, Generation: 1}
	v, err := db.Videos().Observe(ctx, obs, now)
	require.NoError(t, err)
	require.NotEmpty(t, v.ID)
	require.NoError(t, db.Videos().ExcludeMatching(ctx, "mixed", domain.MatchPrefix, "private", now))
	obs.SizeBytes = 20
	obs.Generation = 2
	v, err = db.Videos().Observe(ctx, obs, now)
	require.NoError(t, err)
	require.Equal(t, domain.VideoExcluded, v.Status)
	ok, err := db.Videos().SetMetadata(ctx, v.ID, v.Revision, domain.VideoMetadata{Container: "mp4", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}, now)
	require.NoError(t, err)
	require.False(t, ok)
	tx, err := db.BeginWrite(ctx)
	require.NoError(t, err)
	obs.RelPath = "rolled-back.mp4"
	_, err = db.Videos().WithTx(tx).Observe(ctx, obs, now)
	require.NoError(t, err)
	require.NoError(t, db.Videos().WithTx(tx).CompleteScan(ctx, "mixed", 4, now))
	require.NoError(t, tx.Rollback())
	_, err = db.Videos().GetByPath(ctx, "mixed", obs.RelPath)
	require.True(t, errors.Is(err, domain.ErrNotFound))
	obs.Generation = 3
	_, err = db.Videos().Observe(ctx, obs, now)
	require.NoError(t, err, "rolled-back completion must not leave a scan fence")
	for _, path := range []string{"../outside.mp4", "/absolute.mp4", "a/../b.mp4", "a.jpg", "a\\b.mp4"} {
		obs.RelPath = path
		_, err = db.Videos().Observe(ctx, obs, now)
		require.Error(t, err, path)
	}
}
