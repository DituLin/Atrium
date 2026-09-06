package widget_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/DituLin/Atrium/internal/widget"
)

func weatherConfig() config.Weather {
	return config.Weather{
		Enabled:         true,
		Provider:        "open-meteo",
		Latitude:        1.3521,
		Longitude:       103.8198,
		LocationLabel:   "Home",
		RefreshInterval: config.Duration(30 * time.Minute),
		StaleAfter:      config.Duration(2 * time.Hour),
	}
}

func TestWeatherIsNilWhenDisabled(t *testing.T) {
	db := testutil.NewDB(t)
	cfg := weatherConfig()
	cfg.Enabled = false
	require.Nil(t, widget.NewWeatherService(cfg, db, nil))
}

func TestWeatherFetchesThroughProviderStub(t *testing.T) {
	db := testutil.NewDB(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "1.3521", r.URL.Query().Get("latitude"))
		require.Equal(t, "103.8198", r.URL.Query().Get("longitude"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"current":{"temperature_2m":29.4,"weather_code":61}}`))
	}))
	defer srv.Close()

	cfg := weatherConfig()
	svc := widget.NewWeatherService(cfg, db, &widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()})
	require.NotNil(t, svc)

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	got, err := svc.Current(context.Background(), now)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "open-meteo", got.Provider)
	require.Equal(t, "Home", got.LocationLabel)
	require.InDelta(t, 29.4, got.TemperatureC, 0.001)
	require.Equal(t, 61, got.ConditionCode)
	require.Equal(t, "rain", got.ConditionText)
	require.False(t, got.Stale)
	require.EqualValues(t, 1, calls.Load())

	// Inside the refresh interval the cached payload is reused.
	_, err = svc.Current(context.Background(), now.Add(10*time.Minute))
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "no refresh before the interval elapses")

	// Past the refresh interval the provider is called again.
	_, err = svc.Current(context.Background(), now.Add(31*time.Minute))
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
}

func TestWeatherStaleAfterTwoHours(t *testing.T) {
	db := testutil.NewDB(t)
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"current":{"temperature_2m":21,"weather_code":0}}`))
	}))
	defer srv.Close()

	svc := widget.NewWeatherService(weatherConfig(), db, &widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()})
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	first, err := svc.Current(context.Background(), now)
	require.NoError(t, err)
	require.False(t, first.Stale)

	// The provider fails from here on; the last payload must survive.
	fail.Store(true)
	later, err := svc.Current(context.Background(), now.Add(3*time.Hour))
	require.NoError(t, err)
	require.NotNil(t, later, "a provider failure keeps the last payload")
	require.InDelta(t, 21, later.TemperatureC, 0.001)
	require.True(t, later.Stale, "older than stale_after must be marked stale")
	require.Equal(t, "clear", later.ConditionText)
}

func TestWeatherHiddenWhenNeverFetched(t *testing.T) {
	db := testutil.NewDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	svc := widget.NewWeatherService(weatherConfig(), db, &widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()})
	got, err := svc.Current(context.Background(), time.Now())
	require.NoError(t, err)
	require.Nil(t, got, "a widget that never fetched successfully stays hidden")
}

func TestConditionTextIsAClosedTable(t *testing.T) {
	require.Equal(t, "clear", widget.ConditionText(0))
	require.Equal(t, "fog", widget.ConditionText(45))
	require.Equal(t, "thunderstorm", widget.ConditionText(95))
	require.Equal(t, "unknown", widget.ConditionText(1234))
}
