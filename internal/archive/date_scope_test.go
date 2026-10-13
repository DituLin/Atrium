package archive

import "testing"

func TestDateWithinBoundsSourcePrecision(t *testing.T) {
	for _, c := range []struct {
		value, from, to, zone string
		want                  bool
	}{
		{"2026-10-03T18:00:00Z", "2026-10-04", "2026-10-04", "Asia/Singapore", true},
		{"2026-10-03T18:00:00Z", "2026-10-03", "2026-10-03", "Asia/Singapore", false},
		{"2026-10-04T16:00:00Z", "2026-10-04", "2026-10-04", "Asia/Singapore", false},
		{"2026-10", "2026-10-01", "2026-10-31", "Asia/Singapore", true},
		{"2026-10", "2026-10-04", "2026-10-04", "Asia/Singapore", false},
		{"2026", "2026-01-01", "2026-12-31", "Asia/Singapore", true},
		{"2026", "2026-10-01", "2026-10-31", "Asia/Singapore", false},
		{"2026-10-04", "2026-10-04", "2026-10-04", "Asia/Singapore", true},
		{"unknown", "2026-10-01", "2026-10-31", "Asia/Singapore", false},
		{"2026-03-09T03:59:59Z", "2026-03-08", "2026-03-08", "America/New_York", true},
		{"2026-03-09T04:00:00Z", "2026-03-08", "2026-03-08", "America/New_York", false},
	} {
		if got := DateWithinBounds(c.value, c.from, c.to, c.zone); got != c.want {
			t.Errorf("%+v got %v", c, got)
		}
	}
}
