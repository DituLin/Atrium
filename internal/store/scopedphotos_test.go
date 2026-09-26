package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestScopedPhotosFilterBeforePaginationAndCounts(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "allowed")
	seedSource(t, db, "private")
	for i := 0; i < 8; i++ {
		source := "allowed"
		if i%2 == 0 {
			source = "private"
		}
		p := &domain.Photo{ID: fmt.Sprintf("p%d", i), SourceID: source, RelPath: fmt.Sprintf("%d.jpg", i), Ext: "jpg", Status: domain.PhotoReady, PreviewStatus: domain.PreviewReady, FirstSeenAt: now, LastSeenAt: now, CreatedAt: now, UpdatedAt: now}
		require.NoError(t, db.Photos().Insert(ctx, p))
	}
	require.NoError(t, db.Photos().SetStatus(ctx, "p7", domain.PhotoExcluded, now))
	require.NoError(t, db.Photos().SetPreviewStatus(ctx, "p5", domain.PreviewEvicted, "", now))
	q := store.ScopedPhotoQuery{SourceIDs: []string{"allowed"}, Collection: domain.CollectionRecent, Limit: 1}
	rows, err := db.Photos().ListScoped(ctx, q)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "p3", rows[0].ID)
	q.AfterID = rows[0].ID
	q.AfterTime = rows[0].FirstSeenAt
	rows, err = db.Photos().ListScoped(ctx, q)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "p1", rows[0].ID)
	counts, err := db.Photos().CountScoped(ctx, []string{"allowed"}, "2026-09-05", now)
	require.NoError(t, err)
	require.EqualValues(t, 2, counts.Ready)
	require.EqualValues(t, 2, counts.UnknownCaptured)
	_, err = db.Photos().GetScoped(ctx, []string{"allowed"}, "p0")
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = db.Photos().GetScoped(ctx, []string{"allowed"}, "p7")
	require.ErrorIs(t, err, domain.ErrNotFound)
	q.SourceIDs = nil
	rows, err = db.Photos().ListScoped(ctx, q)
	require.NoError(t, err)
	require.Empty(t, rows, "empty scope must never mean all sources")
	counts, err = db.Photos().CountScoped(ctx, nil, "2026-09-05", now)
	require.NoError(t, err)
	require.Zero(t, counts.Ready)
	require.NoError(t, db.Sources().Revoke(ctx, "allowed", "test", now))
	rows, err = db.Photos().ListScoped(ctx, store.ScopedPhotoQuery{SourceIDs: []string{"allowed"}, Collection: domain.CollectionAll, Limit: 20})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestScopedPhotosRejectInvalidQuery(t *testing.T) {
	db := testutil.NewDB(t)
	for _, q := range []store.ScopedPhotoQuery{
		{Collection: "invented", Limit: 20},
		{Collection: domain.CollectionAll, Limit: 0},
		{Collection: domain.CollectionAll, Limit: 51},
	} {
		_, err := db.Photos().ListScoped(context.Background(), q)
		require.Error(t, err)
	}
}
