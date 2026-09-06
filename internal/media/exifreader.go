package media

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/evanoberholster/imagemeta"
)

// ExifReader is the default MetadataReader, backed by
// github.com/evanoberholster/imagemeta (design D9). It is the only place in
// Atrium that knows which EXIF library is in use.
type ExifReader struct{}

// NewExifReader builds the default metadata reader.
func NewExifReader() *ExifReader { return &ExifReader{} }

// Read implements MetadataReader.
func (ExifReader) Read(_ context.Context, r io.ReadSeeker) (Metadata, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Metadata{}, err
	}
	ex, err := imagemeta.Decode(r)
	if err != nil {
		if errors.Is(err, imagemeta.ErrNoExif) || errors.Is(err, imagemeta.ErrMetadataNotSupported) ||
			errors.Is(err, imagemeta.ErrImageTypeNotFound) {
			return Metadata{}, ErrNoMetadata
		}
		// A partially readable directory still yields usable tags; only treat
		// the read as failed when nothing at all came back.
		if ex.ExifIFD.DateTimeOriginal.IsZero() && ex.IFD0.Orientation == 0 {
			return Metadata{}, ErrNoMetadata
		}
	}

	out := Metadata{
		Orientation: int(ex.IFD0.Orientation),
		Width:       int(ex.ExifIFD.PixelXDimension),
		Height:      int(ex.ExifIFD.PixelYDimension),
	}
	if out.Width == 0 && ex.IFD0.ImageWidth > 0 {
		out.Width = int(ex.IFD0.ImageWidth)
		out.Height = int(ex.IFD0.ImageHeight)
	}

	original := ex.ExifIFD.DateTimeOriginal
	if original.IsZero() {
		if out.Orientation == 0 && out.Width == 0 {
			return Metadata{}, ErrNoMetadata
		}
		return out, nil
	}
	// The library normalises the tag into the offset location when
	// OffsetTimeOriginal is present and into UTC otherwise. Only the wall
	// clock components are trustworthy, so they are recombined explicitly.
	if loc := ex.ExifIFD.OffsetTimeOriginal; loc != nil {
		out.HasOffset = true
		out.DateTimeOriginal = wallClockIn(original, loc)
		_, offset := out.DateTimeOriginal.Zone()
		out.OffsetSeconds = offset
		return out, nil
	}
	out.DateTimeOriginal = wallClockIn(original, time.UTC)
	return out, nil
}

// wallClockIn rebuilds a timestamp from its calendar components in loc,
// without shifting the instant.
func wallClockIn(t time.Time, loc *time.Location) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
}
