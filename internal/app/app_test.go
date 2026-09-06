package app_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/app"
	"github.com/DituLin/Atritum/internal/auth/tlsgen"
	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/logging"
	"github.com/DituLin/Atritum/internal/store"
)

func testConfig(t *testing.T, mutate ...func(*config.Config)) *config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.Home.Timezone = "Asia/Singapore"
	cfg.Storage.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.Server.Listen = "127.0.0.1:0"
	cfg.Server.PublicURL = "https://127.0.0.1:8443"
	for _, m := range mutate {
		m(cfg)
	}
	require.NoError(t, cfg.Validate())
	return cfg
}

func TestLayoutCreatesTreeWith0700(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	l := app.NewLayout(root)
	require.NoError(t, l.Ensure())

	for _, dir := range []string{l.Root, l.Cache(), l.Logs(), l.TLS(), l.Backups()} {
		st, err := os.Stat(dir)
		require.NoError(t, err, dir)
		require.True(t, st.IsDir())
		require.Equal(t, os.FileMode(0o700), st.Mode().Perm(), "%s must not be readable by others", dir)
	}
	require.NoError(t, l.Ensure(), "Ensure is idempotent")
}

func TestRuntimeStartServeAndShutdown(t *testing.T) {
	cfg := testConfig(t, func(c *config.Config) { c.Server.TLS.Mode = config.TLSOff })
	rt, err := app.New(context.Background(), app.Options{Config: cfg, Logger: logging.Discard()})
	require.NoError(t, err)

	require.NoError(t, rt.Start(context.Background()))
	addr := rt.Addr()
	require.NotEmpty(t, addr)

	resp, err := http.Get("http://" + addr + "/health/live") //nolint:noctx // short-lived test request
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	ready, err := http.Get("http://" + addr + "/health/ready") //nolint:noctx // short-lived test request
	require.NoError(t, err)
	defer func() { _ = ready.Body.Close() }()
	require.Equal(t, http.StatusOK, ready.StatusCode)

	start := time.Now()
	require.NoError(t, rt.Shutdown())
	require.Less(t, time.Since(start), app.ShutdownTimeout, "shutdown must finish inside its budget")

	_, err = http.Get("http://" + addr + "/health/live") //nolint:noctx // short-lived test request
	require.Error(t, err, "the listener is closed after shutdown")
}

func TestRuntimeServesTLSWithGeneratedCertificate(t *testing.T) {
	cfg := testConfig(t)
	rt, err := app.New(context.Background(), app.Options{Config: cfg, Logger: logging.Discard()})
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	t.Cleanup(func() { _ = rt.Shutdown() })

	caPEM, err := tlsgen.ExportCA(rt.Layout().TLS())
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM))

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	_, port, err := net.SplitHostPort(rt.Addr())
	require.NoError(t, err)

	resp, err := client.Get("https://127.0.0.1:" + port + "/health/live") //nolint:noctx // short-lived test request
	require.NoError(t, err, "the generated certificate must be valid for 127.0.0.1")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Equal(t, "ok", body["status"])
}

func TestStartupMarksAcceptedCommandsUnknown(t *testing.T) {
	cfg := testConfig(t, func(c *config.Config) { c.Server.TLS.Mode = config.TLSOff })
	ctx := context.Background()
	now := time.Now()

	// First boot: register a screen and leave a command open.
	rt, err := app.New(ctx, app.Options{Config: cfg, Logger: logging.Discard()})
	require.NoError(t, err)
	db := rt.DB()
	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{
		ID: "living_room_tv", Name: "Living room", TokenHash: "hash", CreatedAt: now, ApprovedAt: now,
	}))
	cmd := &domain.Command{
		ScreenID: "living_room_tv", Kind: domain.CommandNavigate,
		Payload:  domain.CommandPayload{Route: domain.RouteDashboard},
		IssuedBy: "admin", IssuedAt: now, ExpiresAt: now.Add(10 * time.Second),
		Status: domain.CommandAccepted,
	}
	require.NoError(t, db.Commands().Issue(ctx, cmd))
	require.NoError(t, rt.Shutdown())

	// Second boot: the open command becomes unknown/server_restart (design §4.2).
	rt2, err := app.New(ctx, app.Options{Config: cfg, Logger: logging.Discard()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt2.Shutdown() })

	got, err := rt2.DB().Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	require.Equal(t, domain.CommandUnknown, got.Status)
	require.Equal(t, "server_restart", got.ErrorCode)
}

func TestStartupReconcilesSourcesAndTimezone(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	ctx := context.Background()

	withSource := testConfig(t, func(c *config.Config) {
		c.Storage.DataDir = dataDir
		c.Server.TLS.Mode = config.TLSOff
		src := config.SourceDefaults()
		src.ID = "family_photos"
		src.Name = "Family photos"
		src.Root = "/Volumes/photos/family"
		c.Sources = []config.Source{src}
	})
	rt, err := app.New(ctx, app.Options{Config: withSource, Logger: logging.Discard()})
	require.NoError(t, err)
	src, err := rt.DB().Sources().Get(ctx, "family_photos")
	require.NoError(t, err)
	require.Equal(t, domain.SourceActive, src.Status)

	tz, err := rt.DB().Settings().Get(ctx, store.SettingHomeTimezone)
	require.NoError(t, err)
	require.Equal(t, "Asia/Singapore", tz)
	require.NoError(t, rt.Shutdown())

	// The source disappears from the config and the timezone changes.
	without := testConfig(t, func(c *config.Config) {
		c.Storage.DataDir = dataDir
		c.Server.TLS.Mode = config.TLSOff
		c.Home.Timezone = "Europe/Berlin"
	})
	rt2, err := app.New(ctx, app.Options{Config: without, Logger: logging.Discard()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt2.Shutdown() })

	src, err = rt2.DB().Sources().Get(ctx, "family_photos")
	require.NoError(t, err)
	require.Equal(t, domain.SourceRevoked, src.Status, "a source is revoked, never deleted")
	require.Equal(t, "removed_from_config", src.RevokeReason)

	tz, err = rt2.DB().Settings().Get(ctx, store.SettingHomeTimezone)
	require.NoError(t, err)
	require.Equal(t, "Europe/Berlin", tz)

	counts, err := rt2.DB().Jobs().Counts(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[domain.JobQueued], "a timezone change queues recompute_day")
}

func TestServeStopsOnContextCancel(t *testing.T) {
	cfg := testConfig(t, func(c *config.Config) { c.Server.TLS.Mode = config.TLSOff })
	rt, err := app.New(context.Background(), app.Options{Config: cfg, Logger: logging.Discard()})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rt.Serve(ctx) }()

	require.Eventually(t, func() bool { return rt.Addr() != "" }, 3*time.Second, 10*time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(app.ShutdownTimeout + 2*time.Second):
		t.Fatal("Serve did not return after cancellation")
	}
}
