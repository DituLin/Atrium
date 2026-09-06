package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/clock"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}

func TestHomeDayUsesTheHomeTimezoneNotUTC(t *testing.T) {
	sg := mustLoad(t, "Asia/Singapore")
	// 2026-09-05 17:30 UTC is already 2026-09-06 in Singapore (UTC+8).
	instant := time.Date(2026, 9, 5, 17, 30, 0, 0, time.UTC)
	require.Equal(t, "2026-09-06", clock.HomeDay(instant, sg))
	require.Equal(t, "2026-09-05", clock.HomeDay(instant, time.UTC))

	// Just before local midnight the day is still the fifth.
	require.Equal(t, "2026-09-05", clock.HomeDay(time.Date(2026, 9, 5, 15, 59, 59, 0, time.UTC), sg))
}

func TestDayBoundsAreLeftClosedRightOpen(t *testing.T) {
	sg := mustLoad(t, "Asia/Singapore")
	start, end := clock.DayBounds(time.Date(2026, 9, 5, 17, 30, 0, 0, time.UTC), sg)

	require.Equal(t, "2026-09-06T00:00:00+08:00", start.Format(time.RFC3339))
	require.Equal(t, "2026-09-07T00:00:00+08:00", end.Format(time.RFC3339))
	require.Equal(t, 24*time.Hour, end.Sub(start))
	require.Equal(t, "2026-09-06", clock.HomeDay(start, sg))
	require.Equal(t, "2026-09-07", clock.HomeDay(end, sg), "the end bound belongs to the next day")
}

func TestDayBoundsAcrossDSTSpringForward(t *testing.T) {
	berlin := mustLoad(t, "Europe/Berlin")
	// 2026-03-29 is the spring-forward day in Berlin: the local day is 23 hours.
	start, end := clock.DayBounds(time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC), berlin)
	require.Equal(t, "2026-03-29T00:00:00+01:00", start.Format(time.RFC3339))
	require.Equal(t, "2026-03-30T00:00:00+02:00", end.Format(time.RFC3339))
	require.Equal(t, 23*time.Hour, end.Sub(start))
}

func TestDayBoundsAcrossDSTFallBack(t *testing.T) {
	berlin := mustLoad(t, "Europe/Berlin")
	// 2026-10-25 is the fall-back day in Berlin: the local day is 25 hours.
	start, end := clock.DayBounds(time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC), berlin)
	require.Equal(t, "2026-10-25T00:00:00+02:00", start.Format(time.RFC3339))
	require.Equal(t, "2026-10-26T00:00:00+01:00", end.Format(time.RFC3339))
	require.Equal(t, 25*time.Hour, end.Sub(start))
}

func TestOffsetAt(t *testing.T) {
	berlin := mustLoad(t, "Europe/Berlin")
	require.Equal(t, 3600, clock.OffsetAt(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), berlin))
	require.Equal(t, 7200, clock.OffsetAt(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), berlin))

	sg := mustLoad(t, "Asia/Singapore")
	require.Equal(t, 8*3600, clock.OffsetAt(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), sg))
	require.Equal(t, 8*3600, clock.OffsetAt(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), sg))
}

func TestNextTransitionForZoneWithDST(t *testing.T) {
	berlin := mustLoad(t, "Europe/Berlin")

	// From mid-January the next change is the spring-forward on 29 March.
	next := clock.NextTransition(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), berlin)
	require.NotNil(t, next)
	require.Equal(t, "2026-03-29T01:00:00Z", next.Format(time.RFC3339))

	// From July the next change is the fall-back on 25 October.
	next = clock.NextTransition(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), berlin)
	require.NotNil(t, next)
	require.Equal(t, "2026-10-25T01:00:00Z", next.Format(time.RFC3339))
}

func TestNextTransitionIsNilForZoneWithoutDST(t *testing.T) {
	sg := mustLoad(t, "Asia/Singapore")
	require.Nil(t, clock.NextTransition(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), sg))
	require.Nil(t, clock.NextTransition(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), time.UTC))
}

func TestHomeWrapsClockAndLocation(t *testing.T) {
	fake := clock.NewFake(time.Date(2026, 9, 5, 17, 30, 0, 0, time.UTC))
	home, err := clock.NewHome("Asia/Singapore", fake)
	require.NoError(t, err)

	require.Equal(t, "Asia/Singapore", home.Location().String())
	require.Equal(t, "2026-09-06", home.Today())
	require.Equal(t, 8*3600, home.OffsetAt(fake.Now()))
	require.Nil(t, home.NextTransition(fake.Now()))
	require.Equal(t, "2026-09-06T01:30:00+08:00", home.Now().Format(time.RFC3339))

	fake.Advance(24 * time.Hour)
	require.Equal(t, "2026-09-07", home.Today())

	_, err = clock.NewHome("Not/AZone", fake)
	require.Error(t, err)
}

func TestParseDay(t *testing.T) {
	sg := mustLoad(t, "Asia/Singapore")
	start, end, err := clock.ParseDay("2026-09-06", sg)
	require.NoError(t, err)
	require.Equal(t, "2026-09-06T00:00:00+08:00", start.Format(time.RFC3339))
	require.Equal(t, "2026-09-07T00:00:00+08:00", end.Format(time.RFC3339))

	_, _, err = clock.ParseDay("not-a-day", sg)
	require.Error(t, err)
}

func TestFakeClockIsControllable(t *testing.T) {
	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	fake := clock.NewFake(base)
	require.Equal(t, base, fake.Now())
	fake.Advance(90 * time.Minute)
	require.Equal(t, base.Add(90*time.Minute), fake.Now())
	fake.Set(base)
	require.Equal(t, base, fake.Now())
}
