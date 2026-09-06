package media_test

import (
	"bytes"
	"context"
	"image"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/media"
	"github.com/DituLin/Atritum/internal/media/testutil"
)

func jpegBytes(t *testing.T, w, h int, opts testutil.ExifOptions) []byte {
	t.Helper()
	raw, err := testutil.JPEG(testutil.Marker(testutil.Gradient(w, h)), opts, 92)
	require.NoError(t, err)
	return raw
}

func TestExtractMetaStoresCaptureTimeAndChainsPreview(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	data := jpegBytes(t, 64, 48, testutil.ExifOptions{
		DateTimeOriginal:   time.Date(2026, 9, 5, 10, 12, 0, 0, time.UTC),
		OffsetTimeOriginal: "+08:00",
		Orientation:        1,
	})
	photo := fx.seed(t, "album/a.jpg", "jpg", data)

	require.NoError(t, fx.pipeline.ExtractMeta(ctx, fx.job(domain.JobExtractMeta, photo.ID)))

	got := fx.reload(t, photo.ID)
	assert.Equal(t, domain.MetaReady, got.MetaStatus)
	assert.Equal(t, domain.CapturedExact, got.CapturedConfidence)
	assert.Equal(t, "2026-09-05", got.CapturedDay)
	assert.NotEmpty(t, got.Fingerprint)

	open, err := fx.db.Jobs().HasOpen(ctx, domain.JobBuildPreview, photo.ID)
	require.NoError(t, err)
	assert.True(t, open, "extract_meta must chain build_preview")
}

func TestExtractMetaWithoutExifIsUnknownNotMtime(t *testing.T) {
	fx := newFixture(t, nil)
	png, err := testutil.PNG(testutil.Gradient(32, 32))
	require.NoError(t, err)
	photo := fx.seed(t, "album/b.png", "png", png)

	require.NoError(t, fx.pipeline.ExtractMeta(context.Background(), fx.job(domain.JobExtractMeta, photo.ID)))
	got := fx.reload(t, photo.ID)
	assert.Equal(t, domain.CapturedUnknown, got.CapturedConfidence)
	assert.Nil(t, got.CapturedAt)
	assert.Empty(t, got.CapturedDay)
	assert.Equal(t, media.MetaErrorNoMetadata, got.MetaError)
}

func TestExtractMetaDefersWhenSourceOffline(t *testing.T) {
	fx := newFixture(t, nil)
	photo := fx.seed(t, "album/a.jpg", "jpg", jpegBytes(t, 16, 16, testutil.ExifOptions{}))
	fx.sources.online = false

	err := fx.pipeline.ExtractMeta(context.Background(), fx.job(domain.JobExtractMeta, photo.ID))
	assert.True(t, jobs.IsDeferred(err), "an offline share postpones work, it does not fail it")
}

func TestBuildPreviewProducesBoundedVariants(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	photo := fx.seed(t, "album/big.jpg", "jpg", jpegBytes(t, 1600, 900, testutil.ExifOptions{}))
	require.NoError(t, fx.pipeline.ExtractMeta(ctx, fx.job(domain.JobExtractMeta, photo.ID)))
	require.NoError(t, fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID)))

	got := fx.reload(t, photo.ID)
	assert.Equal(t, domain.PreviewReady, got.PreviewStatus)
	assert.Equal(t, domain.PhotoReady, got.Status, "a ready preview publishes the photo")

	preview, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	require.NoError(t, err)
	assert.Equal(t, 512, preview.Width)
	assert.LessOrEqual(t, preview.Bytes, int64(1<<20))
	assert.True(t, fx.cache.Exists(preview.RelPath))

	thumb, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantThumb)
	require.NoError(t, err)
	assert.Equal(t, 128, thumb.Width)

	// A JPEG whose EXIF carries no pixel dimensions still gets true sizes,
	// taken from the decoded image rather than left at zero.
	assert.Equal(t, 1600, got.Width)
	assert.Equal(t, 900, got.Height)

	// Re-encoding strips EXIF: the cached JPEG must carry no APP1 segment.
	raw, err := fx.cache.Read(preview.RelPath)
	require.NoError(t, err)
	assert.False(t, bytes.Contains(raw, []byte("Exif\x00\x00")), "previews must not carry EXIF or GPS")
}

func TestBuildPreviewAppliesExifOrientation(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	// Orientation 6 means "rotate 90° clockwise", so a 400x200 source becomes
	// a 200x400 preview.
	photo := fx.seed(t, "album/rot.jpg", "jpg", jpegBytes(t, 400, 200, testutil.ExifOptions{Orientation: 6}))
	require.NoError(t, fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID)))

	preview, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	require.NoError(t, err)
	assert.Equal(t, 200, preview.Width)
	assert.Equal(t, 400, preview.Height)

	// The stored dimensions describe the upright image, not the stored one.
	got := fx.reload(t, photo.ID)
	assert.Equal(t, 200, got.Width)
	assert.Equal(t, 400, got.Height)
}

func TestBuildPreviewCorruptFileRetriesThenFails(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	photo := fx.seed(t, "album/bad.jpg", "jpg", testutil.Corrupt())

	err := fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID))
	require.Error(t, err)
	got := fx.reload(t, photo.ID)
	// A truncated JPEG cannot even be inspected, so it is a format verdict.
	assert.Equal(t, domain.PhotoUnsupported, got.Status)
	assert.Equal(t, media.ErrCodeUnsupported, got.PreviewError)
	assert.True(t, jobs.IsPermanent(err))
}

func TestBuildPreviewRejectsOversizedDimensions(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	photo := fx.seed(t, "album/huge.png", "png", testutil.HugePNG(60000, 60000))

	err := fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID))
	require.Error(t, err)
	got := fx.reload(t, photo.ID)
	assert.Equal(t, media.ErrCodeTooLarge, got.PreviewError)
	assert.Equal(t, domain.PreviewFailed, got.PreviewStatus)
}

func TestBuildPreviewRejectsOversizedSourceBytes(t *testing.T) {
	fx := newFixture(t, func(o *media.Options) { o.Media.MaxSourceBytes = 100 })
	ctx := context.Background()
	photo := fx.seed(t, "album/big.jpg", "jpg", jpegBytes(t, 200, 200, testutil.ExifOptions{}))

	err := fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID))
	require.Error(t, err)
	assert.Equal(t, media.ErrCodeTooLarge, fx.reload(t, photo.ID).PreviewError)
}

func TestBuildPreviewRetriesRecoverableIOErrors(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	photo := fx.seed(t, "album/a.jpg", "jpg", jpegBytes(t, 32, 32, testutil.ExifOptions{}))
	fx.fs.Denied["album"] = true

	err := fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID))
	require.Error(t, err)
	got := fx.reload(t, photo.ID)
	assert.Equal(t, media.ErrCodeIO, got.PreviewError)
	assert.Equal(t, 1, got.PreviewAttempts)
	require.NotNil(t, got.PreviewNextRetryAt)
	assert.Equal(t, fx.now.Add(time.Minute).UTC(), got.PreviewNextRetryAt.UTC())
}

func TestBuildPreviewGivesUpAfterMaxAttempts(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	photo := fx.seed(t, "album/a.jpg", "jpg", jpegBytes(t, 32, 32, testutil.ExifOptions{}))
	fx.fs.Denied["album"] = true

	for i := 0; i < media.MaxPreviewAttempts; i++ {
		_ = fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID))
	}
	got := fx.reload(t, photo.ID)
	assert.Equal(t, domain.PreviewFailed, got.PreviewStatus)
	assert.Equal(t, media.MaxPreviewAttempts, got.PreviewAttempts)
}

func TestBuildPreviewPausesOnLowDisk(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	photo := fx.seed(t, "album/a.jpg", "jpg", jpegBytes(t, 32, 32, testutil.ExifOptions{}))
	fx.disk.FreeBytes = 10

	err := fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID))
	assert.True(t, jobs.IsDeferred(err))
	assert.Equal(t, media.PausedLowDisk, fx.pipeline.CacheState(ctx).PausedReason)
	assert.Zero(t, fx.reload(t, photo.ID).PreviewAttempts, "a paused pipeline must not burn the retry budget")
}

func TestBuildPreviewSkipsExcludedAndRemoved(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	photo := fx.seed(t, "album/a.jpg", "jpg", jpegBytes(t, 32, 32, testutil.ExifOptions{}))
	require.NoError(t, fx.db.Photos().SetStatus(ctx, photo.ID, domain.PhotoExcluded, fx.now))

	require.NoError(t, fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID)))
	_, err := fx.db.Previews().Get(ctx, photo.ID, domain.VariantPreview)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestRenderNeverUpscales(t *testing.T) {
	small := image.NewRGBA(image.Rect(0, 0, 40, 30))
	out := media.Fit(small, 1000)
	assert.Equal(t, 40, out.Bounds().Dx())
}

func TestRecomputeDayRewritesHomeDays(t *testing.T) {
	fx := newFixture(t, nil)
	ctx := context.Background()
	photo := fx.seed(t, "album/a.jpg", "jpg", jpegBytes(t, 16, 16, testutil.ExifOptions{}))
	at := time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC) // 2026-09-06 in Singapore
	require.NoError(t, fx.db.Photos().SetMeta(ctx, photo.ID, storeMeta(at), fx.now))

	require.NoError(t, fx.pipeline.RecomputeDay(ctx, fx.job(domain.JobRecomputeDay, "")))
	assert.Equal(t, "2026-09-06", fx.reload(t, photo.ID).CapturedDay)
}

func TestCacheRelPathLayout(t *testing.T) {
	rel, err := media.CacheRelPath(domain.VariantPreview, "01JABCDEF")
	require.NoError(t, err)
	assert.Equal(t, "preview/01/JA/01JABCDEF.jpg", rel)

	_, err = media.CacheRelPath(domain.VariantThumb, "../../etc")
	assert.Error(t, err)
	_, err = media.CacheRelPath("bogus", "01JABCDEF")
	assert.Error(t, err)
}
