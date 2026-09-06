package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/cli"
)

// controlServer answers the command routes and lets a test script the status
// the polling loop sees on each `GET /commands/{id}`.
type controlServer struct {
	*httptest.Server
	issuedBody atomic.Value // string
	issueCode  atomic.Int32
	statuses   []string
	polls      atomic.Int32
	lastIssued atomic.Value // map[string]any
}

func newControlServer(t *testing.T, statuses ...string) *controlServer {
	t.Helper()
	cs := &controlServer{statuses: statuses}
	cs.issueCode.Store(http.StatusAccepted)
	cs.issuedBody.Store(`{"id":"01JCMD","screen_id":"living_room_tv","sequence":4,"kind":"navigate",
		"payload":{"route":"photos"},"issued_at":"2026-09-06T12:00:00Z",
		"expires_at":"2026-09-06T12:00:10Z","status":"accepted"}`)

	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+adminToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"no credential"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/commands"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			cs.lastIssued.Store(body)
			w.WriteHeader(int(cs.issueCode.Load()))
			_, _ = w.Write([]byte(cs.issuedBody.Load().(string)))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/commands/"):
			i := int(cs.polls.Add(1)) - 1
			if i >= len(cs.statuses) {
				i = len(cs.statuses) - 1
			}
			_, _ = w.Write([]byte(cs.pollBody(cs.statuses[i])))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"no route"}}`))
		}
	}))
	t.Cleanup(cs.Close)
	return cs
}

func (cs *controlServer) pollBody(status string) string {
	extra := ""
	if status == "unknown" {
		extra = `,"error_code":"ack_timeout","result":{"observed":{"applied_sequence":3,` +
			`"route":{"name":"dashboard"}}}`
	}
	return `{"id":"01JCMD","screen_id":"living_room_tv","sequence":4,"kind":"navigate",
		"payload":{"route":"photos"},"issued_at":"2026-09-06T12:00:00Z",
		"expires_at":"2026-09-06T12:00:10Z","status":"` + status + `"` + extra + `}`
}

func TestScreenNavigateIssuesTheCommand(t *testing.T) {
	cs := newControlServer(t, "accepted")
	out, err := run(t, "admin", "screen", "navigate", "living_room_tv",
		"--route", "photos", "--collection", "recent", "--url", cs.URL, "--token", adminToken)
	require.NoError(t, err, out)
	assert.Contains(t, out, "01JCMD")
	assert.Contains(t, out, "accepted")

	body := cs.lastIssued.Load().(map[string]any)
	assert.Equal(t, "navigate", body["kind"])
	payload := body["payload"].(map[string]any)
	assert.Equal(t, "photos", payload["route"])
	assert.Equal(t, "recent", payload["collection"])
}

// `--wait` follows the command to a terminal state and exits zero only on
// applied, which is what makes it usable in an acceptance script (FR-18).
func TestScreenNavigateWaitSucceedsOnApplied(t *testing.T) {
	cs := newControlServer(t, "accepted", "accepted", "applied")
	out, err := run(t, "admin", "screen", "navigate", "living_room_tv",
		"--route", "dashboard", "--wait", "--url", cs.URL, "--token", adminToken)
	require.NoError(t, err, out)
	assert.Contains(t, out, "applied")
	assert.Contains(t, out, "elapsed")
	assert.GreaterOrEqual(t, cs.polls.Load(), int32(3))
}

func TestScreenNavigateWaitFailsOnUnknown(t *testing.T) {
	cs := newControlServer(t, "unknown")
	out, err := run(t, "admin", "screen", "navigate", "living_room_tv",
		"--route", "dashboard", "--wait", "--url", cs.URL, "--token", adminToken)
	require.Error(t, err)
	assert.ErrorIs(t, err, cli.ErrCommandNotApplied)
	assert.Contains(t, err.Error(), "ack_timeout")
	// The operator is told what the screen reports rather than a bare failure.
	assert.Contains(t, out, "observed")
	assert.Contains(t, out, "sequence 3")
}

// An offline screen is a 409 that still carries the recorded command.
func TestScreenRefreshOnOfflineScreenPrintsTheFailedCommand(t *testing.T) {
	cs := newControlServer(t)
	cs.issueCode.Store(http.StatusConflict)
	cs.issuedBody.Store(`{"error":{"code":"screen_offline","message":"screen has no live session"},
		"command":{"id":"01JCMD","screen_id":"living_room_tv","sequence":4,"kind":"refresh",
		"payload":{},"issued_at":"2026-09-06T12:00:00Z","expires_at":"2026-09-06T12:00:10Z",
		"status":"failed","error_code":"screen_offline"}}`)

	out, err := run(t, "admin", "screen", "refresh", "living_room_tv",
		"--url", cs.URL, "--token", adminToken)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "offline")
	assert.Contains(t, out, "failed (screen_offline)")
	assert.Zero(t, cs.polls.Load(), "nothing is queued for an offline screen")
}

func TestScreenShowRequiresAPhoto(t *testing.T) {
	cs := newControlServer(t, "applied")
	_, err := run(t, "admin", "screen", "show", "living_room_tv", "--url", cs.URL, "--token", adminToken)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--photo is required")

	out, err := run(t, "admin", "screen", "show", "living_room_tv",
		"--photo", "01JPHOTO", "--url", cs.URL, "--token", adminToken)
	require.NoError(t, err, out)
	payload := cs.lastIssued.Load().(map[string]any)["payload"].(map[string]any)
	assert.Equal(t, "01JPHOTO", payload["photo_id"])
}

func TestCommandsGetAndList(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/commands/01JCMD": `{"id":"01JCMD","screen_id":"living_room_tv","sequence":4,
			"kind":"show","payload":{"photo_id":"01JPHOTO"},"issued_at":"2026-09-06T12:00:00Z",
			"expires_at":"2026-09-06T12:00:10Z","status":"applied",
			"result":{"route":{"name":"photo","photo_id":"01JPHOTO"},"resource_id":"01JPHOTO"}}`,
		"GET /api/v1/commands": `{"commands":[{"id":"01JCMD","screen_id":"living_room_tv",
			"sequence":4,"kind":"show","payload":{},"issued_at":"2026-09-06T12:00:00Z",
			"expires_at":"2026-09-06T12:00:10Z","status":"applied"}]}`,
	})

	out, err := run(t, "admin", "commands", "get", "01JCMD", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	assert.Contains(t, out, "applied")
	assert.Contains(t, out, "photo#01JPHOTO")

	listed, err := run(t, "admin", "commands", "list", "--screen", "living_room_tv",
		"--status", "applied", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, listed)
	assert.Contains(t, listed, "01JCMD")
	assert.Contains(t, listed, "SEQ")

	jsonOut, err := run(t, "admin", "commands", "list", "--json", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, jsonOut)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &parsed))
	assert.Len(t, parsed["commands"], 1)
}

func TestAdminTokenRotatePrintsTheNewToken(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"POST /api/v1/admin/token/rotate": `{"token":"atr_adm_` + testSecret +
			`","created_at":"2026-09-06T12:00:00Z","note":"store this now"}`,
	})
	out, err := run(t, "admin", "token", "rotate", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	assert.Contains(t, out, "atr_adm_")
	assert.Contains(t, out, "store this now")
}
