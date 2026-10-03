package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegrationIssueWritesSecretOnlyToNewFile(t *testing.T) {
	secret := "atr_int_" + testSecret
	srv := fakeServer(t, map[string]string{"POST /api/v1/admin/integrations": `{"principal":{"id":"principal1","label":"home"},"token":"` + secret + `"}`})
	path := filepath.Join(t.TempDir(), "home.token")
	args := []string{"admin", "integrations", "issue", "--label", "home", "--permissions", "home.read", "--expires-at", "2027-01-01T00:00:00Z", "--out", path, "--url", srv.URL, "--token", adminToken, "--json"}
	out, err := run(t, args...)
	require.NoError(t, err, out)
	require.NotContains(t, out, secret)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, secret+"\n", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	_, err = run(t, args...)
	require.Error(t, err, "never overwrite an existing token file")
}

func TestIntegrationIssueRejectsMissingOutputAndSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(target, link))
	for _, extra := range [][]string{nil, {"--out", link}} {
		args := []string{"admin", "integrations", "issue", "--label", "home", "--permissions", "home.read", "--expires-at", "2027-01-01T00:00:00Z"}
		args = append(args, extra...)
		_, err := run(t, args...)
		require.Error(t, err)
	}
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
}

func TestAdminHouseholdShow(t *testing.T) {
	srv := fakeServer(t, map[string]string{"GET /api/v1/admin/household": `{"id":"stable-home"}`})
	out, err := run(t, "admin", "household", "show", "--url", srv.URL, "--token", adminToken)
	require.NoError(t, err, out)
	require.Contains(t, out, "stable-home")
}

func TestArchiveGrantRequiresExplicitCASAndScopes(t *testing.T) {
	srv := fakeServer(t, map[string]string{"POST /api/v1/admin/integrations/principal1/grant-archives": `{"id":"principal1","policy_version":2}`})
	base := []string{"admin", "integrations", "grant-archives", "principal1", "--url", srv.URL, "--token", adminToken}
	for _, args := range [][]string{
		{"--households", "home", "--permissions", "archive.read"},
		{"--expected-policy-version", "1", "--permissions", "archive.read"},
		{"--expected-policy-version", "1", "--households", "home"},
	} {
		_, err := run(t, append(append([]string{}, base...), args...)...)
		require.Error(t, err)
	}
	args := append(base, "--households", "home", "--permissions", "archive.read,archive.ingest", "--expected-policy-version", "1")
	out, err := run(t, args...)
	require.NoError(t, err, out)
	require.Contains(t, out, "principal1")
}

func TestArchiveCLITransmitsExplicitHouseholdsAndGrantCAS(t *testing.T) {
	for _, operation := range []string{"issue", "policy", "grant-archives"} {
		t.Run(operation, func(t *testing.T) {
			type request struct {
				method, path string
				body         map[string]any
			}
			requests := make(chan request, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				requests <- request{r.Method, r.URL.Path, body}
				w.Header().Set("Content-Type", "application/json")
				if operation == "issue" {
					_, _ = w.Write([]byte(`{"principal":{"id":"p"},"token":"atr_int_` + testSecret + `"}`))
				} else {
					_, _ = w.Write([]byte(`{"id":"p","policy_version":2}`))
				}
			}))
			defer srv.Close()
			args := []string{"admin", "integrations", operation}
			if operation != "issue" {
				args = append(args, "p")
			}
			args = append(args, "--permissions", "archive.read,archive.ingest", "--households", "stable-home", "--url", srv.URL, "--token", adminToken)
			if operation == "issue" {
				args = append(args, "--label", "archive", "--expires-at", "2027-01-01T00:00:00Z", "--out", filepath.Join(t.TempDir(), "credential"))
			}
			if operation == "grant-archives" {
				args = append(args, "--expected-policy-version", "7")
			}
			out, err := run(t, args...)
			require.NoError(t, err, out)
			got := <-requests
			policy := got.body
			if operation == "issue" {
				policy = got.body["policy"].(map[string]any)
			}
			require.Equal(t, []any{"stable-home"}, policy["household_ids"])
			require.Equal(t, []any{"archive.read", "archive.ingest"}, policy["permissions"])
			if operation == "grant-archives" {
				require.Equal(t, "POST", got.method)
				require.Equal(t, "/api/v1/admin/integrations/p/grant-archives", got.path)
				require.Equal(t, float64(7), got.body["expected_policy_version"])
				require.Len(t, got.body, 3, "grant must not submit a replacement legacy policy")
			}
			if operation == "policy" {
				require.Equal(t, "PUT", got.method)
				require.Empty(t, policy["screen_ids"])
				require.Empty(t, policy["member_ids"])
			}
		})
	}
}
