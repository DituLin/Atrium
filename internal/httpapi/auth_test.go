package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/auth"
)

// credential names the token a matrix case presents.
type credential int

const (
	credNone credential = iota
	credScreenBearer
	credAdminBearer
	credScreenCookie
	credBadBearer
)

func TestAuthMatrixCredentialByRouteScope(t *testing.T) {
	cases := []struct {
		name      string
		cred      credential
		origin    string
		fetchSite string
		referer   string
		method    string
		path      string
		want      int
		wantErr   string
	}{
		{name: "screen bearer on screen route", cred: credScreenBearer,
			method: "GET", path: "/api/v1/home", want: http.StatusOK},
		{name: "admin bearer on screen route", cred: credAdminBearer,
			method: "GET", path: "/api/v1/home", want: http.StatusOK},
		{name: "screen cookie with allowed origin", cred: credScreenCookie, origin: testOrigin,
			method: "GET", path: "/api/v1/home", want: http.StatusOK},
		{name: "screen cookie without origin or metadata", cred: credScreenCookie,
			method: "GET", path: "/api/v1/home", want: http.StatusForbidden, wantErr: "forbidden"},
		{name: "screen cookie same-origin fetch metadata", cred: credScreenCookie, fetchSite: "same-origin",
			method: "GET", path: "/api/v1/home", want: http.StatusOK},
		{name: "screen cookie navigation metadata", cred: credScreenCookie, fetchSite: "none",
			method: "GET", path: "/api/v1/home", want: http.StatusOK},
		{name: "screen cookie cross-site fetch metadata", cred: credScreenCookie, fetchSite: "cross-site",
			method: "GET", path: "/api/v1/home", want: http.StatusForbidden, wantErr: "forbidden"},
		{name: "screen cookie with allowed referer", cred: credScreenCookie, referer: testOrigin + "/dashboard",
			method: "GET", path: "/api/v1/home", want: http.StatusOK},
		{name: "screen cookie with foreign referer", cred: credScreenCookie, referer: "https://evil.example/x",
			method: "GET", path: "/api/v1/home", want: http.StatusForbidden, wantErr: "forbidden"},
		{name: "screen cookie with foreign origin", cred: credScreenCookie, origin: "https://evil.example",
			method: "GET", path: "/api/v1/home", want: http.StatusForbidden, wantErr: "forbidden"},
		{name: "screen cookie on loopback origin", cred: credScreenCookie, origin: "https://127.0.0.1:8443",
			method: "GET", path: "/api/v1/home", want: http.StatusOK},
		{name: "screen bearer on admin route", cred: credScreenBearer,
			method: "GET", path: "/api/v1/screens", want: http.StatusForbidden, wantErr: "forbidden"},
		{name: "admin bearer on admin route", cred: credAdminBearer,
			method: "GET", path: "/api/v1/screens", want: http.StatusOK},
		{name: "screen cookie on admin route", cred: credScreenCookie, origin: testOrigin,
			method: "GET", path: "/api/v1/screens", want: http.StatusUnauthorized},
		{name: "no credential on screen route", cred: credNone,
			method: "GET", path: "/api/v1/home", want: http.StatusUnauthorized, wantErr: "unauthorized"},
		{name: "malformed bearer", cred: credBadBearer,
			method: "GET", path: "/api/v1/home", want: http.StatusUnauthorized},
		{name: "no credential on health", cred: credNone,
			method: "GET", path: "/health/live", want: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// One harness per case keeps the failure limiter from leaking state.
			h := newHarness(t)
			adminToken := h.newAdminToken()
			screenToken := h.pairScreen(adminToken, "living_room_tv", "Living room")

			var opts []func(*http.Request)
			switch tc.cred {
			case credScreenBearer:
				opts = append(opts, bearer(screenToken))
			case credAdminBearer:
				opts = append(opts, bearer(adminToken))
			case credScreenCookie:
				opts = append(opts, cookie(screenToken))
			case credBadBearer:
				opts = append(opts, bearer("not-a-token"))
			case credNone:
			}
			if tc.origin != "" {
				opts = append(opts, origin(tc.origin))
			}
			if tc.fetchSite != "" {
				opts = append(opts, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", tc.fetchSite) })
			}
			if tc.referer != "" {
				opts = append(opts, func(r *http.Request) { r.Header.Set("Referer", tc.referer) })
			}
			resp := h.request(tc.method, tc.path, "", opts...)
			require.Equal(t, tc.want, resp.StatusCode)
			if tc.wantErr != "" {
				require.Equal(t, tc.wantErr, errorCode(t, resp))
			}
		})
	}
}

func TestCookieIsNeverAcceptedOnAdminRoutes(t *testing.T) {
	h := newHarness(t)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "Living room")

	// Even a valid screen cookie from an allowed origin cannot reach admin.
	resp := h.request(http.MethodGet, "/api/v1/screens", "", cookie(screen), origin(testOrigin))
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCookieIsRejectedOnNonGetRoutes(t *testing.T) {
	h := newHarness(t)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodDelete, "/api/v1/screens/living_room_tv", "", cookie(screen), origin(testOrigin))
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"cookie credentials are never honoured outside GET")
}

func TestAuthFailureLimiterReturns429(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < auth.AuthFailuresPerMinute; i++ {
		resp := h.request(http.MethodGet, "/api/v1/home", "", bearer("atr_scr_"+badSecret))
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "failure %d", i+1)
	}
	limited := h.request(http.MethodGet, "/api/v1/home", "", bearer("atr_scr_"+badSecret))
	require.Equal(t, http.StatusTooManyRequests, limited.StatusCode)
	require.Equal(t, "rate_limited", errorCode(t, limited))

	// A valid credential is blocked too while the cooldown lasts.
	admin := h.newAdminToken()
	blocked := h.request(http.MethodGet, "/api/v1/screens", "", bearer(admin))
	require.Equal(t, http.StatusTooManyRequests, blocked.StatusCode)

	h.clock.Advance(auth.AuthBlockDuration + 1)
	recovered := h.request(http.MethodGet, "/api/v1/screens", "", bearer(admin))
	require.Equal(t, http.StatusOK, recovered.StatusCode, "the cooldown must expire")
}

// badSecret is a well-formed but unknown 43-character token secret.
const badSecret = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestRevokedScreenLosesAccessImmediately(t *testing.T) {
	h := newHarness(t)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "Living room")

	ok := h.request(http.MethodGet, "/api/v1/home", "", bearer(screen))
	require.Equal(t, http.StatusOK, ok.StatusCode)

	revoke := h.request(http.MethodDelete, "/api/v1/screens/living_room_tv", "", bearer(admin))
	require.Equal(t, http.StatusOK, revoke.StatusCode)
	require.Equal(t, "revoked", decode[map[string]any](t, revoke)["status"])

	after := h.request(http.MethodGet, "/api/v1/home", "", bearer(screen))
	require.Equal(t, http.StatusGone, after.StatusCode)
	require.Equal(t, "screen_revoked", errorCode(t, after))

	// The pairing history is gone as well.
	pairings, err := h.db.Pairings().List(context.Background(), 10)
	require.NoError(t, err)
	require.Empty(t, pairings)
}

func TestAdminTokenRevocationBlocksAccess(t *testing.T) {
	h := newHarness(t)
	admin := h.newAdminToken()
	ok := h.request(http.MethodGet, "/api/v1/screens", "", bearer(admin))
	require.Equal(t, http.StatusOK, ok.StatusCode)

	_, err := h.db.AdminTokens().RevokeAll(context.Background(), h.clock.Now())
	require.NoError(t, err)

	after := h.request(http.MethodGet, "/api/v1/screens", "", bearer(admin))
	require.Equal(t, http.StatusUnauthorized, after.StatusCode)
}
