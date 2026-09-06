package media_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/media"
	"github.com/DituLin/Atritum/internal/media/testutil"
)

// fakeConverter stands in for sips so the HEIC path is testable off macOS.
type fakeConverter struct {
	jpeg     []byte
	created  time.Time
	convErr  error
	disabled bool
	lastEdge int
}

func (f *fakeConverter) Enabled() bool { return !f.disabled }

func (f *fakeConverter) Convert(_ context.Context, _, dstDir string, maxEdge int) (string, error) {
	f.lastEdge = maxEdge
	if f.convErr != nil {
		return "", f.convErr
	}
	out := filepath.Join(dstDir, "converted.jpg")
	if err := os.WriteFile(out, f.jpeg, 0o600); err != nil {
		return "", err
	}
	return out, nil
}

func (f *fakeConverter) CreationTime(context.Context, string) (time.Time, error) {
	if f.created.IsZero() {
		return time.Time{}, errors.New("no creation date")
	}
	return f.created, nil
}

func TestHEICConversionProducesPreview(t *testing.T) {
	conv := &fakeConverter{jpeg: jpegBytes(t, 800, 600, testutil.ExifOptions{})}
	fx := newFixture(t, func(o *media.Options) { o.Converter = conv })
	ctx := context.Background()
	photo := fx.seed(t, "album/a.heic", "heic", []byte("not really heic bytes"))

	require.NoError(t, fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID)))
	assert.Equal(t, domain.PreviewReady, fx.reload(t, photo.ID).PreviewStatus)
	assert.Equal(t, 512, conv.lastEdge, "the converter is asked to downscale during conversion")
}

func TestHEICOffMarksUnsupported(t *testing.T) {
	fx := newFixture(t, nil) // the fixture defaults to converter "off"
	ctx := context.Background()
	photo := fx.seed(t, "album/a.heic", "heic", []byte("heic"))

	err := fx.pipeline.BuildPreview(ctx, fx.job(domain.JobBuildPreview, photo.ID))
	require.Error(t, err)
	assert.True(t, jobs.IsPermanent(err))
	got := fx.reload(t, photo.ID)
	assert.Equal(t, domain.PhotoUnsupported, got.Status)
	assert.Equal(t, media.ErrCodeUnsupported, got.PreviewError)
}

func TestHEICCreationTimeFallback(t *testing.T) {
	conv := &fakeConverter{
		jpeg:    jpegBytes(t, 64, 64, testutil.ExifOptions{}),
		created: time.Date(2026, 7, 4, 9, 30, 0, 0, time.UTC),
	}
	fx := newFixture(t, func(o *media.Options) { o.Converter = conv })
	ctx := context.Background()
	photo := fx.seed(t, "album/a.heic", "heic", []byte("heic"))

	require.NoError(t, fx.pipeline.ExtractMeta(ctx, fx.job(domain.JobExtractMeta, photo.ID)))
	got := fx.reload(t, photo.ID)
	// The converter reports a wall clock with no zone, so it is an inference.
	assert.Equal(t, domain.CapturedInferred, got.CapturedConfidence)
	assert.Equal(t, "2026-07-04", got.CapturedDay)
}

func TestParseSipsCreation(t *testing.T) {
	at, err := media.ParseSipsCreation("/Volumes/x/a.heic\n  creation: 2026:09:05 10:12:00\n")
	require.NoError(t, err)
	assert.Equal(t, "2026-09-05 10:12:00", at.Format("2006-01-02 15:04:05"))

	_, err = media.ParseSipsCreation("nothing here")
	assert.Error(t, err)
}

func TestIsHEIC(t *testing.T) {
	assert.True(t, media.IsHEIC("heic"))
	assert.True(t, media.IsHEIC("heif"))
	assert.False(t, media.IsHEIC("jpg"))
}

// TestSipsConverterAgainstRealBinary only runs when the maintainer opts in and
// provides a sample; no HEIC file is committed to the repository.
func TestSipsConverterAgainstRealBinary(t *testing.T) {
	if os.Getenv("ATRIUM_TEST_SIPS") != "1" {
		t.Skip("set ATRIUM_TEST_SIPS=1 and ATRIUM_TEST_HEIC=<path> to exercise sips")
	}
	sample := os.Getenv("ATRIUM_TEST_HEIC")
	if sample == "" {
		t.Skip("ATRIUM_TEST_HEIC is not set")
	}
	conv := media.NewConverter(media.ConverterSips)
	require.True(t, conv.Enabled(), "/usr/bin/sips must exist")

	dir := t.TempDir()
	out, err := conv.Convert(context.Background(), sample, dir, 1024)
	require.NoError(t, err)
	info, err := os.Stat(out)
	require.NoError(t, err)
	assert.Positive(t, info.Size())
}
