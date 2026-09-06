package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminSourcesListShowsHealthAndCountsWithoutPaths(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/sources": `{"sources":[{"id":"family_photos","name":"Family photos",
			"status":"active","health":"online","identity_bound":true,"identity_fstype":"smbfs",
			"identity_matches":true,"scan_generation":7,"last_scan_at":"2026-09-05T12:00:00Z",
			"stuck_ops":0,"photos":{"ready":120,"pending":3,"unsupported":1,"removed":2,"excluded":0}}]}`,
	})
	out, err := run(t, "admin", "sources", "list", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "family_photos")
	require.Contains(t, out, "bound/smbfs")
	require.Contains(t, out, "120")
	require.NotContains(t, out, "/Volumes")
}

func TestAdminSourcesListFlagsIdentityMismatch(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/sources": `{"sources":[{"id":"family_photos","status":"active","health":"unknown",
			"health_detail":"identity_mismatch","identity_bound":true,"identity_matches":false,
			"photos":{}}]}`,
	})
	out, err := run(t, "admin", "sources", "list", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "MISMATCH")
	require.Contains(t, out, "identity_mismatch")
}

func TestAdminSourcesScanFullAndMerge(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"POST /api/v1/sources/family_photos/scan": `{"merged":true,"scan_run":{"id":"01JSCAN",
			"source_id":"family_photos","mode":"full","status":"running","started_at":"2026-09-05T12:00:00Z",
			"files_seen":10,"files_new":2}}`,
	})
	out, err := run(t, "admin", "sources", "scan", "family_photos", "--full", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "merged into it")
	require.Contains(t, out, "01JSCAN")
	require.Contains(t, out, "seen 10, new 2")
}

func TestAdminSourceRevokeRestoreAndRebind(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"POST /api/v1/sources/family_photos/revoke":  `{"id":"family_photos","status":"revoked","health":"online"}`,
		"POST /api/v1/sources/family_photos/restore": `{"id":"family_photos","status":"active","health":"online"}`,
		"POST /api/v1/sources/family_photos/rebind-identity": `{"id":"family_photos","status":"active",
			"health":"online","identity_bound":true}`,
	})
	for _, action := range []string{"revoke", "restore", "rebind-identity"} {
		out, err := run(t, "admin", "sources", action, "family_photos", "--url", srv.URL, "--token", adminToken)
		require.NoError(t, err, out)
		require.Contains(t, out, "family_photos")
	}
}

func TestAdminScansListAndGet(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/scans": `{"scans":[{"id":"01JSCAN","source_id":"family_photos","mode":"scheduled",
			"status":"completed","started_at":"2026-09-05T12:00:00Z","files_seen":42,"files_new":1}]}`,
		"GET /api/v1/scans/01JSCAN": `{"id":"01JSCAN","source_id":"family_photos","mode":"scheduled",
			"status":"completed","started_at":"2026-09-05T12:00:00Z","finished_at":"2026-09-05T12:00:03Z",
			"duration_ms":3000,"files_seen":42,"files_new":1,"files_removed":0,"errors":0}`,
	})
	out, err := run(t, "admin", "scans", "list", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "01JSCAN")
	require.Contains(t, out, "completed")

	out, err = run(t, "admin", "scans", "get", "01JSCAN", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "3000 ms")
	require.Contains(t, out, "seen 42")
}

func TestAdminPhotosGetShowsPathAndErrors(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/photos/01JPHOTO/admin": `{"id":"01JPHOTO","source_id":"family_photos",
			"rel_path":"album/holiday.jpg","ext":"jpg","size_bytes":2048,"status":"unsupported",
			"captured_at":null,"captured_confidence":"unknown","first_seen_at":"2026-09-05T12:00:00Z",
			"is_baseline":true,"width":4032,"height":3024,"meta_status":"ready","meta_error":"no_metadata",
			"preview":{"status":"failed"},"preview_error":"decode_failed","preview_attempts":5,
			"missing_generations":0}`,
	})
	out, err := run(t, "admin", "photos", "get", "01JPHOTO", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "album/holiday.jpg")
	require.Contains(t, out, "decode_failed")
	require.Contains(t, out, "baseline import")
	require.Contains(t, out, "4032x3024")
}

func TestAdminPhotosListReportsUnknownCapturedCount(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/photos": `{"items":[{"id":"01JPHOTO","source_id":"family_photos",
			"captured_at":"2026-09-05T10:12:00+08:00","captured_confidence":"exact",
			"first_seen_at":"2026-09-05T12:00:00Z","preview":{"status":"ready"}}],
			"next_cursor":null,"meta":{"collection":"captured_today","unknown_captured_count":4}}`,
	})
	out, err := run(t, "admin", "photos", "list", "--collection", "captured_today",
		"--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "01JPHOTO")
	require.Contains(t, out, "4 photos have no known capture time")
}

func TestAdminPhotosListBaselineOnlyEmptyState(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/photos": `{"items":[],"next_cursor":null,
			"meta":{"collection":"recent","unknown_captured_count":0,"baseline_only":true}}`,
	})
	out, err := run(t, "admin", "photos", "list", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "collection is empty")
	require.Contains(t, out, "baseline only")
}

func TestAdminPhotosExcludeIncludeRetry(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"POST /api/v1/photos/01JPHOTO/exclude": "",
		"POST /api/v1/photos/01JPHOTO/include": "",
		"POST /api/v1/photos/01JPHOTO/retry":   "",
	})
	out, err := run(t, "admin", "photos", "exclude", "01JPHOTO", "--reason", "private",
		"--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "returns 404")

	out, err = run(t, "admin", "photos", "include", "01JPHOTO", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "included 01JPHOTO")

	out, err = run(t, "admin", "photos", "retry", "01JPHOTO", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "re-queued 01JPHOTO")
}

func TestAdminExclusionsListAddRemove(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/exclusions": `{"exclusions":[{"id":"01JRULE","source_id":"family_photos",
			"match_kind":"prefix","pattern":"private","reason":"operator","created_at":"2026-09-05T12:00:00Z"}]}`,
		"POST /api/v1/exclusions": `{"id":"01JRULE","source_id":"family_photos","match_kind":"prefix",
			"pattern":"private","created_at":"2026-09-05T12:00:00Z"}`,
		"DELETE /api/v1/exclusions/01JRULE": "",
	})
	out, err := run(t, "admin", "exclusions", "list", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "01JRULE")
	require.Contains(t, out, "private")

	out, err = run(t, "admin", "exclusions", "add", "--source", "family_photos", "--prefix", "private/",
		"--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "stay hidden after a full rescan")

	out, err = run(t, "admin", "exclusions", "remove", "01JRULE", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "removed 01JRULE")
}

func TestAdminExclusionsAddRequiresArguments(t *testing.T) {
	srv := fakeServer(t, map[string]string{})
	_, err := run(t, "admin", "exclusions", "add", "--url", srv.URL, "--token", adminToken)
	require.Error(t, err)
	_, err = run(t, "admin", "exclusions", "add", "--source", "family_photos", "--url", srv.URL, "--token", adminToken)
	require.Error(t, err)
}

func TestAdminSourcesListJSONOutput(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"GET /api/v1/sources": `{"sources":[{"id":"family_photos","status":"active","health":"online","photos":{}}]}`,
	})
	out, err := run(t, "admin", "sources", "list", "--json", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	require.Len(t, parsed["sources"], 1)
}
