package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const adminToken = "atr_adm_" + testSecret

func TestAdminDiagTableAndJSON(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/diagnostics": `{
			"version":"0.1.0+abc1234","uptime_seconds":42,"insecure":false,
			"db":{"path_redacted":true,"size_bytes":8192,"migrations":1},
			"sources":[{"id":"family_photos","health":"online","identity_bound":true,
			            "last_success_at":"2026-09-05T12:00:00Z"}],
			"screens":[{"id":"living_room_tv","online":true,"last_seen_at":"2026-09-05T12:00:00Z",
			            "applied_sequence":3,"open_commands":0}],
			"widgets":{"weather":{"enabled":false}},"pairings":{}}`,
	})

	out, err := run(t, "admin", "diag", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "0.1.0+abc1234")
	require.Contains(t, out, "family_photos")
	require.Contains(t, out, "online")
	require.Contains(t, out, "living_room_tv")
	require.NotContains(t, out, "INSECURE")

	jsonOut, err := run(t, "admin", "diag", "--url", srv.URL, "--token", adminToken, "--json")
	require.NoError(t, err, jsonOut)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &parsed))
	require.Equal(t, "0.1.0+abc1234", parsed["version"])
}

func TestAdminDiagReportsInsecureMode(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/diagnostics": `{"version":"0.1.0","uptime_seconds":1,"insecure":true,
			"db":{"size_bytes":0,"migrations":1},"sources":[],"screens":[],"widgets":{},"pairings":{}}`,
	})
	out, err := run(t, "admin", "diag", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "INSECURE (tls off)")
	require.Contains(t, out, "(none)")
}

func TestAdminPairListApproveAndReject(t *testing.T) {
	pairings := `{"pairings":[{"id":"01JPAIRING","code":"123456","status":"pending",
		"created_at":"2026-09-05T12:00:00Z","expires_at":"2026-09-05T12:05:00Z"}]}`
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/pairings": pairings,
		"POST /api/v1/pairings/01JPAIRING/approve": `{"pairing":{"id":"01JPAIRING","code":"123456","status":"approved"},
			"screen":{"id":"living_room_tv","name":"Living room","status":"active","registered":true}}`,
		"DELETE /api/v1/pairings/01JPAIRING": "",
	})

	list, err := run(t, "admin", "pair", "list", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, list)
	require.Contains(t, list, "123456")
	require.Contains(t, list, "pending")

	// The operator types the code the TV shows; the CLI resolves it to the ID.
	approve, err := run(t, "admin", "pair", "approve", "123456",
		"--id", "living_room_tv", "--name", "Living room", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, approve)
	require.Contains(t, approve, "approved pairing 123456")
	require.Contains(t, approve, "living_room_tv")

	reject, err := run(t, "admin", "pair", "reject", "01JPAIRING", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, reject)
	require.Contains(t, reject, "rejected pairing")
}

func TestAdminPairApproveRequiresID(t *testing.T) {
	srv := fakeServer(t, map[string]string{})
	_, err := run(t, "admin", "pair", "approve", "123456", "--url", srv.URL, "--token", adminToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id is required")
}

func TestAdminPairApproveUnknownCode(t *testing.T) {
	srv := fakeServer(t, map[string]string{"GET /api/v1/pairings": `{"pairings":[]}`})
	_, err := run(t, "admin", "pair", "approve", "999999", "--id", "tv",
		"--url", srv.URL, "--token", adminToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pairing with code 999999")
}

func TestAdminScreensListGetRevoke(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/screens": `{"screens":[{"id":"living_room_tv","name":"Living room","status":"active",
			"registered":true,"online":true,"approved_at":"2026-09-05T12:00:00Z",
			"last_seen_at":"2026-09-05T12:00:30Z","last_sequence":3,"applied_sequence":3}]}`,
		"GET /api/v1/screens/living_room_tv": `{"id":"living_room_tv","name":"Living room","status":"active",
			"registered":true,"online":false,"approved_at":"2026-09-05T12:00:00Z","last_sequence":3,"applied_sequence":2}`,
		"DELETE /api/v1/screens/living_room_tv": `{"id":"living_room_tv","name":"Living room","status":"revoked",
			"registered":false,"online":false,"approved_at":"2026-09-05T12:00:00Z"}`,
	})

	list, err := run(t, "admin", "screens", "list", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, list)
	require.Contains(t, list, "living_room_tv")
	require.Contains(t, list, "yes")

	get, err := run(t, "admin", "screens", "get", "living_room_tv", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, get)
	require.Contains(t, get, "online       no")
	require.Contains(t, get, "issued 3, applied 2")

	revoke, err := run(t, "admin", "screens", "revoke", "living_room_tv", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, revoke)
	require.Contains(t, revoke, "revoked living_room_tv")

	jsonOut, err := run(t, "admin", "screens", "list", "--url", srv.URL, "--token", adminToken, "--json")
	require.NoError(t, err, jsonOut)
	var parsed struct {
		Screens []map[string]any `json:"screens"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &parsed))
	require.Len(t, parsed.Screens, 1)
}

func TestAdminEmptyListingsRenderAPlaceholder(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/screens":  `{"screens":[]}`,
		"GET /api/v1/pairings": `{"pairings":[]}`,
	})
	screens, err := run(t, "admin", "screens", "list", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err)
	require.Contains(t, screens, "no screens paired")

	pairings, err := run(t, "admin", "pair", "list", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err)
	require.Contains(t, pairings, "no pairing requests")
}

func TestAdminSurfacesServerErrors(t *testing.T) {
	srv := fakeServer(t, map[string]string{})
	_, err := run(t, "admin", "screens", "get", "missing", "--url", srv.URL, "--token", adminToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
	require.Contains(t, err.Error(), "not_found")
}

func TestAdminRejectsAWrongToken(t *testing.T) {
	srv := fakeServer(t, map[string]string{"GET /api/v1/screens": `{"screens":[]}`})
	_, err := run(t, "admin", "screens", "list", "--url", srv.URL, "--token", "atr_adm_wrong")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestAdminRequiresAToken(t *testing.T) {
	t.Setenv("ATRIUM_ADMIN_TOKEN", "")
	_, err := run(t, "admin", "screens", "list", "--url", "https://127.0.0.1:8443")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no admin token")
}

func TestAdminReadsTokenFromEnvironment(t *testing.T) {
	srv := fakeServer(t, map[string]string{"GET /api/v1/screens": `{"screens":[]}`})
	t.Setenv("ATRIUM_ADMIN_TOKEN", adminToken)
	out, err := run(t, "admin", "screens", "list", "--url", srv.URL)
	require.NoError(t, err, out)
	require.True(t, strings.Contains(out, "no screens paired"))
}
