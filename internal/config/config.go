// Package config loads, validates and redacts the Atrium YAML configuration.
package config

import (
	"time"
)

// Config is the whole configuration file (technical design §10).
type Config struct {
	Home    Home     `yaml:"home"`
	Server  Server   `yaml:"server"`
	Storage Storage  `yaml:"storage"`
	Sources []Source `yaml:"sources"`
	Media   Media    `yaml:"media"`
	Screens Screens  `yaml:"screens"`
	Widgets Widgets  `yaml:"widgets"`
	Logging Logging  `yaml:"logging"`
	Backup  Backup   `yaml:"backup"`

	// path is the file the config was loaded from; empty for defaults.
	path string `yaml:"-"`
}

// Path returns the file the configuration was loaded from.
func (c *Config) Path() string { return c.path }

// Home describes the household context.
type Home struct {
	Name     string `yaml:"name"`
	Timezone string `yaml:"timezone"`
}

// Server holds listener, origin and TLS settings.
type Server struct {
	Listen         string   `yaml:"listen"`
	PublicURL      string   `yaml:"public_url"`
	ExtraSANs      []string `yaml:"extra_sans"`
	AllowedOrigins []string `yaml:"allowed_origins"`
	TLS            TLS      `yaml:"tls"`
}

// TLSMode selects how the server obtains its certificate.
type TLSMode string

// TLS modes.
const (
	TLSAuto TLSMode = "auto"
	TLSFile TLSMode = "file"
	TLSOff  TLSMode = "off"
)

// TLS configures transport security.
type TLS struct {
	Mode     TLSMode `yaml:"mode"`
	CertFile string  `yaml:"cert_file"`
	KeyFile  string  `yaml:"key_file"`
}

// Storage configures the data directory and the preview cache budget.
type Storage struct {
	DataDir          string `yaml:"data_dir"`
	CacheBudgetBytes int64  `yaml:"cache_budget_bytes"`
	MinFreeBytes     int64  `yaml:"min_free_bytes"`
}

// Source is one authorized read-only photo root.
type Source struct {
	ID                string   `yaml:"id"`
	Name              string   `yaml:"name"`
	Root              string   `yaml:"root"`
	IncludeExtensions []string `yaml:"include_extensions"`
	Identity          Identity `yaml:"identity"`
	Scan              Scan     `yaml:"scan"`
	IOTimeout         Duration `yaml:"io_timeout"`
	MaxInflight       int      `yaml:"max_inflight"`
}

// Identity constrains what counts as the expected mount.
type Identity struct {
	RequireMount bool   `yaml:"require_mount"`
	AllowLocal   bool   `yaml:"allow_local"`
	MarkerFile   string `yaml:"marker_file"`
}

// Scan configures the indexer cadence and stability rules.
type Scan struct {
	Interval          Duration `yaml:"interval"`
	StabilityInterval Duration `yaml:"stability_interval"`
	StabilityChecks   int      `yaml:"stability_checks"`
	StabilityMaxRound int      `yaml:"stability_max_rounds"`
}

// Media configures decoding and preview generation.
type Media struct {
	Workers         int      `yaml:"workers"`
	DecodeTimeout   Duration `yaml:"decode_timeout"`
	PreviewMaxEdge  int      `yaml:"preview_max_edge"`
	PreviewMaxBytes int64    `yaml:"preview_max_bytes"`
	ThumbMaxEdge    int      `yaml:"thumb_max_edge"`
	MaxPixels       int64    `yaml:"max_pixels"`
	MaxSourceBytes  int64    `yaml:"max_source_bytes"`
	HEIC            HEIC     `yaml:"heic"`
}

// HEIC selects the HEIC conversion strategy.
type HEIC struct {
	Converter string `yaml:"converter"` // sips | off
}

// Screens configures presence and command semantics.
type Screens struct {
	HeartbeatInterval Duration `yaml:"heartbeat_interval"`
	OfflineAfter      Duration `yaml:"offline_after"`
	CommandTTL        Duration `yaml:"command_ttl"`
	SlideshowInterval Duration `yaml:"slideshow_interval"`
}

// Widgets holds optional dashboard widgets.
type Widgets struct {
	Weather Weather `yaml:"weather"`
	Notice  Notice  `yaml:"notice"`
}

// Weather configures the optional weather provider.
type Weather struct {
	Enabled         bool     `yaml:"enabled"`
	Provider        string   `yaml:"provider"`
	Latitude        float64  `yaml:"latitude"`
	Longitude       float64  `yaml:"longitude"`
	LocationLabel   string   `yaml:"location_label"`
	RefreshInterval Duration `yaml:"refresh_interval"`
	StaleAfter      Duration `yaml:"stale_after"`
	// BaseURL overrides the provider endpoint; used by tests only.
	BaseURL string `yaml:"base_url"`
}

// Notice is a static operator message.
type Notice struct {
	Enabled bool   `yaml:"enabled"`
	Text    string `yaml:"text"`
}

// Logging configures the JSON log files.
type Logging struct {
	Level      string `yaml:"level"`
	RetainDays int    `yaml:"retain_days"`
	MaxTotalMB int    `yaml:"max_total_mb"`
}

// Backup configures the daily SQLite snapshot.
type Backup struct {
	Enabled bool   `yaml:"enabled"`
	Dir     string `yaml:"dir"`
	Time    string `yaml:"time"`
	Keep    int    `yaml:"keep"`
}

// Duration is a YAML-friendly time.Duration written as "30s", "2h".
type Duration time.Duration

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		*d = 0
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML renders the duration as a string.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// D converts to a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }
