package media_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/media"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJanitorThumbnailEvictionDoesNotInvalidateMainPreview(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Storage.CacheBudgetBytes = 1000 })
	ctx := context.Background()
	photo := fx.seedPreview(t, "kept", 800, fx.now)
	rel, err := media.CacheRelPath(domain.VariantThumb, photo.ID)
	require.NoError(t, err)
	require.NoError(t, fx.cache.Write(rel, []byte("thumbnail")))
	require.NoError(t, fx.db.Previews().Put(ctx, &domain.PreviewFile{
		PhotoID: photo.ID, Variant: domain.VariantThumb, RelPath: rel, Bytes: 300,
		CreatedAt: fx.now.Add(-time.Hour), LastAccessAt: fx.now.Add(-time.Hour),
	}))
	fx.pipeline.RunJanitor(ctx)
	assert.Equal(t, domain.PreviewReady, fx.reload(t, photo.ID).PreviewStatus)
	_, err = fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	assert.NoError(t, err)
	_, err = fx.db.Previews().Get(ctx, photo.ID, domain.VariantThumb)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestJanitorFailedDiskDeletionKeepsAccounting(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Storage.CacheBudgetBytes = 100 })
	ctx := context.Background()
	photo := fx.seedPreview(t, "blocked", 500, fx.now)
	file, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	require.NoError(t, err)
	require.NoError(t, fx.cache.Remove(file.RelPath))
	// A nonempty directory cannot be removed as a file, even as root.
	path := fx.cache.Abs(file.RelPath)
	require.NoError(t, os.Mkdir(path, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "still-present"), []byte("data"), 0o600))
	done := make(chan struct{})
	go func() { fx.pipeline.RunJanitor(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("failed deletion must not spin forever")
	}
	_, err = fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	assert.NoError(t, err, "disk deletion failure must retain the accounting row")
	used, err := fx.db.Previews().TotalBytes(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 500, used)
	assert.Equal(t, domain.PreviewReady, fx.reload(t, photo.ID).PreviewStatus)
}

func TestJanitorQuotaShrinkTraversesMultipleBatchesAndKeepsPool(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Storage.CacheBudgetBytes = 1000 })
	ctx := context.Background()
	for i := 0; i < 230; i++ {
		fx.seedPreview(t, fmt.Sprintf("shrink-%03d", i), 100, fx.now.Add(time.Duration(i)*time.Second))
	}
	before, err := fx.db.Photos().ListEligibleIDs(ctx, 1000)
	require.NoError(t, err)
	fx.pipeline.RunJanitor(ctx)
	used, err := fx.db.Previews().TotalBytes(ctx)
	require.NoError(t, err)
	assert.LessOrEqual(t, used, int64(900))
	after, err := fx.db.Photos().ListEligibleIDs(ctx, 1000)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestJanitorLoopReclaimsReducedBudgetAtStartup(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Storage.CacheBudgetBytes = 100 })
	fx.seedPreview(t, "startup", 500, fx.now)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { fx.pipeline.RunJanitorLoop(ctx, time.Hour); close(done) }()
	require.Eventually(t, func() bool {
		used, err := fx.db.Previews().TotalBytes(ctx)
		return err == nil && used <= 90
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	<-done
}

func TestJanitorDoesNotClearTerminalPreviewFailure(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Storage.CacheBudgetBytes = 100 })
	ctx := context.Background()
	photo := fx.seedPreview(t, "failed", 500, fx.now)
	require.NoError(t, fx.db.Photos().SetPreviewFailed(ctx, photo.ID, "test", fx.now))
	fx.pipeline.RunJanitor(ctx)
	assert.Equal(t, domain.PreviewFailed, fx.reload(t, photo.ID).PreviewStatus)
	ids, err := fx.db.Photos().ListEligibleIDs(ctx, 100)
	require.NoError(t, err)
	assert.Empty(t, ids)
}
