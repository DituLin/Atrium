package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// Validate enforces every rule from technical design §10.
func (c *Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.Home.Timezone == "" {
		add("home.timezone is required")
	} else if _, err := time.LoadLocation(c.Home.Timezone); err != nil {
		add("home.timezone %q is not a known IANA timezone", c.Home.Timezone)
	}

	if c.Server.Listen == "" {
		add("server.listen is required")
	} else if _, _, err := net.SplitHostPort(c.Server.Listen); err != nil {
		add("server.listen %q is not host:port", c.Server.Listen)
	}
	if c.Server.PublicURL != "" {
		u, err := url.Parse(c.Server.PublicURL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			add("server.public_url %q must be an absolute http(s) URL", c.Server.PublicURL)
		}
	}
	switch c.Server.TLS.Mode {
	case TLSAuto, TLSOff:
	case TLSFile:
		if c.Server.TLS.CertFile == "" || c.Server.TLS.KeyFile == "" {
			add("server.tls.mode=file requires cert_file and key_file")
		}
	default:
		add("server.tls.mode %q must be auto, file or off", c.Server.TLS.Mode)
	}

	if c.Storage.DataDir == "" {
		add("storage.data_dir is required")
	}
	if c.Storage.CacheBudgetBytes <= 0 {
		add("storage.cache_budget_bytes must be positive")
	}
	if c.Storage.MinFreeBytes < 0 {
		add("storage.min_free_bytes must not be negative")
	}

	seen := make(map[string]struct{}, len(c.Sources))
	for i, s := range c.Sources {
		prefix := fmt.Sprintf("sources[%d]", i)
		if !domain.ValidSlug(s.ID) {
			add("%s.id %q must match [a-z0-9_]+", prefix, s.ID)
		}
		if _, dup := seen[s.ID]; dup {
			add("%s.id %q is duplicated", prefix, s.ID)
		}
		seen[s.ID] = struct{}{}
		switch {
		case s.Root == "":
			add("%s.root is required", prefix)
		case s.Root == "/":
			add("%s.root must not be the filesystem root", prefix)
		case !filepath.IsAbs(s.Root):
			add("%s.root %q must be absolute", prefix, s.Root)
		}
		if len(s.IncludeExtensions) == 0 {
			add("%s.include_extensions must not be empty", prefix)
		}
		if s.Scan.Interval <= 0 {
			add("%s.scan.interval must be positive", prefix)
		}
		if s.Scan.StabilityChecks < 1 {
			add("%s.scan.stability_checks must be at least 1", prefix)
		}
		if s.Scan.StabilityMaxRound < 1 {
			add("%s.scan.stability_max_rounds must be at least 1", prefix)
		}
		if s.IOTimeout <= 0 {
			add("%s.io_timeout must be positive", prefix)
		}
		if s.MaxInflight < 1 {
			add("%s.max_inflight must be at least 1", prefix)
		}
		if isWithin(c.Storage.DataDir, s.Root) {
			add("storage.data_dir must not be inside source root %q", s.Root)
		}
		if isWithin(s.Root, c.Storage.DataDir) {
			add("%s.root must not be inside storage.data_dir", prefix)
		}
	}

	if c.Media.Workers < 1 {
		add("media.workers must be at least 1")
	}
	if c.Media.DecodeTimeout <= 0 {
		add("media.decode_timeout must be positive")
	}
	if c.Media.PreviewMaxEdge < 64 {
		add("media.preview_max_edge must be at least 64")
	}
	if c.Media.ThumbMaxEdge < 32 {
		add("media.thumb_max_edge must be at least 32")
	}
	if c.Media.PreviewMaxBytes <= 0 {
		add("media.preview_max_bytes must be positive")
	}
	if c.Media.MaxPixels <= 0 {
		add("media.max_pixels must be positive")
	}
	if c.Media.MaxSourceBytes <= 0 {
		add("media.max_source_bytes must be positive")
	}
	switch c.Media.HEIC.Converter {
	case "sips", "off":
	default:
		add("media.heic.converter %q must be sips or off", c.Media.HEIC.Converter)
	}

	if c.Screens.HeartbeatInterval <= 0 {
		add("screens.heartbeat_interval must be positive")
	}
	if c.Screens.OfflineAfter <= c.Screens.HeartbeatInterval {
		add("screens.offline_after must be greater than heartbeat_interval")
	}
	if c.Screens.CommandTTL <= 0 {
		add("screens.command_ttl must be positive")
	}
	if c.Screens.SlideshowInterval <= 0 {
		add("screens.slideshow_interval must be positive")
	}

	if c.Widgets.Weather.Enabled {
		if c.Widgets.Weather.Provider != "open-meteo" {
			add("widgets.weather.provider %q is not supported", c.Widgets.Weather.Provider)
		}
		if c.Widgets.Weather.Latitude < -90 || c.Widgets.Weather.Latitude > 90 {
			add("widgets.weather.latitude out of range")
		}
		if c.Widgets.Weather.Longitude < -180 || c.Widgets.Weather.Longitude > 180 {
			add("widgets.weather.longitude out of range")
		}
		if c.Widgets.Weather.RefreshInterval <= 0 {
			add("widgets.weather.refresh_interval must be positive")
		}
		if c.Widgets.Weather.StaleAfter <= 0 {
			add("widgets.weather.stale_after must be positive")
		}
	}

	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		add("logging.level %q must be debug, info, warn or error", c.Logging.Level)
	}
	if c.Logging.RetainDays < 1 {
		add("logging.retain_days must be at least 1")
	}
	if c.Logging.MaxTotalMB < 1 {
		add("logging.max_total_mb must be at least 1")
	}

	if c.Backup.Enabled {
		if c.Backup.Dir == "" {
			add("backup.dir is required when backup is enabled")
		}
		if c.Backup.Keep < 1 {
			add("backup.keep must be at least 1")
		}
		if _, _, err := ParseDayTime(c.Backup.Time); err != nil {
			add("backup.time %q must be HH:MM", c.Backup.Time)
		}
	}
	if c.Backup.Dir != "" {
		if isWithin(c.Backup.Dir, c.Storage.DataDir) {
			add("backup.dir must not be inside storage.data_dir")
		}
		for _, s := range c.Sources {
			if isWithin(c.Backup.Dir, s.Root) {
				add("backup.dir must not be inside source root %q", s.Root)
			}
		}
	}

	return errors.Join(errs...)
}

// ParseDayTime parses an "HH:MM" local wall time into hour and minute.
func ParseDayTime(s string) (hour, minute int, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour in %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute in %q", s)
	}
	return h, m, nil
}

// isWithin reports whether child is equal to or nested under parent.
func isWithin(child, parent string) bool {
	if child == "" || parent == "" {
		return false
	}
	c := filepath.Clean(child)
	p := filepath.Clean(parent)
	if c == p {
		return true
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// IsWithin reports whether child is equal to or nested under parent.
func IsWithin(child, parent string) bool { return isWithin(child, parent) }
