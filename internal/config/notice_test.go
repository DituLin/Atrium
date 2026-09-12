package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNoticeTimeConfig(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		valid        bool
	}{
		{"legacy", "", true},
		{"offsets", "updated_at: '2026-09-12T10:00:00Z'\n    valid_from: '2026-09-12T18:00:00+08:00'\n    valid_until: '2026-09-12T11:00:00Z'", true},
		{"empty", "updated_at: ''\n    valid_from: null", true},
		{"local", "updated_at: '2026-09-12T10:00:00'", false},
		{"date", "valid_from: '2026-09-12'", false},
		{"equal", "valid_from: '2026-09-12T18:00:00+08:00'\n    valid_until: '2026-09-12T10:00:00Z'", false},
		{"reverse", "valid_from: '2026-09-12T18:00:00+08:00'\n    valid_until: '2026-09-12T09:00:00Z'", false},
		{"bad day", "updated_at: '2026-02-30T10:00:00Z'", false},
		{"fractional", "updated_at: '2026-09-12T10:00:00.123456789Z'", true},
		{"bad offset", "valid_until: '2026-09-12T10:00:00+24:00'", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := writeConfig(t, minimal+"\nwidgets:\n  notice:\n    enabled: true\n    text: hello\n    "+tc.fields+"\n")
			cfg, err := config.Load(p)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, "hello", cfg.Widgets.Notice.Text)
			} else {
				require.Error(t, err)
				require.Nil(t, cfg)
			}
		})
	}
}

func TestSuccessfulLoadObservationOnly(t *testing.T) {
	defaults := config.Defaults()
	require.Nil(t, defaults.LoadedAt())
	parsed, err := config.Parse([]byte("home:\n  timezone: UTC\n"))
	require.NoError(t, err)
	require.NoError(t, parsed.Validate())
	require.Nil(t, parsed.LoadedAt())
	p, _ := writeConfig(t, minimal)
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(p, old, old))
	before := time.Now()
	cfg, err := config.Load(p)
	after := time.Now()
	require.NoError(t, err)
	stamp := cfg.LoadedAt()
	require.NotNil(t, stamp)
	require.False(t, stamp.Before(before))
	require.False(t, stamp.After(after))
	original := *stamp
	*stamp = time.Time{}
	require.Equal(t, original, *cfg.LoadedAt(), "caller cannot change observation metadata")
}

func TestDisabledNoticeStillRejectsInvalidConfiguration(t *testing.T) {
	p, _ := writeConfig(t, minimal+"\nwidgets:\n  notice:\n    enabled: false\n    updated_at: yesterday\n")
	cfg, err := config.Load(p)
	require.ErrorContains(t, err, "widgets.notice.updated_at")
	require.Nil(t, cfg)
}
