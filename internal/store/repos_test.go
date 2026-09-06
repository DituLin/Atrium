package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
)

func seedSource(t *testing.T, db *store.DB, id string) time.Time {
	t.Helper()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Sources().Upsert(context.Background(), id, id, "/Volumes/photos/"+id, now))
	return now
}

func TestPhotosInsertGetAndCounts(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "family_photos")
	repo := db.Photos()

	captured := now.Add(-2 * time.Hour)
	offset := 8 * 3600
	ph := &domain.Photo{
		SourceID: "family_photos", RelPath: "2026/a.jpg", Ext: "jpg", SizeBytes: 1234, MtimeUnix: now.Unix(),
		Fingerprint: "fp1", Status: domain.PhotoReady, Width: 4032, Height: 3024, Orientation: 6,
		CapturedAt: &captured, CapturedOffsetSeconds: &offset, CapturedConfidence: domain.CapturedExact,
		CapturedDay: "2026-09-05", FirstSeenAt: now, LastSeenAt: now, LastSeenGeneration: 1,
		MetaStatus: domain.MetaReady, PreviewStatus: domain.PreviewReady, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, repo.Insert(ctx, ph))
	require.NotEmpty(t, ph.ID)

	got, err := repo.Get(ctx, ph.ID)
	require.NoError(t, err)
	require.Equal(t, "2026/a.jpg", got.RelPath)
	require.Equal(t, 6, got.Orientation)
	require.Equal(t, domain.CapturedExact, got.CapturedConfidence)
	require.NotNil(t, got.CapturedOffsetSeconds)
	require.Equal(t, 8*3600, *got.CapturedOffsetSeconds)
	require.WithinDuration(t, captured, *got.CapturedAt, time.Millisecond)

	byPath, err := repo.GetByPath(ctx, "family_photos", "2026/a.jpg")
	require.NoError(t, err)
	require.Equal(t, ph.ID, byPath.ID)

	ready, err := repo.CountReady(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, ready)

	day, err := repo.CountCapturedDay(ctx, "2026-09-05")
	require.NoError(t, err)
	require.EqualValues(t, 1, day)

	fresh, err := repo.CountFirstSeenSince(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, fresh)

	unknown, err := repo.CountUnknownCaptured(ctx)
	require.NoError(t, err)
	require.Zero(t, unknown)

	require.NoError(t, repo.SetPreviewStatus(ctx, ph.ID, domain.PreviewEvicted, "", now))
	previews, err := repo.PreviewStatusCounts(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, previews[domain.PreviewEvicted])

	require.NoError(t, repo.SetStatus(ctx, ph.ID, domain.PhotoRemoved, now))
	statuses, err := repo.StatusCounts(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, statuses[domain.PhotoRemoved])

	// A revoked source hides its photos from the eligible count.
	require.NoError(t, repo.SetStatus(ctx, ph.ID, domain.PhotoReady, now))
	require.NoError(t, db.Sources().Revoke(ctx, "family_photos", "test", now))
	ready, err = repo.CountReady(ctx)
	require.NoError(t, err)
	require.Zero(t, ready)
}

func TestExclusionsUniqueAndDelete(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	seedSource(t, db, "family_photos")
	repo := db.Exclusions()

	ex := &domain.Exclusion{SourceID: "family_photos", MatchKind: domain.MatchPrefix, Pattern: "private/", Reason: "private"}
	require.NoError(t, repo.Add(ctx, ex))
	dup := &domain.Exclusion{SourceID: "family_photos", MatchKind: domain.MatchPrefix, Pattern: "private/"}
	require.ErrorIs(t, repo.Add(ctx, dup), domain.ErrConflict)

	list, err := repo.List(ctx, "family_photos")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, domain.MatchPrefix, list[0].MatchKind)

	require.NoError(t, repo.Delete(ctx, ex.ID))
	require.ErrorIs(t, repo.Delete(ctx, ex.ID), domain.ErrNotFound)
}

func TestPreviewFilesLRUAndCascade(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "family_photos")

	ph := &domain.Photo{
		SourceID: "family_photos", RelPath: "a.jpg", Ext: "jpg", SizeBytes: 1, MtimeUnix: 1,
		Status: domain.PhotoReady, FirstSeenAt: now, LastSeenAt: now, LastSeenGeneration: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Photos().Insert(ctx, ph))

	repo := db.Previews()
	require.NoError(t, repo.Put(ctx, &domain.PreviewFile{
		PhotoID: ph.ID, Variant: domain.VariantPreview, RelPath: "preview/aa/bb/x.jpg",
		Bytes: 1000, Width: 2560, Height: 1920, Fingerprint: "fp1",
		CreatedAt: now, LastAccessAt: now.Add(-time.Hour),
	}))
	require.NoError(t, repo.Put(ctx, &domain.PreviewFile{
		PhotoID: ph.ID, Variant: domain.VariantThumb, RelPath: "thumb/aa/bb/x.jpg",
		Bytes: 100, Width: 480, Height: 360, Fingerprint: "fp1",
		CreatedAt: now, LastAccessAt: now,
	}))

	total, err := repo.TotalBytes(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1100, total)

	lru, err := repo.LRU(ctx, 10)
	require.NoError(t, err)
	require.Len(t, lru, 2)
	require.Equal(t, domain.VariantPreview, lru[0].Variant, "oldest access first")

	require.NoError(t, repo.Touch(ctx, ph.ID, domain.VariantPreview, now.Add(time.Hour)))
	lru, err = repo.LRU(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, domain.VariantThumb, lru[0].Variant)

	_, err = db.SQL().ExecContext(ctx, `DELETE FROM photos WHERE id = ?`, ph.ID)
	require.NoError(t, err)
	total, err = repo.TotalBytes(ctx)
	require.NoError(t, err)
	require.Zero(t, total, "preview rows cascade with the photo")
}
