package config

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Environment variables that override configuration.
const (
	EnvConfig  = "ATRIUM_CONFIG"
	EnvDataDir = "ATRIUM_DATA_DIR"
	// EnvAdminToken is read by the CLI only, never by the server.
	EnvAdminToken = "ATRIUM_ADMIN_TOKEN"
)

// Defaults returns a configuration with every documented default applied.
func Defaults() *Config {
	return &Config{
		Home: Home{Name: "Home", Timezone: ""},
		Server: Server{
			Listen:    "0.0.0.0:8443",
			PublicURL: "",
			TLS:       TLS{Mode: TLSAuto},
		},
		Storage: Storage{
			DataDir:          "~/Library/Application Support/Atrium",
			CacheBudgetBytes: 10737418240,
			MinFreeBytes:     5368709120,
		},
		Media: Media{
			Workers:         2,
			DecodeTimeout:   Duration(30e9),
			PreviewMaxEdge:  2560,
			PreviewMaxBytes: 1048576,
			ThumbMaxEdge:    480,
			MaxPixels:       80000000,
			MaxSourceBytes:  62914560,
			HEIC:            HEIC{Converter: "sips"},
		},
		Screens: Screens{
			HeartbeatInterval: Duration(15e9),
			OfflineAfter:      Duration(45e9),
			CommandTTL:        Duration(10e9),
			SlideshowInterval: Duration(30e9),
		},
		Widgets: Widgets{
			Weather: Weather{
				Provider:        "open-meteo",
				RefreshInterval: Duration(30 * 60e9),
				StaleAfter:      Duration(2 * 3600e9),
			},
		},
		Logging: Logging{Level: "info", RetainDays: 7, MaxTotalMB: 200},
		Backup:  Backup{Time: "03:00", Keep: 7},
	}
}

// SourceDefaults returns the per-source defaults.
func SourceDefaults() Source {
	return Source{
		IncludeExtensions: []string{"jpg", "jpeg", "png", "heic", "heif"},
		Identity:          Identity{RequireMount: true},
		Scan: Scan{
			Interval:          Duration(60e9),
			StabilityInterval: Duration(5e9),
			StabilityChecks:   2,
			StabilityMaxRound: 3,
		},
		IOTimeout:   Duration(20e9),
		MaxInflight: 8,
	}
}

// Load reads path, applies defaults and validates the result.
// An empty path falls back to ATRIUM_CONFIG.
func Load(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv(EnvConfig)
	}
	if path == "" {
		return nil, errors.New("config: no path given and ATRIUM_CONFIG is unset")
	}
	raw, err := os.ReadFile(path) //nolint:gosec // operator-provided path
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	cfg, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	cfg.path = path
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes YAML over the defaults and normalizes it without validating.
func Parse(raw []byte) (*Config, error) {
	cfg := Defaults()
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && err.Error() != "EOF" {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// normalize expands paths, applies per-source defaults and env overrides.
func (c *Config) normalize() error {
	if v := os.Getenv(EnvDataDir); v != "" {
		c.Storage.DataDir = v
	}
	expanded, err := ExpandPath(c.Storage.DataDir)
	if err != nil {
		return err
	}
	c.Storage.DataDir = expanded

	if c.Backup.Dir != "" {
		if c.Backup.Dir, err = ExpandPath(c.Backup.Dir); err != nil {
			return err
		}
	}
	for i := range c.Sources {
		s := &c.Sources[i]
		d := SourceDefaults()
		if len(s.IncludeExtensions) == 0 {
			s.IncludeExtensions = d.IncludeExtensions
		}
		for j, e := range s.IncludeExtensions {
			s.IncludeExtensions[j] = strings.ToLower(strings.TrimPrefix(e, "."))
		}
		if s.Scan.Interval == 0 {
			s.Scan.Interval = d.Scan.Interval
		}
		if s.Scan.StabilityInterval == 0 {
			s.Scan.StabilityInterval = d.Scan.StabilityInterval
		}
		if s.Scan.StabilityChecks == 0 {
			s.Scan.StabilityChecks = d.Scan.StabilityChecks
		}
		if s.Scan.StabilityMaxRound == 0 {
			s.Scan.StabilityMaxRound = d.Scan.StabilityMaxRound
		}
		if s.IOTimeout == 0 {
			s.IOTimeout = d.IOTimeout
		}
		if s.MaxInflight == 0 {
			s.MaxInflight = d.MaxInflight
		}
		if s.Root, err = ExpandPath(s.Root); err != nil {
			return err
		}
		if s.Name == "" {
			s.Name = s.ID
		}
	}
	if c.Server.TLS.CertFile != "" {
		if c.Server.TLS.CertFile, err = ExpandPath(c.Server.TLS.CertFile); err != nil {
			return err
		}
	}
	if c.Server.TLS.KeyFile != "" {
		if c.Server.TLS.KeyFile, err = ExpandPath(c.Server.TLS.KeyFile); err != nil {
			return err
		}
	}
	return nil
}

// ExpandPath resolves a leading "~" and returns an absolute path.
func ExpandPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			u, uerr := user.Current()
			if uerr != nil {
				return "", fmt.Errorf("expand %q: %w", p, err)
			}
			home = u.HomeDir
		}
		p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("absolute path for %q: %w", p, err)
	}
	return filepath.Clean(abs), nil
}
