// Package clock provides the home timezone helpers used across Atrium.
package clock

import (
	"fmt"
	"time"
)

// Clock abstracts time so tests can freeze it.
type Clock interface {
	Now() time.Time
}

// SystemClock reads the wall clock.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// Fake is a controllable clock for tests.
type Fake struct{ t time.Time }

// NewFake creates a fake clock positioned at t.
func NewFake(t time.Time) *Fake { return &Fake{t: t} }

// Now returns the fake time.
func (f *Fake) Now() time.Time { return f.t }

// Set moves the fake clock.
func (f *Fake) Set(t time.Time) { f.t = t }

// Advance moves the fake clock forward.
func (f *Fake) Advance(d time.Duration) { f.t = f.t.Add(d) }

// Home binds a clock to the configured home timezone.
type Home struct {
	loc   *time.Location
	clock Clock
}

// NewHome builds a Home clock for an IANA timezone name.
func NewHome(tz string, c Clock) (*Home, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", tz, err)
	}
	if c == nil {
		c = SystemClock{}
	}
	return &Home{loc: loc, clock: c}, nil
}

// Location returns the home location.
func (h *Home) Location() *time.Location { return h.loc }

// Now returns the current instant in the home location.
func (h *Home) Now() time.Time { return h.clock.Now().In(h.loc) }

// HomeDay renders t as YYYY-MM-DD in the home timezone.
func (h *Home) HomeDay(t time.Time) string { return HomeDay(t, h.loc) }

// Today renders the current home day.
func (h *Home) Today() string { return HomeDay(h.clock.Now(), h.loc) }

// DayBounds returns the left-closed, right-open UTC bounds of the home day
// containing t.
func (h *Home) DayBounds(t time.Time) (time.Time, time.Time) { return DayBounds(t, h.loc) }

// OffsetAt returns the UTC offset in seconds at t.
func (h *Home) OffsetAt(t time.Time) int { return OffsetAt(t, h.loc) }

// NextTransition returns the next UTC-offset change after t, if any within a year.
func (h *Home) NextTransition(t time.Time) *time.Time { return NextTransition(t, h.loc) }

// HomeDay renders t as YYYY-MM-DD in loc.
func HomeDay(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

// DayBounds returns [start, end) of the local day containing t.
func DayBounds(t time.Time, loc *time.Location) (time.Time, time.Time) {
	l := t.In(loc)
	start := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	return start, end
}

// ParseDay parses a YYYY-MM-DD home day into its [start, end) bounds.
func ParseDay(day string, loc *time.Location) (time.Time, time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse day %q: %w", day, err)
	}
	return d, d.AddDate(0, 0, 1), nil
}

// OffsetAt returns the UTC offset in seconds at t for loc.
func OffsetAt(t time.Time, loc *time.Location) int {
	_, off := t.In(loc).Zone()
	return off
}

// NextTransition finds the next UTC-offset change strictly after t, searching
// up to 400 days ahead. It returns nil when the zone has no upcoming change.
func NextTransition(t time.Time, loc *time.Location) *time.Time {
	base := OffsetAt(t, loc)
	// Coarse scan by hour, then binary search to the second.
	const horizon = 400 * 24 * time.Hour
	lo := t
	for step := time.Hour; ; {
		next := lo.Add(step)
		if next.Sub(t) > horizon {
			return nil
		}
		if OffsetAt(next, loc) != base {
			hi := next
			for hi.Sub(lo) > time.Second {
				mid := lo.Add(hi.Sub(lo) / 2)
				if OffsetAt(mid, loc) == base {
					lo = mid
				} else {
					hi = mid
				}
			}
			res := hi.UTC().Truncate(time.Second)
			return &res
		}
		lo = next
	}
}
