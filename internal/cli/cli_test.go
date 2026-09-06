package cli_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/cli"
)

// run executes the command tree with args and returns its combined output.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	root := cli.NewRoot()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return buf.String(), err
}

// fakeServer serves the admin routes the CLI calls.
func fakeServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer atr_adm_"+testSecret {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"no credential"}}`))
			return
		}
		body, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"no route"}}`))
			return
		}
		if body == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testSecret is a syntactically valid 43-character token secret.
const testSecret = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestVersionCommand(t *testing.T) {
	out, err := run(t, "version")
	require.NoError(t, err)
	require.Contains(t, out, "atrium ")

	short, err := run(t, "version", "--short")
	require.NoError(t, err)
	require.NotContains(t, short, "atrium ")
	require.NotEmpty(t, strings.TrimSpace(short))
}

func TestInitCreatesConfigDataDirTLSAndToken(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	dataDir := filepath.Join(dir, "data")

	out, err := run(t, "init", "--config", configPath, "--data-dir", dataDir,
		"--public-url", "https://127.0.0.1:8443", "--timezone", "Asia/Singapore")
	require.NoError(t, err, out)

	require.FileExists(t, configPath)
	require.DirExists(t, filepath.Join(dataDir, "cache"))
	require.DirExists(t, filepath.Join(dataDir, "logs"))
	require.DirExists(t, filepath.Join(dataDir, "tls"))
	require.DirExists(t, filepath.Join(dataDir, "backups"))
	require.FileExists(t, filepath.Join(dataDir, "atrium.db"))
	require.FileExists(t, filepath.Join(dataDir, "tls", "ca.crt"))
	require.FileExists(t, filepath.Join(dataDir, "tls", "server.crt"))

	tokenPath := filepath.Join(dataDir, "admin.token")
	st, err := os.Stat(tokenPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	token, err := os.ReadFile(tokenPath)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(strings.TrimSpace(string(token)), "atr_adm_"))
	require.Contains(t, out, "Admin token")

	// Re-running init keeps everything and does not mint a second token.
	again, err := run(t, "init", "--config", configPath, "--data-dir", dataDir)
	require.NoError(t, err, again)
	require.Contains(t, again, "config exists")
	require.Contains(t, again, "already present")
	tokenAfter, err := os.ReadFile(tokenPath)
	require.NoError(t, err)
	require.Equal(t, string(token), string(tokenAfter))
}

func TestTokenResetIssuesANewCredential(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	dataDir := filepath.Join(dir, "data")
	_, err := run(t, "init", "--config", configPath, "--data-dir", dataDir, "--timezone", "UTC")
	require.NoError(t, err)

	before, err := os.ReadFile(filepath.Join(dataDir, "admin.token"))
	require.NoError(t, err)

	out, err := run(t, "token", "reset", "--config", configPath, "--data-dir", dataDir)
	require.NoError(t, err, out)
	require.Contains(t, out, "revoked")

	after, err := os.ReadFile(filepath.Join(dataDir, "admin.token"))
	require.NoError(t, err)
	require.NotEqual(t, string(before), string(after), "reset must replace the token")
	require.True(t, strings.HasPrefix(strings.TrimSpace(string(after)), "atr_adm_"))
}

func TestTLSExportCAAndRenew(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	dataDir := filepath.Join(dir, "data")
	_, err := run(t, "init", "--config", configPath, "--data-dir", dataDir, "--timezone", "UTC")
	require.NoError(t, err)

	out, err := run(t, "tls", "export-ca", "--config", configPath, "--data-dir", dataDir)
	require.NoError(t, err, out)
	require.Contains(t, out, "BEGIN CERTIFICATE")

	renew, err := run(t, "tls", "renew", "--config", configPath, "--data-dir", dataDir)
	require.NoError(t, err, renew)
	require.Contains(t, renew, "renewed")
}
