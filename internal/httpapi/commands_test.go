package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/DituLin/Atrium/internal/screen"
)

// stubSessions answers presence and records revocations without sockets.
type stubSessions struct {
	online  map[string]bool
	revoked []string
	deliver error
}

func (s *stubSessions) Online(id string) bool { return s.online[id] }

func (s *stubSessions) Revoke(id, _ string) { s.revoked = append(s.revoked, id) }

func (s *stubSessions) Deliver(string, *domain.Command) error { return s.deliver }

// newCommandHarness wires the command service and a presence stub.
func newCommandHarness(t *testing.T, online bool) (*harness, *stubSessions) {
	t.Helper()
	sessions := &stubSessions{online: map[string]bool{"living_room_tv": online}}
	h := newHarnessWith(t, func(h *harness, deps *httpapi.Deps) {
		deps.Sessions = sessions
		deps.Commands = screen.NewService(screen.Options{
			DB: h.db, Hub: sessions, Logger: logging.Discard(),
			Now: deps.Now, TTL: 10 * time.Second,
		})
	})
	return h, sessions
}

func TestIssueCommandAcceptsAndReturnsTheCommand(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodPost, "/api/v1/screens/living_room_tv/commands",
		`{"kind":"navigate","payload":{"route":"photos","collection":"recent"}}`, bearer(admin))
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	body := decode[map[string]any](t, resp)
	assert.Equal(t, "navigate", body["kind"])
	assert.Equal(t, "accepted", body["status"])
	assert.EqualValues(t, 1, body["sequence"])
	assert.NotEmpty(t, body["id"])
	assert.NotEmpty(t, body["expires_at"])

	// The command is readable by ID and appears in the listing.
	get := h.request(http.MethodGet, "/api/v1/commands/"+body["id"].(string), "", bearer(admin))
	require.Equal(t, http.StatusOK, get.StatusCode)

	list := h.request(http.MethodGet, "/api/v1/commands?screen_id=living_room_tv", "", bearer(admin))
	require.Equal(t, http.StatusOK, list.StatusCode)
	listed := decode[struct {
		Commands []map[string]any `json:"commands"`
	}](t, list)
	require.Len(t, listed.Commands, 1)
}

func TestIssueCommandToOfflineScreenConflicts(t *testing.T) {
	h, _ := newCommandHarness(t, false)
	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodPost, "/api/v1/screens/living_room_tv/commands",
		`{"kind":"refresh","payload":{}}`, bearer(admin))
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	body := decode[map[string]any](t, resp)
	require.Equal(t, "screen_offline", body["error"].(map[string]any)["code"])
	// The 409 carries the command that was recorded as failed, so the caller
	// can see exactly what happened rather than guessing (design §6.5).
	cmd := body["command"].(map[string]any)
	assert.Equal(t, "failed", cmd["status"])
	assert.Equal(t, "screen_offline", cmd["error_code"])
}

func TestIssueCommandValidationFailures(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")

	cases := map[string]string{
		"unknown kind":       `{"kind":"reboot","payload":{}}`,
		"forbidden route":    `{"kind":"navigate","payload":{"route":"pair"}}`,
		"unknown collection": `{"kind":"navigate","payload":{"route":"photos","collection":"secret"}}`,
		"show without photo": `{"kind":"show","payload":{}}`,
		"show unknown photo": `{"kind":"show","payload":{"photo_id":"01JNOPE"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := h.request(http.MethodPost, "/api/v1/screens/living_room_tv/commands", body, bearer(admin))
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			assert.Equal(t, "invalid_command", errorCode(t, resp))
		})
	}
}

func TestIssueCommandToUnknownScreenIsNotFound(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	admin := h.newAdminToken()
	resp := h.request(http.MethodPost, "/api/v1/screens/kitchen_tv/commands",
		`{"kind":"refresh","payload":{}}`, bearer(admin))
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// Command routes are administrative: a screen credential must not reach them.
func TestCommandRoutesRejectScreenCredentials(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	admin := h.newAdminToken()
	screenToken := h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodPost, "/api/v1/screens/living_room_tv/commands",
		`{"kind":"refresh","payload":{}}`, bearer(screenToken))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	listed := h.request(http.MethodGet, "/api/v1/commands", "", bearer(screenToken))
	assert.Equal(t, http.StatusForbidden, listed.StatusCode)
}

func TestCommandsListRejectsAnUnknownStatus(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	admin := h.newAdminToken()
	resp := h.request(http.MethodGet, "/api/v1/commands?status=maybe", "", bearer(admin))
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "invalid_request", errorCode(t, resp))
}

// Revoking a screen closes its live session immediately (PRD §8.2).
func TestRevokeClosesTheLiveSession(t *testing.T) {
	h, sessions := newCommandHarness(t, true)
	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodDelete, "/api/v1/screens/living_room_tv", "", bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []string{"living_room_tv"}, sessions.revoked)
}

// `online` is session presence, not the last authenticated request.
func TestScreenOnlineComesFromSessionPresence(t *testing.T) {
	h, sessions := newCommandHarness(t, true)
	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodGet, "/api/v1/screens", "", bearer(admin))
	listed := decode[struct {
		Screens []map[string]any `json:"screens"`
	}](t, resp)
	require.Len(t, listed.Screens, 1)
	assert.Equal(t, true, listed.Screens[0]["online"])
	assert.Equal(t, true, listed.Screens[0]["registered"])

	sessions.online["living_room_tv"] = false
	resp = h.request(http.MethodGet, "/api/v1/screens/living_room_tv", "", bearer(admin))
	one := decode[map[string]any](t, resp)
	assert.Equal(t, false, one["online"], "no session means offline even moments after a request")
	assert.Equal(t, true, one["registered"])
}

// `/screens/connect` is a literal path registered alongside `/screens/{id}`.
// Go's mux prefers the more specific pattern, so the upgrade must not be
// swallowed by the admin screen route.
func TestConnectRouteBeatsTheScreenIDPattern(t *testing.T) {
	var reached bool
	h := newHarnessWith(t, func(_ *harness, deps *httpapi.Deps) {
		deps.WS = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusTeapot)
		})
	})
	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")

	resp := h.request(http.MethodGet, "/api/v1/screens/connect", "")
	require.Equal(t, http.StatusTeapot, resp.StatusCode)
	assert.True(t, reached)

	// A real screen ID still reaches the admin handler.
	byID := h.request(http.MethodGet, "/api/v1/screens/living_room_tv", "", bearer(admin))
	require.Equal(t, http.StatusOK, byID.StatusCode)
}
