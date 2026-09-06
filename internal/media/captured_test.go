package media_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/clock"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/media"
)

func homeSG(t *testing.T) *clock.Home {
	t.Helper()
	h, err := clock.NewHome("Asia/Singapore", clock.SystemClock{})
	require.NoError(t, err)
	return h
}

func TestResolveCapturedExact(t *testing.T) {
	home := homeSG(t)
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 9, 5, 10, 12, 0, 0, time.FixedZone("", 8*3600))

	got := media.ResolveCaptured(media.Metadata{
		DateTimeOriginal: at, HasOffset: true, OffsetSeconds: 8 * 3600,
	}, home, now)

	assert.Equal(t, domain.CapturedExact, got.Confidence)
	require.NotNil(t, got.At)
	assert.Equal(t, "2026-09-05T02:12:00Z", got.At.Format(time.RFC3339))
	require.NotNil(t, got.OffsetSeconds)
	assert.Equal(t, 8*3600, *got.OffsetSeconds)
	assert.Equal(t, "2026-09-05", got.Day)
}

func TestResolveCapturedInferredUsesHomeTimezone(t *testing.T) {
	home := homeSG(t)
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	// A wall clock the library hands back as UTC because no offset tag exists.
	wall := time.Date(2026, 9, 5, 23, 30, 0, 0, time.UTC)

	got := media.ResolveCaptured(media.Metadata{DateTimeOriginal: wall}, home, now)
	assert.Equal(t, domain.CapturedInferred, got.Confidence)
	assert.Nil(t, got.OffsetSeconds)
	require.NotNil(t, got.At)
	// 23:30 in Singapore is 15:30 UTC on the same calendar day.
	assert.Equal(t, "2026-09-05T15:30:00Z", got.At.Format(time.RFC3339))
	assert.Equal(t, "2026-09-05", got.Day)
}

func TestResolveCapturedUnknownAndImplausible(t *testing.T) {
	home := homeSG(t)
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)

	missing := media.ResolveCaptured(media.Metadata{}, home, now)
	assert.Equal(t, domain.CapturedUnknown, missing.Confidence)
	assert.Equal(t, media.MetaErrorNoMetadata, missing.Error)
	assert.Nil(t, missing.At)

	future := media.ResolveCaptured(media.Metadata{
		DateTimeOriginal: now.Add(72 * time.Hour), HasOffset: true,
	}, home, now)
	assert.Equal(t, domain.CapturedUnknown, future.Confidence)
	assert.Equal(t, media.MetaErrorImplausibleDatetime, future.Error)

	ancient := media.ResolveCaptured(media.Metadata{
		DateTimeOriginal: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), HasOffset: true,
	}, home, now)
	assert.Equal(t, domain.CapturedUnknown, ancient.Confidence)
	assert.Equal(t, media.MetaErrorImplausibleDatetime, ancient.Error)
}

func TestResolveCapturedAcceptsBoundaryValues(t *testing.T) {
	home := homeSG(t)
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)

	epoch := media.ResolveCaptured(media.Metadata{
		DateTimeOriginal: time.Unix(0, 0).UTC(), HasOffset: true,
	}, home, now)
	assert.Equal(t, domain.CapturedExact, epoch.Confidence)

	tomorrow := media.ResolveCaptured(media.Metadata{
		DateTimeOriginal: now.Add(23 * time.Hour), HasOffset: true,
	}, home, now)
	assert.Equal(t, domain.CapturedExact, tomorrow.Confidence)
}

func TestFingerprintIsStableAndSensitive(t *testing.T) {
	data := bytes.Repeat([]byte("atrium"), 30000) // larger than one chunk
	r := bytes.NewReader(data)

	a, err := media.Fingerprint(r, int64(len(data)), 1000)
	require.NoError(t, err)
	b, err := media.Fingerprint(bytes.NewReader(data), int64(len(data)), 1000)
	require.NoError(t, err)
	assert.Equal(t, a, b, "the same bytes and mtime must produce the same fingerprint")

	c, err := media.Fingerprint(bytes.NewReader(data), int64(len(data)), 1001)
	require.NoError(t, err)
	assert.NotEqual(t, a, c, "a changed mtime must invalidate the preview")

	changed := append([]byte{}, data...)
	changed[len(changed)-1] = 'X'
	d, err := media.Fingerprint(bytes.NewReader(changed), int64(len(changed)), 1000)
	require.NoError(t, err)
	assert.NotEqual(t, a, d, "a change in the tail must be detected")
}

func TestFingerprintSmallAndEmptyFiles(t *testing.T) {
	small := []byte("hi")
	a, err := media.Fingerprint(bytes.NewReader(small), int64(len(small)), 7)
	require.NoError(t, err)
	assert.Len(t, a, 64)

	empty, err := media.Fingerprint(bytes.NewReader(nil), 0, 7)
	require.NoError(t, err)
	assert.NotEqual(t, a, empty)
}
