package config_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/DituLin/Atrium/internal/config"
)

const qweatherConfig = `
home:
  timezone: Asia/Shanghai
widgets:
  weather:
    enabled: true
    provider: qweather
    latitude: 30.2
    longitude: 120.1
    qweather:
      api_host: synthetic.qweatherapi.com
      timezone: Asia/Shanghai
      api_key_file: /private/synthetic-weather-key
`

func TestQWeatherConfigurationParsesAndValidates(t *testing.T) {
	cfg, err := config.Parse([]byte(qweatherConfig))
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
	redacted, err := yaml.Marshal(cfg.Redacted())
	require.NoError(t, err)
	require.NotContains(t, string(redacted), "synthetic.qweatherapi.com")
	require.NotContains(t, string(redacted), "/private/synthetic-weather-key")
}

func TestQWeatherConfigurationRejectsAmbiguousAndUnsafeSettings(t *testing.T) {
	for name, replacement := range map[string][2]string{
		"host URL":          {"synthetic.qweatherapi.com", "https://synthetic.qweatherapi.com"},
		"legacy host":       {"synthetic.qweatherapi.com", "api.qweather.com"},
		"host path":         {"synthetic.qweatherapi.com", "synthetic.qweatherapi.com/path"},
		"timezone":          {"timezone: Asia/Shanghai\n      api_key_file", "timezone: Mars/Unknown\n      api_key_file"},
		"missing auth":      {"api_key_file: /private/synthetic-weather-key", "api_key_file: ''"},
		"both auth methods": {"api_key_file: /private/synthetic-weather-key", "api_key_file: /private/synthetic-weather-key\n      jwt:\n        developer_id: Q123456789\n        project_id: Project123\n        credential_id: Credential123\n        private_key_file: /private/synthetic-weather.pem"},
		"partial jwt":       {"api_key_file: /private/synthetic-weather-key", "jwt:\n        developer_id: Q123456789"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(strings.Replace(qweatherConfig, replacement[0], replacement[1], 1)))
			require.NoError(t, err)
			err = cfg.Validate()
			require.Error(t, err)
			require.NotContains(t, err.Error(), "synthetic.qweatherapi.com")
			require.NotContains(t, err.Error(), "/private/synthetic-weather-key")
		})
	}
}

func TestQWeatherJWTConfigurationAndHomeTimezoneFallback(t *testing.T) {
	body := strings.Replace(qweatherConfig, "      timezone: Asia/Shanghai\n", "", 1)
	body = strings.Replace(body, "api_key_file: /private/synthetic-weather-key", "jwt:\n        developer_id: Q123456789\n        project_id: Project123\n        credential_id: Credential123\n        private_key_file: ~/synthetic-weather.pem", 1)
	cfg, err := config.Parse([]byte(body))
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
	redacted, err := yaml.Marshal(cfg.Redacted())
	require.NoError(t, err)
	for _, value := range []string{"Q123456789", "Project123", "Credential123", "synthetic-weather.pem"} {
		require.NotContains(t, string(redacted), value)
	}
}
