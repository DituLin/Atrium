package media

import (
	"time"

	"github.com/DituLin/Atritum/internal/clock"
	"github.com/DituLin/Atritum/internal/domain"
)

// Metadata error codes stored on photos.meta_error.
const (
	MetaErrorImplausibleDatetime = "implausible_datetime"
	MetaErrorNoMetadata          = "no_metadata"
	MetaErrorReadFailed          = "read_failed"
)

// PlausibleFrom is the earliest capture time Atrium accepts.
var PlausibleFrom = time.Unix(0, 0).UTC()

// PlausibleGrace is how far into the future a capture time may sit before it
// is treated as a broken camera clock.
const PlausibleGrace = 24 * time.Hour

// Captured is the resolved capture time of a photo.
type Captured struct {
	At         *time.Time
	Confidence domain.CapturedConfidence
	// OffsetSeconds is the original UTC offset when it was recorded.
	OffsetSeconds *int
	// Day is the home-timezone calendar day, empty when the time is unknown.
	Day string
	// Error is a meta_error code when the value was rejected.
	Error string
}

// ResolveCaptured applies the rules of PRD §5.2 and design §6.3 step 3.
//
// The file modification time is never consulted: a copy onto the NAS rewrites
// it, so using it would silently claim that the whole library was shot today.
func ResolveCaptured(md Metadata, home *clock.Home, now time.Time) Captured {
	if md.DateTimeOriginal.IsZero() {
		return Captured{Confidence: domain.CapturedUnknown, Error: MetaErrorNoMetadata}
	}

	var (
		at         time.Time
		confidence domain.CapturedConfidence
		offset     *int
	)
	if md.HasOffset {
		at = md.DateTimeOriginal
		confidence = domain.CapturedExact
		seconds := md.OffsetSeconds
		offset = &seconds
	} else {
		// No offset tag: the wall clock is interpreted in the home timezone
		// and the result is flagged as an inference, never as fact.
		wall := md.DateTimeOriginal
		at = time.Date(wall.Year(), wall.Month(), wall.Day(),
			wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), home.Location())
		confidence = domain.CapturedInferred
	}

	if !plausible(at, now) {
		return Captured{Confidence: domain.CapturedUnknown, Error: MetaErrorImplausibleDatetime}
	}
	utc := at.UTC()
	return Captured{
		At:            &utc,
		Confidence:    confidence,
		OffsetSeconds: offset,
		Day:           home.HomeDay(at),
	}
}

// plausible bounds a capture time to [1970, now + 1 day].
func plausible(at, now time.Time) bool {
	return !at.Before(PlausibleFrom) && !at.After(now.Add(PlausibleGrace))
}
