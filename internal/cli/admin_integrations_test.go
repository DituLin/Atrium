package cli_test

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
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
