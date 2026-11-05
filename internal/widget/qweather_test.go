package widget_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/DituLin/Atrium/internal/widget"
)

func qweatherKey(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "weather-key")
	require.NoError(t, os.WriteFile(p, []byte("synthetic-api-key\n"), 0600))
	return p
}

func qweatherCurrent() string {
	return `{"condition":{"code":"104","text":"阴"},"temperature":{"value":0,"unit":"°C"},"humidity":0,"wind":{"speed":{"value":2.5,"unit":"m/s"},"direction":{"degree":0}},"observationTime":"2026-11-05T12:00:00+08:00","metadata":{"attributions":["https://developer.qweather.com/attribution.html","javascript:alert(1)","https://example.com/weather-credit"]}}`
}

func qweatherProvider(t *testing.T, handler http.HandlerFunc) *widget.QWeather {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &widget.QWeather{Config: config.QWeather{APIHost: "synthetic.qweatherapi.com", Timezone: "Asia/Shanghai", APIKeyFile: qweatherKey(t)}, Client: srv.Client(), BaseURL: srv.URL, Now: func() time.Time { return time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC) }}
}

func TestQWeatherCoordinatesForecastUnitsAndAttribution(t *testing.T) {
	var calls atomic.Int32
	p := qweatherProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "synthetic-api-key", r.Header.Get("X-QW-Api-Key"))
		require.Empty(t, r.Header.Get("Authorization"))
		require.Equal(t, "true", r.URL.Query().Get("localTime"))
		require.Equal(t, "zh", r.URL.Query().Get("lang"))
		require.Empty(t, r.URL.Query().Get("key"))
		switch r.URL.Path {
		case "/weather/v1/current/30.20/120.10":
			_, _ = w.Write([]byte(qweatherCurrent()))
		case "/weather/v1/hourly/30.20/120.10":
			require.Equal(t, "24", r.URL.Query().Get("hours"))
			_, _ = w.Write([]byte(`{"hours":[{"forecastTime":"2026-11-05T13:00+08:00","temperature":{"value":0,"unit":"°C"}},{"forecastTime":"2026-11-05T14:00+08:00","temperature":{"value":33,"unit":"°F"}},{"forecastTime":"2026-11-05T15:00+08:00","temperature":null}]}`))
		case "/weather/v1/daily/30.20/120.10":
			require.Equal(t, "5", r.URL.Query().Get("days"))
			_, _ = w.Write([]byte(`{"days":[{"forecastStartTime":"2026-11-05T00:00+08:00","temperatureMax":{"value":20,"unit":"°C"},"temperatureMin":{"value":0,"unit":"°C"},"daytime":{"condition":{"code":"305"}},"astro":{"sunset":"2026-11-05T17:15+08:00"}}]}`))
		default:
			t.Errorf("unexpected API path")
			w.WriteHeader(404)
		}
	})
	r, err := p.Fetch(context.Background(), 30.2, 120.1)
	require.NoError(t, err)
	require.Equal(t, "qweather", p.Name())
	require.EqualValues(t, 3, calls.Load())
	require.Equal(t, 0.0, r.TemperatureC)
	require.Equal(t, 3, r.ConditionCode)
	require.Equal(t, "阴", r.ConditionText)
	require.NotNil(t, r.HumidityPct)
	require.Equal(t, 0.0, *r.HumidityPct)
	require.Equal(t, 9.0, *r.WindSpeedKMH)
	require.Equal(t, 0.0, *r.WindDirectionDeg)
	require.Equal(t, "Asia/Shanghai", r.ForecastTimezone)
	require.Len(t, r.Hourly, 3)
	require.Equal(t, 0.0, *r.Hourly[0].TemperatureC)
	require.Nil(t, r.Hourly[1].TemperatureC)
	require.Nil(t, r.Hourly[2].TemperatureC)
	require.Len(t, r.Daily, 1)
	require.Equal(t, "2026-11-05", r.Daily[0].Date)
	require.Equal(t, 61, *r.Daily[0].ConditionCode)
	require.Equal(t, "2026-11-05T17:15:00+08:00", r.Sunset)
	wire := weatherJSON(t, r)
	require.Equal(t, "2026-11-05T12:00:00+08:00", wire["observed_at"])
	require.Contains(t, wire["attributions"], "https://example.com/weather-credit")
	require.NotContains(t, wire["attributions"], "javascript:alert(1)")
}

func TestQWeatherRequiredCurrentAndOptionalForecastFailures(t *testing.T) {
	for name, body := range map[string]string{
		"missing temperature":    `{"condition":{"code":"100"}}`,
		"wrong temperature unit": `{"condition":{"code":"100"},"temperature":{"value":20,"unit":"°F"}}`,
		"missing condition":      `{"temperature":{"value":20,"unit":"°C"}}`,
		"null temperature":       `{"condition":{"code":"100"},"temperature":{"value":null,"unit":"°C"}}`,
		"error body":             `{"error":{"status":401,"detail":"synthetic-api-key"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := qweatherProvider(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
			r, err := p.Fetch(context.Background(), 0, 0)
			require.Nil(t, r)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "synthetic-api-key")
		})
	}
	p := qweatherProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/current/") {
			_, _ = w.Write([]byte(qweatherCurrent()))
			return
		}
		w.WriteHeader(503)
	})
	r, err := p.Fetch(context.Background(), 0, 0)
	require.NoError(t, err)
	require.Equal(t, 0.0, r.TemperatureC)
	require.Empty(t, r.Hourly)
	require.Empty(t, r.Daily)
}

func TestQWeatherRejectsUnsafeCredentialFilesWithoutNetwork(t *testing.T) {
	for _, mode := range []string{"public", "symlink", "hardlink", "directory", "multiline", "empty", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			p := qweatherProvider(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = w.Write([]byte(qweatherCurrent())) })
			key := p.Config.APIKeyFile
			switch mode {
			case "public":
				require.NoError(t, os.Chmod(key, 0644))
			case "symlink":
				p.Config.APIKeyFile = filepath.Join(t.TempDir(), "link")
				require.NoError(t, os.Symlink(key, p.Config.APIKeyFile))
			case "hardlink":
				require.NoError(t, os.Link(key, filepath.Join(t.TempDir(), "link")))
			case "directory":
				p.Config.APIKeyFile = t.TempDir()
			case "multiline":
				require.NoError(t, os.WriteFile(key, []byte("first-key\nsecond-key\n"), 0600))
			case "empty":
				require.NoError(t, os.WriteFile(key, nil, 0600))
			case "oversized":
				require.NoError(t, os.WriteFile(key, []byte(strings.Repeat("a", 20000)), 0600))
			}
			_, err := p.Fetch(context.Background(), 0, 0)
			require.Error(t, err)
			require.NotContains(t, err.Error(), key)
			require.Zero(t, calls.Load())
		})
	}
}

func TestQWeatherJWTUsesEd25519AndShortLivedRequiredClaims(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "weather.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600))
	now := time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC)
	p := qweatherProvider(t, func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("X-QW-Api-Key"))
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		require.Len(t, parts, 3)
		decode := func(s string) map[string]any {
			raw, e := base64.RawURLEncoding.DecodeString(s)
			require.NoError(t, e)
			var m map[string]any
			require.NoError(t, json.Unmarshal(raw, &m))
			return m
		}
		header, claims := decode(parts[0]), decode(parts[1])
		require.Equal(t, map[string]any{"alg": "EdDSA", "kid": "Credential123"}, header)
		require.Equal(t, "Q123456789", claims["iss"])
		require.Equal(t, "Project123", claims["sub"])
		require.Len(t, claims, 4)
		require.LessOrEqual(t, int64(claims["iat"].(float64)), now.Unix())
		require.Greater(t, int64(claims["exp"].(float64)), now.Unix())
		require.LessOrEqual(t, int64(claims["exp"].(float64))-now.Unix(), int64(900))
		sig, e := base64.RawURLEncoding.DecodeString(parts[2])
		require.NoError(t, e)
		require.True(t, ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), sig))
		_, _ = w.Write([]byte(qweatherCurrent()))
	})
	p.Config.APIKeyFile = ""
	p.Config.JWT = config.QWeatherJWT{DeveloperID: "Q123456789", ProjectID: "Project123", CredentialID: "Credential123", PrivateKeyFile: path}
	p.Now = func() time.Time { return now }
	_, err = p.Fetch(context.Background(), 0, 0)
	require.NoError(t, err)
}

func TestQWeatherHTTPFailuresBackoffWithoutLeakingResponse(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			now := time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC)
			p := qweatherProvider(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "600")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("synthetic-api-key"))
			})
			p.Now = func() time.Time { return now }
			for range 3 {
				_, err := p.Fetch(context.Background(), 0, 0)
				require.Error(t, err)
				require.NotContains(t, err.Error(), "synthetic-api-key")
				require.NotContains(t, err.Error(), p.BaseURL)
			}
			require.EqualValues(t, 1, calls.Load())
			now = now.Add(time.Hour)
			_, err := p.Fetch(context.Background(), 0, 0)
			require.Error(t, err)
			require.EqualValues(t, 2, calls.Load())
		})
	}
}

func TestQWeatherDoesNotForwardCredentialsOnRedirect(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("X-QW-Api-Key") != "")
		_, _ = w.Write([]byte(qweatherCurrent()))
	}))
	defer target.Close()
	p := qweatherProvider(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	_, err := p.Fetch(context.Background(), 0, 0)
	require.Error(t, err)
	require.False(t, leaked.Load())
}

func TestQWeatherOptionalTimeoutLeavesTimeToPersistValidCurrent(t *testing.T) {
	p := qweatherProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/current/") {
			_, _ = w.Write([]byte(qweatherCurrent()))
			return
		}
		<-r.Context().Done()
	})
	cfg := weatherConfig()
	cfg.Provider = "qweather"
	cfg.QWeather = p.Config
	svc := widget.NewWeatherService(cfg, testutil.NewDB(t), p)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	got, err := svc.Current(ctx, p.Now())
	require.NoError(t, err)
	require.NotNil(t, got, "optional forecast timeout must not hide valid current conditions")
	require.NoError(t, ctx.Err(), "reserve time for the DB and outer response")
	require.Equal(t, 0.0, got.TemperatureC)
}

func TestQWeatherPreservesPlainTextAttributionAndUnknownCondition(t *testing.T) {
	body := strings.Replace(qweatherCurrent(), `"104"`, `"998"`, 1)
	body = strings.Replace(body, `"https://example.com/weather-credit"`, `"https://example.com/weather-credit","Synthetic credit: Weather source","https://user:secret@example.com/credit"`, 1)
	body = strings.Replace(body, `"observationTime":"2026-11-05T12:00:00+08:00",`, "", 1)
	p := qweatherProvider(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
	r, err := p.Fetch(context.Background(), 0, 0)
	require.NoError(t, err)
	require.Equal(t, 999, r.ConditionCode)
	wire := weatherJSON(t, r)
	require.NotContains(t, wire, "observed_at")
	require.Contains(t, wire["attributions"], "Synthetic credit: Weather source")
	require.NotContains(t, wire["attributions"], "https://user:secret@example.com/credit")
}

func TestQWeatherRejectsOtherSigningAlgorithmsAndTrailingPrivateKeys(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	for _, raw := range [][]byte{pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), append(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), []byte("unexpected trailing data")...)} {
		path := filepath.Join(t.TempDir(), "wrong.pem")
		require.NoError(t, os.WriteFile(path, raw, 0600))
		var calls atomic.Int32
		p := qweatherProvider(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = w.Write([]byte(qweatherCurrent())) })
		p.Config.APIKeyFile = ""
		p.Config.JWT = config.QWeatherJWT{DeveloperID: "Q123456789", ProjectID: "Project123", CredentialID: "Credential123", PrivateKeyFile: path}
		_, err = p.Fetch(context.Background(), 0, 0)
		require.Error(t, err)
		require.NotContains(t, err.Error(), path)
		require.Zero(t, calls.Load())
	}
}

func TestQWeatherForecastsKeepLocalDatesAcrossDSTAndBoundRows(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	hours := []map[string]any{{"forecastTime": "bad-time"}}
	for i := 40; i >= 0; i-- {
		at := start.Add(time.Duration(i) * time.Hour)
		entry := map[string]any{"forecastTime": at.Format(time.RFC3339), "temperature": map[string]any{"value": float64(i), "unit": "°C"}}
		hours = append(hours, entry)
		if i == 2 {
			hours = append(hours, entry)
		}
	}
	days := []map[string]any{}
	for i := 8; i >= 0; i-- {
		at := start.AddDate(0, 0, i)
		entry := map[string]any{"forecastStartTime": at.Format(time.RFC3339), "temperatureMin": map[string]any{"value": float64(i), "unit": "°C"}, "temperatureMax": map[string]any{"value": float64(i) + 10, "unit": "°C"}, "daytime": map[string]any{"condition": map[string]any{"code": "100"}}, "astro": map[string]any{"sunset": at.Add(17 * time.Hour).Format(time.RFC3339)}}
		days = append(days, entry)
	}
	p := qweatherProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/current/"):
			_, _ = w.Write([]byte(`{"condition":{"code":"100","text":"晴"},"temperature":{"value":10,"unit":"°C"}}`))
		case strings.Contains(r.URL.Path, "/hourly/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"hours": hours})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"days": days})
		}
	})
	p.Config.Timezone = "America/New_York"
	p.Now = func() time.Time { return start.Add(2 * time.Hour) }
	r, err := p.Fetch(context.Background(), 40.7, -74)
	require.NoError(t, err)
	require.Len(t, r.Hourly, 24)
	require.Len(t, r.Daily, 5)
	require.Equal(t, "2026-11-01T01:00:00-04:00", r.Hourly[1].Time)
	require.Equal(t, "2026-11-01T01:00:00-05:00", r.Hourly[2].Time)
	require.Equal(t, "2026-11-01", r.Daily[0].Date)
	require.Equal(t, "2026-11-02", r.Daily[1].Date)
	require.Equal(t, 0.0, *r.TemperatureMinC)
	cfg := weatherConfig()
	cfg.Provider = "qweather"
	cfg.QWeather = p.Config
	cfg.RefreshInterval = config.Duration(48 * time.Hour)
	svc := widget.NewWeatherService(cfg, testutil.NewDB(t), p)
	_, err = svc.Current(context.Background(), start.Add(2*time.Hour))
	require.NoError(t, err)
	cached, err := svc.Current(context.Background(), start.AddDate(0, 0, 1).Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1.0, *cached.TemperatureMinC, "midnight projection follows the IANA zone after DST")
}

func TestQWeatherForecastPermissionBackoffDoesNotBlockCurrent(t *testing.T) {
	var current, hourly, daily atomic.Int32
	p := qweatherProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/current/"):
			current.Add(1)
			_, _ = w.Write([]byte(qweatherCurrent()))
		case strings.Contains(r.URL.Path, "/hourly/"):
			hourly.Add(1)
			w.WriteHeader(403)
		default:
			daily.Add(1)
			w.WriteHeader(403)
		}
	})
	for range 2 {
		r, err := p.Fetch(context.Background(), 0, 0)
		require.NoError(t, err)
		require.NotNil(t, r)
	}
	require.EqualValues(t, 2, current.Load())
	require.EqualValues(t, 1, hourly.Load())
	require.EqualValues(t, 1, daily.Load())
}
