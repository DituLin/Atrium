package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/auth"
)

func TestLimiterAllowsBurstThenRefills(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	l := auth.NewLimiter(auth.LimiterOptions{PerMinute: 5, Now: func() time.Time { return now }})

	for i := 0; i < 5; i++ {
		require.True(t, l.Allow("192.168.1.20"), "burst request %d", i+1)
	}
	require.False(t, l.Allow("192.168.1.20"), "the sixth request in the same minute is refused")
	require.True(t, l.Allow("192.168.1.21"), "the limit is per key")

	now = now.Add(20 * time.Second)
	require.True(t, l.Allow("192.168.1.20"), "the bucket refills over time")
}

func TestLimiterEvictsIdleKeys(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	l := auth.NewLimiter(auth.LimiterOptions{
		PerMinute: 5, MaxKeys: 4, TTL: time.Minute, Now: func() time.Time { return now },
	})
	for i := 0; i < 3; i++ {
		require.True(t, l.Allow(string(rune('a'+i))))
	}
	require.Equal(t, 3, l.Size())

	now = now.Add(2 * time.Minute)
	require.True(t, l.Allow("fresh"))
	require.Equal(t, 1, l.Size(), "keys idle beyond the TTL are dropped")
}

func TestLimiterIsBoundedBySize(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	l := auth.NewLimiter(auth.LimiterOptions{
		PerMinute: 5, MaxKeys: 8, TTL: time.Hour, Now: func() time.Time { return now },
	})
	for i := 0; i < 100; i++ {
		now = now.Add(time.Millisecond)
		l.Allow(string(rune(i)) + "key")
	}
	require.LessOrEqual(t, l.Size(), 8, "the map must stay bounded")
}

func TestFailureLimiterBlocksForCooldown(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	f := auth.NewFailureLimiter(10, time.Minute, func() time.Time { return now })

	for i := 0; i < 10; i++ {
		require.False(t, f.RecordFailure("192.168.1.20"), "failure %d is within the allowance", i+1)
	}
	require.True(t, f.RecordFailure("192.168.1.20"), "the eleventh failure blocks the caller")
	require.True(t, f.Blocked("192.168.1.20"))
	require.False(t, f.Blocked("192.168.1.21"), "the block is per key")
	require.InDelta(t, time.Minute.Seconds(), f.RetryAfter("192.168.1.20").Seconds(), 1)

	now = now.Add(61 * time.Second)
	require.False(t, f.Blocked("192.168.1.20"), "the cooldown expires")
	require.Zero(t, f.RetryAfter("192.168.1.20"))
}

func TestOriginPolicy(t *testing.T) {
	p := auth.NewOriginPolicy([]string{
		"https://192.168.1.10:8443", "https://localhost:8443", "https://127.0.0.1:8443",
	})

	require.True(t, p.Check("https://192.168.1.10:8443"))
	require.True(t, p.Check("HTTPS://192.168.1.10:8443"), "matching is case-insensitive")
	require.True(t, p.Check("https://localhost:8443"))
	require.False(t, p.Check("http://192.168.1.10:8443"), "the scheme is part of the origin")
	require.False(t, p.Check("https://192.168.1.10:9443"), "the port is part of the origin")
	require.False(t, p.Check("https://evil.example"))
	require.False(t, p.Check(""), "a missing Origin is never trusted for a cookie")
	require.False(t, p.Check("null"))
	require.Len(t, p.Allowed(), 3)
}

func TestBearerAndCookieExtraction(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/home", nil)
	require.Empty(t, auth.BearerToken(r))
	require.Empty(t, auth.CookieToken(r))

	r.Header.Set("Authorization", "Bearer atr_scr_value")
	require.Equal(t, "atr_scr_value", auth.BearerToken(r))
	r.Header.Set("Authorization", "bearer atr_scr_value")
	require.Equal(t, "atr_scr_value", auth.BearerToken(r), "the scheme is case-insensitive")
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	require.Empty(t, auth.BearerToken(r), "only the Bearer scheme is accepted")

	r.AddCookie(&http.Cookie{Name: auth.ScreenCookieName, Value: "cookie_token"})
	require.Equal(t, "cookie_token", auth.CookieToken(r))
}

func TestClientIPIgnoresProxyHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/home", nil)
	r.RemoteAddr = "192.168.1.20:54321"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	require.Equal(t, "192.168.1.20", auth.ClientIP(r),
		"a forwarded header would let a caller reset its own rate limit")
}
