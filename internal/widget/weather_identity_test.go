package widget_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/DituLin/Atrium/internal/widget"
)

type identityProvider struct {
	name    string
	reading *widget.Reading
	err     error
	calls   int
}

func (p *identityProvider) Name() string { return p.name }
func (p *identityProvider) Fetch(context.Context, float64, float64) (*widget.Reading, error) {
	p.calls++
	return p.reading, p.err
}

func TestWeatherFactoryUsesQWeatherWithoutOpenMeteoFallback(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"current":{"temperature_2m":10,"weather_code":0}}`))
	}))
	defer srv.Close()
	cfg := weatherConfig()
	cfg.Provider = "qweather"
	cfg.BaseURL = srv.URL
	cfg.QWeather = config.QWeather{APIHost: "synthetic.qweatherapi.com", Timezone: "Asia/Shanghai", APIKeyFile: "/missing/synthetic-weather-key"}
	svc := widget.NewWeatherService(cfg, testutil.NewDB(t), nil)
	got, err := svc.Current(context.Background(), time.Now())
	require.NoError(t, err)
	require.Nil(t, got)
	require.Zero(t, calls.Load())
}

func TestWeatherCacheSeparatesProviderAndRequestedPlace(t *testing.T) {
	for _, change := range []string{"provider", "coordinate", "timezone", "host"} {
		t.Run(change, func(t *testing.T) {
			db := testutil.NewDB(t)
			now := time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC)
			cfg := weatherConfig()
			cfg.Provider = "qweather"
			cfg.QWeather = config.QWeather{APIHost: "first.qweatherapi.com", Timezone: "Asia/Shanghai"}
			first := &identityProvider{name: "qweather", reading: &widget.Reading{TemperatureC: 11, ConditionCode: 0}}
			_, err := widget.NewWeatherService(cfg, db, first).Current(context.Background(), now)
			require.NoError(t, err)
			name := "qweather"
			switch change {
			case "provider":
				name = "open-meteo"
				cfg.Provider = name
			case "coordinate":
				cfg.Longitude += 1
			case "timezone":
				cfg.QWeather.Timezone = "Asia/Tokyo"
			case "host":
				cfg.QWeather.APIHost = "second.qweatherapi.com"
			}
			second := &identityProvider{name: name, reading: &widget.Reading{TemperatureC: 22, ConditionCode: 3}}
			got, err := widget.NewWeatherService(cfg, db, second).Current(context.Background(), now.Add(time.Minute))
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, 22.0, got.TemperatureC)
			require.Equal(t, 1, second.calls)
			// A different location with an unreachable provider cannot replay an
			// older location under the newly configured display label.
			cfg.Longitude += 1
			failed := &identityProvider{name: name, err: errors.New("offline")}
			got, err = widget.NewWeatherService(cfg, db, failed).Current(context.Background(), now.Add(2*time.Minute))
			require.NoError(t, err)
			require.Nil(t, got)
		})
	}
}

func TestQWeatherCacheUsesActualCoordinatePrecision(t *testing.T) {
	db := testutil.NewDB(t)
	now := time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC)
	cfg := weatherConfig()
	cfg.Provider = "qweather"
	cfg.QWeather.Timezone = "Asia/Shanghai"
	cfg.Latitude = 30.2001
	first := &identityProvider{name: "qweather", reading: &widget.Reading{TemperatureC: 11, ConditionCode: 0}}
	_, err := widget.NewWeatherService(cfg, db, first).Current(context.Background(), now)
	require.NoError(t, err)
	cfg.Latitude = 30.2002
	second := &identityProvider{name: "qweather", err: errors.New("should not fetch")}
	got, err := widget.NewWeatherService(cfg, db, second).Current(context.Background(), now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 11.0, got.TemperatureC)
	require.Zero(t, second.calls)
}

func TestWeatherLegacyCacheRefreshesBeforeOpenMeteoFallbackAndNeverBecomesQWeather(t *testing.T) {
	for _, name := range []string{"open-meteo", "qweather"} {
		t.Run(name, func(t *testing.T) {
			db := testutil.NewDB(t)
			now := time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC)
			require.NoError(t, db.WidgetCache().Put(context.Background(), store.CachedWidget{Widget: domain.WidgetWeather, Payload: `{"reading":{"temperature_c":8,"condition_code":3}}`, FetchedAt: now, ExpiresAt: now.Add(time.Hour)}))
			cfg := weatherConfig()
			cfg.Provider = name
			p := &identityProvider{name: name, err: errors.New("offline")}
			got, err := widget.NewWeatherService(cfg, db, p).Current(context.Background(), now)
			require.NoError(t, err)
			require.Equal(t, 1, p.calls)
			if name == "open-meteo" {
				require.NotNil(t, got)
				require.Equal(t, 8.0, got.TemperatureC)
			} else {
				require.Nil(t, got)
			}
		})
	}
}

func TestWeatherObservationAgeAndAttributionsSurviveCache(t *testing.T) {
	now := time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC)
	p := &identityProvider{name: "qweather", reading: &widget.Reading{TemperatureC: 18, ConditionCode: 3, ObservedAt: now.Add(-3 * time.Hour).Format(time.RFC3339), Attributions: []string{"https://www.qweather.com", "Synthetic weather credit"}}}
	cfg := weatherConfig()
	cfg.Provider = "qweather"
	svc := widget.NewWeatherService(cfg, testutil.NewDB(t), p)
	first, err := svc.Current(context.Background(), now)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.True(t, first.Stale)
	second, err := svc.Current(context.Background(), now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, second)
	require.True(t, second.Stale)
	require.Equal(t, 1, p.calls)
	wire := weatherJSON(t, second)
	require.Equal(t, p.reading.ObservedAt, wire["observed_at"])
	require.Contains(t, wire["attributions"], "Synthetic weather credit")
}

type blockingWeatherProvider struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (p *blockingWeatherProvider) Name() string { return "qweather" }
func (p *blockingWeatherProvider) Fetch(context.Context, float64, float64) (*widget.Reading, error) {
	if p.calls.Add(1) == 1 {
		close(p.started)
		<-p.release
	}
	return &widget.Reading{TemperatureC: 18, ConditionCode: 3}, nil
}

func TestWeatherConcurrentHomeRequestsShareOneDueRefresh(t *testing.T) {
	p := &blockingWeatherProvider{started: make(chan struct{}), release: make(chan struct{})}
	cfg := weatherConfig()
	cfg.Provider = "qweather"
	svc := widget.NewWeatherService(cfg, testutil.NewDB(t), p)
	now := time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC)
	var done sync.WaitGroup
	ready := make(chan struct{}, 12)
	for range 12 {
		done.Add(1)
		go func() {
			defer done.Done()
			ready <- struct{}{}
			got, err := svc.Current(context.Background(), now)
			require.NoError(t, err)
			require.NotNil(t, got)
		}()
	}
	for range 12 {
		<-ready
	}
	<-p.started
	// The first provider request remains pending while every screen enters
	// Current; queued refreshes must reread its completed cache afterwards.
	time.Sleep(30 * time.Millisecond)
	close(p.release)
	done.Wait()
	require.EqualValues(t, 1, p.calls.Load())
	require.NoError(t, svc.Refresh(context.Background(), now))
	require.EqualValues(t, 2, p.calls.Load(), "explicit refresh still bypasses cache")
}

func TestWeatherCancelledRequestWaitingForRefreshDoesNotFetch(t *testing.T) {
	p := &blockingWeatherProvider{started: make(chan struct{}), release: make(chan struct{})}
	cfg := weatherConfig()
	cfg.Provider = "qweather"
	svc := widget.NewWeatherService(cfg, testutil.NewDB(t), p)
	now := time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC)
	first := make(chan error, 1)
	go func() { _, err := svc.Current(context.Background(), now); first <- err }()
	<-p.started
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	second := make(chan error, 1)
	go func() { _, err := svc.Current(ctx, now); second <- err }()
	<-ctx.Done()
	close(p.release)
	require.NoError(t, <-first)
	require.ErrorIs(t, <-second, context.DeadlineExceeded)
	require.EqualValues(t, 1, p.calls.Load())
}
