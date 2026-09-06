package media_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/media"
	"github.com/DituLin/Atrium/internal/media/testutil"
)

func TestExifReaderReadsDateOffsetAndOrientation(t *testing.T) {
	when := time.Date(2026, 9, 5, 10, 12, 0, 0, time.UTC)
	raw, err := testutil.JPEG(testutil.Gradient(64, 48), testutil.ExifOptions{
		DateTimeOriginal:   when,
		OffsetTimeOriginal: "+08:00",
		Orientation:        6,
	}, 85)
	require.NoError(t, err)

	md, err := media.NewExifReader().Read(context.Background(), bytes.NewReader(raw))
	require.NoError(t, err)
	assert.Equal(t, 6, md.Orientation)
	assert.True(t, md.HasOffset)
	assert.Equal(t, 8*3600, md.OffsetSeconds)
	assert.Equal(t, "2026-09-05 10:12:00", md.DateTimeOriginal.Format("2006-01-02 15:04:05"))
	assert.Equal(t, when.Add(-8*time.Hour), md.DateTimeOriginal.UTC())
}

func TestExifReaderWithoutOffsetKeepsWallClock(t *testing.T) {
	when := time.Date(2026, 3, 1, 23, 30, 0, 0, time.UTC)
	raw, err := testutil.JPEG(testutil.Gradient(32, 32), testutil.ExifOptions{DateTimeOriginal: when}, 85)
	require.NoError(t, err)

	md, err := media.NewExifReader().Read(context.Background(), bytes.NewReader(raw))
	require.NoError(t, err)
	assert.False(t, md.HasOffset)
	assert.Equal(t, "2026-03-01 23:30:00", md.DateTimeOriginal.Format("2006-01-02 15:04:05"))
}

func TestExifReaderPNGHasNoMetadata(t *testing.T) {
	raw, err := testutil.PNG(testutil.Gradient(20, 20))
	require.NoError(t, err)
	_, err = media.NewExifReader().Read(context.Background(), bytes.NewReader(raw))
	assert.ErrorIs(t, err, media.ErrNoMetadata)
}

func TestExifReaderPlainJPEGHasNoMetadata(t *testing.T) {
	raw, err := testutil.JPEG(testutil.Gradient(20, 20), testutil.ExifOptions{}, 85)
	require.NoError(t, err)
	_, err = media.NewExifReader().Read(context.Background(), bytes.NewReader(raw))
	assert.ErrorIs(t, err, media.ErrNoMetadata)
}

func TestExifReaderCorruptFile(t *testing.T) {
	_, err := media.NewExifReader().Read(context.Background(), bytes.NewReader(testutil.Corrupt()))
	assert.Error(t, err)
}

func TestExifReaderOrientationOnlyFile(t *testing.T) {
	raw, err := testutil.JPEG(testutil.Gradient(16, 16), testutil.ExifOptions{Orientation: 3}, 85)
	require.NoError(t, err)
	md, err := media.NewExifReader().Read(context.Background(), bytes.NewReader(raw))
	require.NoError(t, err)
	assert.Equal(t, 3, md.Orientation)
	assert.True(t, md.DateTimeOriginal.IsZero())
}
