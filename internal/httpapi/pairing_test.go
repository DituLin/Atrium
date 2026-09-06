package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/config"
)

func TestHealthRoutesNeedNoCredential(t *testing.T) {
	h := newHarness(t)

	live := h.request(http.MethodGet, "/health/live", "")
	require.Equal(t, http.StatusOK, live.StatusCode)
	require.Equal(t, "ok", decode[map[string]any](t, live)["status"])

	ready := h.request(http.MethodGet, "/health/ready", "")
	require.Equal(t, http.StatusOK, ready.StatusCode)
	require.Equal(t, "ok", decode[map[string]any](t, ready)["status"])
	require.NotEmpty(t, live.Header.Get("X-Request-Id"), "every response carries a request id")
}

func TestPairingRoundTrip(t *testing.T) {
	h := newHarness(t)
	adminToken := h.newAdminToken()

	start := h.request(http.MethodPost, "/api/v1/pair/start", `{"client_hint":"living room tv"}`)
	require.Equal(t, http.StatusCreated, start.StatusCode)
	started := decode[struct {
		PairingID      string `json:"pairing_id"`
		Code           string `json:"code"`
		ExpiresAt      string `json:"expires_at"`
		PollIntervalMS int    `json:"poll_interval_ms"`
	}](t, start)
	require.Len(t, started.Code, 6)
	require.Equal(t, auth.PairingPollIntervalMS, started.PollIntervalMS)

	status := h.request(http.MethodGet, "/api/v1/pair/"+started.PairingID, "")
	require.Equal(t, http.StatusOK, status.StatusCode)
	require.Equal(t, "pending", decode[map[string]any](t, status)["status"])

	// Claiming before approval is a conflict, not a credential.
	early := h.request(http.MethodPost, "/api/v1/pair/"+started.PairingID+"/claim", "")
	require.Equal(t, http.StatusConflict, early.StatusCode)

	approve := h.request(http.MethodPost, "/api/v1/pairings/"+started.PairingID+"/approve",
		`{"screen_id":"living_room_tv","name":"Living room"}`, bearer(adminToken))
	require.Equal(t, http.StatusOK, approve.StatusCode)

	claim := h.request(http.MethodPost, "/api/v1/pair/"+started.PairingID+"/claim", "")
	require.Equal(t, http.StatusOK, claim.StatusCode)
	claimed := decode[struct {
		ScreenID string `json:"screen_id"`
		Name     string `json:"name"`
		Token    string `json:"token"`
	}](t, claim)
	require.Equal(t, "living_room_tv", claimed.ScreenID)
	require.Equal(t, "Living room", claimed.Name)
	require.True(t, auth.ValidToken(claimed.Token, auth.ScreenPrefix))

	// The cookie carries every attribute design §6.8 requires.
	var set *http.Cookie
	for _, c := range claim.Cookies() {
		if c.Name == auth.ScreenCookieName {
			set = c
		}
	}
	require.NotNil(t, set)
	require.Equal(t, claimed.Token, set.Value)
	require.True(t, set.HttpOnly)
	require.True(t, set.Secure, "TLS is on by default, so the cookie must be Secure")
	require.Equal(t, http.SameSiteStrictMode, set.SameSite)
	require.Equal(t, "/", set.Path)
	require.Equal(t, 31536000, set.MaxAge)

	// A second claim is gone, and the credential still works.
	second := h.request(http.MethodPost, "/api/v1/pair/"+started.PairingID+"/claim", "")
	require.Equal(t, http.StatusGone, second.StatusCode)
	require.Equal(t, "pairing_claimed", errorCode(t, second))

	me := h.request(http.MethodGet, "/api/v1/screens/me", "", bearer(claimed.Token))
	require.Equal(t, http.StatusOK, me.StatusCode)
	require.Equal(t, "living_room_tv", decode[map[string]any](t, me)["id"])
}

func TestClaimDropsSecureWhenTLSIsOff(t *testing.T) {
	h := newHarness(t, func(c *config.Config) {
		c.Server.TLS.Mode = config.TLSOff
		c.Server.PublicURL = "http://192.168.1.10:8443"
	})
	token := h.pairScreen(h.newAdminToken(), "demo_tv", "Demo")
	require.NotEmpty(t, token)

	start := h.request(http.MethodPost, "/api/v1/pair/start", "")
	pairingID := decode[map[string]any](t, start)["pairing_id"].(string)
	admin := h.newAdminToken()
	h.request(http.MethodPost, "/api/v1/pairings/"+pairingID+"/approve",
		`{"screen_id":"second_tv","name":"Second"}`, bearer(admin))
	claim := h.request(http.MethodPost, "/api/v1/pair/"+pairingID+"/claim", "")

	for _, c := range claim.Cookies() {
		if c.Name == auth.ScreenCookieName {
			require.False(t, c.Secure, "a Secure cookie is dropped by browsers on a plain HTTP origin")
			require.True(t, c.HttpOnly)
		}
	}
}

func TestPairingExpiresAfterTTL(t *testing.T) {
	h := newHarness(t)
	start := h.request(http.MethodPost, "/api/v1/pair/start", "")
	pairingID := decode[map[string]any](t, start)["pairing_id"].(string)

	h.clock.Advance(auth.PairingTTL + time.Second)

	status := h.request(http.MethodGet, "/api/v1/pair/"+pairingID, "")
	require.Equal(t, http.StatusOK, status.StatusCode)
	require.Equal(t, "expired", decode[map[string]any](t, status)["status"])

	approve := h.request(http.MethodPost, "/api/v1/pairings/"+pairingID+"/approve",
		`{"screen_id":"late_tv"}`, bearer(h.newAdminToken()))
	require.Equal(t, http.StatusGone, approve.StatusCode)
	require.Equal(t, "pairing_expired", errorCode(t, approve))
}

func TestPairStartRateLimitPerIP(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 5; i++ {
		resp := h.request(http.MethodPost, "/api/v1/pair/start", "")
		require.Equal(t, http.StatusCreated, resp.StatusCode, "attempt %d", i+1)
	}
	limited := h.request(http.MethodPost, "/api/v1/pair/start", "")
	require.Equal(t, http.StatusTooManyRequests, limited.StatusCode)
	require.Equal(t, "rate_limited", errorCode(t, limited))
	require.Equal(t, "60", limited.Header.Get("Retry-After"))
}

func TestPairingCodesAreUnique(t *testing.T) {
	h := newHarness(t)
	seen := map[string]struct{}{}
	for i := 0; i < 5; i++ {
		resp := h.request(http.MethodPost, "/api/v1/pair/start", "")
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		code := decode[map[string]any](t, resp)["code"].(string)
		require.Len(t, code, 6)
		_, dup := seen[code]
		require.False(t, dup, "codes must be unique among live pairings")
		seen[code] = struct{}{}
	}
}

func TestUnknownPairingIsNotFound(t *testing.T) {
	h := newHarness(t)
	resp := h.request(http.MethodGet, "/api/v1/pair/01JDOESNOTEXIST0000000000", "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, "not_found", errorCode(t, resp))
}

func TestAdminPairingListAndReject(t *testing.T) {
	h := newHarness(t)
	admin := h.newAdminToken()
	start := h.request(http.MethodPost, "/api/v1/pair/start", "")
	pairingID := decode[map[string]any](t, start)["pairing_id"].(string)

	list := h.request(http.MethodGet, "/api/v1/pairings", "", bearer(admin))
	require.Equal(t, http.StatusOK, list.StatusCode)
	listed := decode[struct {
		Pairings []map[string]any `json:"pairings"`
	}](t, list)
	require.Len(t, listed.Pairings, 1)

	del := h.request(http.MethodDelete, "/api/v1/pairings/"+pairingID, "", bearer(admin))
	require.Equal(t, http.StatusNoContent, del.StatusCode)

	after := h.request(http.MethodGet, "/api/v1/pair/"+pairingID, "")
	require.Equal(t, http.StatusNotFound, after.StatusCode)
}
