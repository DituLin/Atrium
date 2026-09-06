package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/config"
)

const minimal = `
home:
  timezone: "Asia/Singapore"
server:
  public_url: "https://192.168.1.10:8443"
storage:
  data_dir: "%DATA%"
sources:
  - id: family_photos
    root: "%ROOT%"
    identity:
      allow_local: true
`

func writeConfig(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	root := filepath.Join(dir, "photos")
	require.NoError(t, os.MkdirAll(root, 0o755))
	body = strings.ReplaceAll(body, "%DATA%", data)
	body = strings.ReplaceAll(body, "%ROOT%", root)
	p := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p, dir
}

func TestLoadAppliesDefaults(t *testing.T) {
	p, _ := writeConfig(t, minimal)
	cfg, err := config.Load(p)
	require.NoError(t, err)

	require.Equal(t, "Home", cfg.Home.Name)
	require.Equal(t, "0.0.0.0:8443", cfg.Server.Listen)
	require.Equal(t, config.TLSAuto, cfg.Server.TLS.Mode)
	require.Equal(t, int64(10737418240), cfg.Storage.CacheBudgetBytes)
	require.Equal(t, 2, cfg.Media.Workers)
	require.Equal(t, 30*time.Second, cfg.Media.DecodeTimeout.D())
	require.Equal(t, 2560, cfg.Media.PreviewMaxEdge)
	require.Equal(t, "sips", cfg.Media.HEIC.Converter)
	require.Equal(t, 15*time.Second, cfg.Screens.HeartbeatInterval.D())
	require.Equal(t, 45*time.Second, cfg.Screens.OfflineAfter.D())
	require.Equal(t, 10*time.Second, cfg.Screens.CommandTTL.D())
	require.Equal(t, "info", cfg.Logging.Level)
	require.Equal(t, 7, cfg.Backup.Keep)

	require.Len(t, cfg.Sources, 1)
	s := cfg.Sources[0]
	require.Equal(t, []string{"jpg", "jpeg", "png", "heic", "heif"}, s.IncludeExtensions)
	require.Equal(t, 60*time.Second, s.Scan.Interval.D())
	require.Equal(t, 5*time.Second, s.Scan.StabilityInterval.D())
	require.Equal(t, 2, s.Scan.StabilityChecks)
	require.Equal(t, 3, s.Scan.StabilityMaxRound)
	require.Equal(t, 8, s.MaxInflight)
	require.Equal(t, "family_photos", s.Name, "name defaults to id")
	require.Equal(t, p, cfg.Path())
}

func TestExampleConfigParses(t *testing.T) {
	raw, err := os.ReadFile("../../config.example.yaml")
	require.NoError(t, err)
	cfg, err := config.Parse(raw)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}

func TestUnknownKeysRejected(t *testing.T) {
	_, err := config.Parse([]byte("home:\n  timezone: UTC\nnonsense: 1\n"))
	require.Error(t, err)
}

func TestTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	got, err := config.ExpandPath("~/Library/Atrium")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "Library/Atrium"), got)
}

func TestEnvDataDirOverride(t *testing.T) {
	p, dir := writeConfig(t, minimal)
	override := filepath.Join(dir, "override")
	t.Setenv(config.EnvDataDir, override)
	cfg, err := config.Load(p)
	require.NoError(t, err)
	require.Equal(t, override, cfg.Storage.DataDir)
}

func TestEnvConfigPath(t *testing.T) {
	p, _ := writeConfig(t, minimal)
	t.Setenv(config.EnvConfig, p)
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, "Asia/Singapore", cfg.Home.Timezone)
}

func TestValidationRules(t *testing.T) {
	base := func() *config.Config {
		cfg := config.Defaults()
		cfg.Home.Timezone = "UTC"
		cfg.Storage.DataDir = "/tmp/atrium-data"
		src := config.SourceDefaults()
		src.ID = "family_photos"
		src.Root = "/Volumes/photos/family"
		cfg.Sources = []config.Source{src}
		return cfg
	}
	require.NoError(t, base().Validate())

	cases := map[string]func(*config.Config){
		"missing timezone":       func(c *config.Config) { c.Home.Timezone = "" },
		"unknown timezone":       func(c *config.Config) { c.Home.Timezone = "Mars/Olympus" },
		"duplicate source ids":   func(c *config.Config) { c.Sources = append(c.Sources, c.Sources[0]) },
		"root is /":              func(c *config.Config) { c.Sources[0].Root = "/" },
		"relative root":          func(c *config.Config) { c.Sources[0].Root = "relative/path" },
		"bad source id":          func(c *config.Config) { c.Sources[0].ID = "Family Photos" },
		"data dir in source":     func(c *config.Config) { c.Storage.DataDir = "/Volumes/photos/family/data" },
		"backup dir in source":   func(c *config.Config) { c.Backup.Dir = "/Volumes/photos/family/backups" },
		"backup dir in data dir": func(c *config.Config) { c.Backup.Dir = "/tmp/atrium-data/backups" },
		"tls file without files": func(c *config.Config) { c.Server.TLS.Mode = config.TLSFile },
		"bad tls mode":           func(c *config.Config) { c.Server.TLS.Mode = "wat" },
		"bad listen":             func(c *config.Config) { c.Server.Listen = "not-a-host-port" },
		"bad public url":         func(c *config.Config) { c.Server.PublicURL = "://bad" },
		"zero workers":           func(c *config.Config) { c.Media.Workers = 0 },
		"bad heic converter":     func(c *config.Config) { c.Media.HEIC.Converter = "libheif" },
		"offline before beat":    func(c *config.Config) { c.Screens.OfflineAfter = c.Screens.HeartbeatInterval },
		"bad log level":          func(c *config.Config) { c.Logging.Level = "trace" },
		"backup without dir":     func(c *config.Config) { c.Backup.Enabled = true; c.Backup.Dir = "" },
		"bad backup time": func(c *config.Config) {
			c.Backup.Enabled = true
			c.Backup.Dir = "/tmp/backups"
			c.Backup.Time = "25:00"
		},
		"weather out of range": func(c *config.Config) {
			c.Widgets.Weather.Enabled = true
			c.Widgets.Weather.Latitude = 120
		},
		"no extensions": func(c *config.Config) { c.Sources[0].IncludeExtensions = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := base()
			mutate(cfg)
			require.Error(t, cfg.Validate(), "expected %s to be rejected", name)
		})
	}
}

func TestRedactedHidesRoots(t *testing.T) {
	cfg := config.Defaults()
	src := config.SourceDefaults()
	src.ID = "family_photos"
	src.Root = "/Volumes/photos/family"
	src.Identity.MarkerFile = ".atrium-marker"
	cfg.Sources = []config.Source{src}

	r := cfg.Redacted()
	require.Equal(t, "<redacted>", r.Sources[0].Root)
	require.Equal(t, "<redacted>", r.Sources[0].Identity.MarkerFile)
	require.Equal(t, "/Volumes/photos/family", cfg.Sources[0].Root, "original untouched")
}

func TestDefaultAllowedOrigins(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.PublicURL = "https://192.168.1.10:8443"
	got := cfg.DefaultAllowedOrigins()
	require.Equal(t, []string{
		"https://192.168.1.10:8443",
		"https://localhost:8443",
		"https://127.0.0.1:8443",
	}, got)

	cfg.Server.TLS.Mode = config.TLSOff
	cfg.Server.PublicURL = "http://127.0.0.1:8080"
	cfg.Server.Listen = "127.0.0.1:8080"
	require.Equal(t, []string{"http://127.0.0.1:8080", "http://localhost:8080"}, cfg.DefaultAllowedOrigins())

	cfg.Server.AllowedOrigins = []string{"https://tv.local"}
	require.Equal(t, []string{"https://tv.local"}, cfg.DefaultAllowedOrigins())
}

func TestParseDayTime(t *testing.T) {
	h, m, err := config.ParseDayTime("03:05")
	require.NoError(t, err)
	require.Equal(t, 3, h)
	require.Equal(t, 5, m)
	for _, bad := range []string{"", "3", "24:00", "03:60", "aa:bb"} {
		_, _, err := config.ParseDayTime(bad)
		require.Error(t, err, bad)
	}
}
