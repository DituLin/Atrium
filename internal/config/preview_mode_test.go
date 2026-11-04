package config_test

import (
	"strings"
	"testing"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPreviewModeDefaultsToEagerAndAcceptsOnDemand(t *testing.T) {
	raw, err := yaml.Marshal(config.Defaults().Media)
	require.NoError(t, err)
	require.Contains(t, string(raw), "preview_mode: eager")
	for _, mode := range []string{"eager", "on_demand"} {
		p, _ := writeConfig(t, minimal+"\nmedia:\n  preview_mode: "+mode+"\n")
		cfg, err := config.Load(p)
		require.NoError(t, err)
		raw, err := yaml.Marshal(cfg.Media)
		require.NoError(t, err)
		require.Contains(t, string(raw), "preview_mode: "+mode)
	}
}

func TestPreviewModeRejectsUnknownValues(t *testing.T) {
	for _, mode := range []string{"", "off", "ON_DEMAND"} {
		p, _ := writeConfig(t, minimal+"\nmedia:\n  preview_mode: \""+mode+"\"\n")
		_, err := config.Load(p)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "media.preview_mode"), err.Error())
	}
}
