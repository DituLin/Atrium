package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGrowthProfilesConfigurationValidation(t *testing.T) {
	entry := func(id, birth, sex, measurement string) string {
		return fmt.Sprintf("  - entity_id: %q\n    birth_date: %q\n    sex: %q\n    height_measurement: %q\n", id, birth, sex, measurement)
	}
	valid := entry("synthetic-child", "2000-01-01", "male", "length")
	for _, profile := range []string{valid, entry("SYNTHETIC_02", "2000-02-29", "female", "height"), entry("synthetic-child", "2000-01-01", "male", "")} {
		cfg, err := config.Parse([]byte("home:\n  timezone: UTC\narchives:\n  growth_profiles:\n" + profile))
		require.NoError(t, err)
		require.NoError(t, cfg.Validate())
	}
	for name, profile := range map[string]string{
		"missing identity":      entry("", "2000-01-01", "male", ""),
		"invalid identity":      entry("bad identity", "2000-01-01", "male", ""),
		"oversized identity":    entry(strings.Repeat("a", 129), "2000-01-01", "male", ""),
		"missing birthday":      entry("synthetic-child", "", "male", ""),
		"partial birthday":      entry("synthetic-child", "2000-01", "male", ""),
		"invalid birthday":      entry("synthetic-child", "2001-02-29", "male", ""),
		"unnormalized birthday": entry("synthetic-child", "2000-1-01", "male", ""),
		"old birthday":          entry("synthetic-child", "1899-12-31", "male", ""),
		"future birthday":       entry("synthetic-child", "2200-01-01", "male", ""),
		"missing sex":           entry("synthetic-child", "2000-01-01", "", ""),
		"invalid sex":           entry("synthetic-child", "2000-01-01", "other", ""),
		"invalid measurement":   entry("synthetic-child", "2000-01-01", "male", "standing"),
		"duplicate identity":    valid + valid,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Parse([]byte("home:\n  timezone: UTC\narchives:\n  growth_profiles:\n" + profile))
			require.NoError(t, err)
			require.ErrorContains(t, cfg.Validate(), "archives.growth_profiles")
		})
	}
	var many strings.Builder
	for i := range 17 {
		many.WriteString(entry(fmt.Sprintf("synthetic-%d", i), "2000-01-01", "male", ""))
	}
	cfg, err := config.Parse([]byte("home:\n  timezone: UTC\narchives:\n  growth_profiles:\n" + many.String()))
	require.NoError(t, err)
	require.ErrorContains(t, cfg.Validate(), "archives.growth_profiles")
}

func TestGrowthProfilesAreRemovedFromRedactedConfiguration(t *testing.T) {
	cfg, err := config.Parse([]byte("home:\n  timezone: UTC\narchives:\n  growth_profiles:\n    - entity_id: synthetic-child\n      birth_date: '2000-01-01'\n      sex: female\n"))
	require.NoError(t, err)
	redacted, err := yaml.Marshal(cfg.Redacted())
	require.NoError(t, err)
	require.NotContains(t, string(redacted), "synthetic-child")
	require.NotContains(t, string(redacted), "2000-01-01")
	require.NotContains(t, string(redacted), "growth_profiles")
	original, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.Contains(t, string(original), "synthetic-child", "redaction must not mutate the source configuration")
}
