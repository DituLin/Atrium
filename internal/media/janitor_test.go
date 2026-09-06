package media_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/media"
	"github.com/DituLin/Atritum/internal/media/testutil"
)

// seedPreview inserts a photo with a cached preview of the given size.
func (f *fixture) seedPreview(t *testing.T, name string, bytes int64, accessed time.Time) *domain.Photo {
	t.Helper()
	ctx := context.Background()
	photo := f.seed(t, "album/"+name+".jpg", "jpg", jpegBytes(t, 16, 16, testutil.ExifOptions{}))
	require.NoError(t, f.db.Photos().SetPreviewReady(ctx, photo.ID, f.now))

	rel, err := media.CacheRelPath(domain.VariantPreview, photo.ID)
	require.NoError(t, err)
	require.NoError(t, f.cache.Write(rel, make([]byte, 16)))
	require.NoError(t, f.db.Previews().Put(ctx, &domain.PreviewFile{
		PhotoID: photo.ID, Variant: domain.VariantPreview, RelPath: rel,
		Bytes: bytes, Width: 16, Height: 16, CreatedAt: accessed, LastAccessAt: accessed,
	}))
	return photo
}

func TestJanitorEvictsLeastRecentlyUsedFirst(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Storage.CacheBudgetBytes = 1000 })
	ctx := context.Background()
	base := fx.now

	oldest := fx.seedPreview(t, "oldest", 500, base.Add(-3*time.Hour))
	middle := fx.seedPreview(t, "middle", 500, base.Add(-2*time.Hour))
	newest := fx.seedPreview(t, "newest", 500, base.Add(-time.Hour))

	fx.pipeline.RunJanitor(ctx)

	// The budget is 1000 and the target 900, so 1500 bytes must fall to <= 900:
	// the two oldest entries go, the newest stays.
	_, err := fx.db.Previews().Get(ctx, oldest.ID, domain.VariantPreview)
	assert.ErrorIs(t, err, domain.ErrNotFound)
	_, err = fx.db.Previews().Get(ctx, middle.ID, domain.VariantPreview)
	assert.ErrorIs(t, err, domain.ErrNotFound)
	_, err = fx.db.Previews().Get(ctx, newest.ID, domain.VariantPreview)
	assert.NoError(t, err)
}

func TestEvictionKeepsTheIndexAndMarksEvicted(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Storage.CacheBudgetBytes = 100 })
	ctx := context.Background()
	photo := fx.seedPreview(t, "one", 500, fx.now.Add(-time.Hour))

	rel, err := media.CacheRelPath(domain.VariantPreview, photo.ID)
	require.NoError(t, err)
	require.True(t, fx.cache.Exists(rel))

	fx.pipeline.RunJanitor(ctx)

	got := fx.reload(t, photo.ID)
	assert.Equal(t, domain.PreviewEvicted, got.PreviewStatus,
		"eviction and removal are different states (PRD 5.2)")
	assert.NotEqual(t, domain.PhotoRemoved, got.Status)
	assert.False(t, fx.cache.Exists(rel), "the cached bytes are gone")
}

func TestJanitorDoesNothingUnderBudget(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Storage.CacheBudgetBytes = 1 << 20 })
	ctx := context.Background()
	photo := fx.seedPreview(t, "one", 500, fx.now)

	fx.pipeline.RunJanitor(ctx)
	_, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	assert.NoError(t, err)
	assert.Equal(t, domain.PreviewReady, fx.reload(t, photo.ID).PreviewStatus)
}

func TestCacheStateReportsLowDiskPause(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	fx.disk.FreeBytes = 1

	state := fx.pipeline.CacheState(ctx)
	assert.Equal(t, media.PausedLowDisk, state.PausedReason)
	assert.EqualValues(t, 1, state.FreeDiskBytes)

	fx.disk.FreeBytes = 1 << 40
	assert.Empty(t, fx.pipeline.CacheState(ctx).PausedReason)
}

func TestTouchIsThrottledToOncePerHour(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	photo := fx.seedPreview(t, "one", 100, fx.now.Add(-24*time.Hour))

	fx.pipeline.Touch(ctx, photo.ID, domain.VariantPreview)
	first, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	require.NoError(t, err)
	assert.Equal(t, fx.now.UTC(), first.LastAccessAt.UTC())

	// A second touch within the hour must not write again.
	fx.now = fx.now.Add(30 * time.Minute)
	fx.pipeline.Touch(ctx, photo.ID, domain.VariantPreview)
	second, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	require.NoError(t, err)
	assert.Equal(t, first.LastAccessAt.UTC(), second.LastAccessAt.UTC())

	fx.now = fx.now.Add(2 * time.Hour)
	fx.pipeline.Touch(ctx, photo.ID, domain.VariantPreview)
	third, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	require.NoError(t, err)
	assert.Equal(t, fx.now.UTC(), third.LastAccessAt.UTC())
}

func TestJanitorLoopStopsWithContext(t *testing.T) {
	fx := newFixture(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { fx.pipeline.RunJanitorLoop(ctx, 5*time.Millisecond); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("janitor loop did not stop")
	}
}
