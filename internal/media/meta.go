// Package media owns everything that touches image bytes: metadata
// extraction, preview and thumbnail generation, HEIC conversion and the cache
// budget janitor (technical design §6.3, tasks B-206..B-209).
package media

import (
	"context"
	"errors"
	"io"
	"time"
)

// Metadata is the subset of EXIF Atrium needs. Everything else is deliberately
// dropped: previews are re-encoded, so no camera, lens or GPS value ever
// leaves this package.
type Metadata struct {
	// DateTimeOriginal carries the wall-clock capture time. Its location is
	// meaningful only when HasOffset is true.
	DateTimeOriginal time.Time
	// HasOffset reports whether OffsetTimeOriginal (EXIF 2.31) was present.
	HasOffset bool
	// OffsetSeconds is the original UTC offset when HasOffset is true.
	OffsetSeconds int
	// Orientation is the EXIF value 1..8, or 0 when absent.
	Orientation int
	// Width and Height come from the EXIF pixel dimensions when present.
	Width  int
	Height int
}

// ErrNoMetadata means the file carries no readable EXIF. It is an expected
// outcome (PNG, stripped JPEG), not a failure.
var ErrNoMetadata = errors.New("media: no metadata")

// MetadataReader extracts EXIF from an image stream. It exists as an
// interface so the library behind it can be swapped without touching the job
// handlers (design D9, dev plan §10 risk row).
type MetadataReader interface {
	Read(ctx context.Context, r io.ReadSeeker) (Metadata, error)
}
