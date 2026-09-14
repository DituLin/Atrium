package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestVideoReadScopePaginationAndImmediateExclusion(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	src, err := db.Sources().Get(ctx, "mixed")
	require.NoError(t, err)
	scope := []store.VideoScope{{SourceID: "mixed", Root: src.RootPath, MP4: true}}
	for i, name := range []string{"a.mp4", "b.mp4", "c.mov"} {
		_, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: name, SizeBytes: 1, Generation: 1}, now.Add(time.Duration(i)*time.Second))
		require.NoError(t, err)
	}
	rows, err := db.Videos().ListVisible(ctx, scope, nil, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "b.mp4", rows[0].RelPath)
	before := store.VideoPosition{Time: rows[0].FirstSeenAt, ID: rows[0].ID}
	next, err := db.Videos().ListVisible(ctx, scope, &before, 10)
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.Equal(t, "a.mp4", next[0].RelPath)
	require.NoError(t, db.Exclusions().Add(ctx, &domain.Exclusion{SourceID: "mixed", MatchKind: domain.MatchPath, Pattern: "b.mp4", CreatedAt: now}))
	_, err = db.Videos().GetVisible(ctx, scope, rows[0].ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	scope[0].Root = "/different"
	rows, err = db.Videos().ListVisible(ctx, scope, nil, 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	scope[0].Root = src.RootPath
	require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
	rows, err = db.Videos().ListVisible(ctx, scope, nil, 10)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestVideoReadHidesMetadataFromPriorAuthorization(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	v, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: 1, Generation: 1}, now)
	require.NoError(t, err)
	task, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	ok, err := db.VideoWork().Publish(ctx, *task, domain.VideoMetadata{Container: "mp4", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}, 100, 10, 10, now)
	require.NoError(t, err)
	require.True(t, ok)
	scope := []store.VideoScope{{SourceID: "mixed", Root: task.Source.Source.RootPath, MP4: true}}
	got, err := db.Videos().GetVisible(ctx, scope, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VideoReady, got.Status)
	require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
	require.NoError(t, db.Sources().Restore(ctx, "mixed", now))
	got, err = db.Videos().GetVisible(ctx, scope, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VideoPending, got.Status)
	require.Empty(t, got.Metadata.VideoCodec)
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	replacement, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, replacement)
	got, err = db.Videos().GetVisible(ctx, scope, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VideoPending, got.Status)
	require.Empty(t, got.Metadata.VideoCodec)
	ok, err = db.VideoWork().Publish(ctx, *replacement, domain.VideoMetadata{Container: "mp4", VideoCodec: "hevc", Width: 20, Height: 20, DurationMS: 2000}, 100, 10, 10, now)
	require.NoError(t, err)
	require.True(t, ok)
	got, err = db.Videos().GetVisible(ctx, scope, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VideoReady, got.Status)
	require.Equal(t, "hevc", got.Metadata.VideoCodec)

}
