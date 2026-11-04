package widget_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func weatherJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	return got
}

func TestWeatherForecastQueriesAndLocalDates(t *testing.T) {
	// A UTC instant on the previous calendar day is already tomorrow in Tokyo.
	current := time.Date(2026, 11, 3, 16, 30, 0, 0, time.UTC)
	midnight := time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)
	sunset := time.Date(2026, 11, 4, 7, 44, 0, 0, time.UTC)
	var hourlyTime []int64
	var hourlyTemp []float64
	for i := range 48 {
		hourlyTime = append(hourlyTime, midnight.Add(time.Duration(i)*time.Hour).Unix())
		hourlyTemp = append(hourlyTemp, float64(i)+10)
	}
	var dailyTime []int64
	var dailyMin, dailyMax []float64
	var codes []int
	var sunsets []int64
	for i := range 7 {
		dailyTime = append(dailyTime, midnight.AddDate(0, 0, i).Unix())
		dailyMin = append(dailyMin, float64(i)+8)
		dailyMax = append(dailyMax, float64(i)+19)
		codes = append(codes, 3)
		sunsets = append(sunsets, sunset.AddDate(0, 0, i).Unix())
	}
	body, err := json.Marshal(map[string]any{
		"timezone": "Asia/Tokyo", "utc_offset_seconds": 32400,
		"current": map[string]any{"time": current.Unix(), "temperature_2m": 17.2, "weather_code": 3, "relative_humidity_2m": 67, "wind_speed_10m": 8.4, "wind_direction_10m": 225},
		"hourly":  map[string]any{"time": hourlyTime, "temperature_2m": hourlyTemp},
		"daily":   map[string]any{"time": dailyTime, "temperature_2m_min": dailyMin, "temperature_2m_max": dailyMax, "weather_code": codes, "sunset": sunsets},
	})
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		require.Equal(t, "auto", q.Get("timezone"))
		require.Equal(t, "unixtime", q.Get("timeformat"))
		require.Equal(t, "5", q.Get("forecast_days"))
		require.Equal(t, "temperature_2m", q.Get("hourly"))
		require.Contains(t, q.Get("current"), "relative_humidity_2m")
		require.Contains(t, q.Get("current"), "wind_speed_10m")
		require.Contains(t, q.Get("current"), "wind_direction_10m")
		require.Contains(t, q.Get("daily"), "temperature_2m_min")
		require.Contains(t, q.Get("daily"), "temperature_2m_max")
		require.Contains(t, q.Get("daily"), "weather_code")
		require.Contains(t, q.Get("daily"), "sunset")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	provider := &widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()}
	reading, err := provider.Fetch(context.Background(), 35, 139)
	require.NoError(t, err)
	got := weatherJSON(t, reading)
	require.Equal(t, 67.0, got["humidity_pct"])
	require.Equal(t, 8.4, got["wind_speed_kmh"])
	require.Equal(t, 225.0, got["wind_direction_deg"])
	require.Equal(t, 8.0, got["temperature_min_c"])
	require.Equal(t, 19.0, got["temperature_max_c"])
	require.Equal(t, "2026-11-04T16:44:00+09:00", got["sunset"])
	require.Equal(t, "Asia/Tokyo", got["forecast_timezone"])
	hourly := got["hourly"].([]any)
	require.Len(t, hourly, 24)
	require.Equal(t, "2026-11-04T01:00:00+09:00", hourly[0].(map[string]any)["time"])
	require.Equal(t, 11.0, hourly[0].(map[string]any)["temperature_c"])
	daily := got["daily"].([]any)
	require.Len(t, daily, 5)
	require.Equal(t, "2026-11-04", daily[0].(map[string]any)["date"])
	require.Equal(t, "2026-11-08", daily[4].(map[string]any)["date"])

	// The widget service persists and exposes the optional forecast too.
	db := testutil.NewDB(t)
	svc := widget.NewWeatherService(weatherConfig(), db, provider)
	weather, err := svc.Current(context.Background(), current)
	require.NoError(t, err)
	wire := weatherJSON(t, weather)
	for _, field := range []string{"humidity_pct", "wind_speed_kmh", "wind_direction_deg", "temperature_min_c", "temperature_max_c", "sunset", "forecast_timezone", "hourly", "daily"} {
		require.Equal(t, got[field], wire[field], field)
	}
}

func TestWeatherForecastFallbackOffsetAndNullableArrays(t *testing.T) {
	// Deliberately incomplete, malformed optional cells must not discard current conditions.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"timezone":"Unknown/Zone","utc_offset_seconds":-18000,
			"current":{"time":1793782800,"temperature_2m":0,"weather_code":0,"relative_humidity_2m":101,"wind_speed_10m":-1,"wind_direction_10m":"NaN"},
			"hourly":{"time":[1793782800,null,"NaN",1793786400,1793790000],"temperature_2m":[0,4,5,null,"NaN"]},
			"daily":{"time":[1793768400,null,1793854800],"temperature_2m_min":[0],"temperature_2m_max":[null,20,25],"weather_code":[0,3,"NaN"],"sunset":[1793833200]}}`))
	}))
	defer srv.Close()
	reading, err := (&widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()}).Fetch(context.Background(), 0, 0)
	require.NoError(t, err)
	got := weatherJSON(t, reading)
	require.Equal(t, 0.0, got["temperature_c"])
	require.Equal(t, 0.0, got["condition_code"])
	for _, field := range []string{"humidity_pct", "wind_speed_kmh", "wind_direction_deg", "temperature_max_c"} {
		require.NotContains(t, got, field, field)
	}
	require.Equal(t, 0.0, got["temperature_min_c"], "zero is a valid observation")
	daily := got["daily"].([]any)
	require.Len(t, daily, 2)
	require.Equal(t, "2026-11-04", daily[0].(map[string]any)["date"])
	require.Equal(t, "2026-11-05", daily[1].(map[string]any)["date"])
	require.Nil(t, daily[1].(map[string]any)["min_c"])
	require.Nil(t, daily[1].(map[string]any)["condition_code"])
	hourly := got["hourly"].([]any)
	require.Len(t, hourly, 3)
	require.Equal(t, "2026-11-04T04:00:00-05:00", hourly[0].(map[string]any)["time"])
	require.Equal(t, 0.0, hourly[0].(map[string]any)["temperature_c"])
	require.Nil(t, hourly[1].(map[string]any)["temperature_c"])
	require.Nil(t, hourly[2].(map[string]any)["temperature_c"])
}

func TestWeatherCurrentOnlyAndOldCacheRemainCompatible(t *testing.T) {
	db := testutil.NewDB(t)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.WidgetCache().Put(context.Background(), store.CachedWidget{
		Widget: domain.WidgetWeather, FetchedAt: now, ExpiresAt: now.Add(time.Hour),
		Payload: `{"reading":{"temperature_c":12.5,"condition_code":3,"condition_text":"overcast"},"fetched_at":"2026-11-04T12:00:00Z"}`,
	}))
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"current":{"temperature_2m":0,"weather_code":0}}`))
	}))
	defer srv.Close()
	svc := widget.NewWeatherService(weatherConfig(), db, &widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()})
	old, err := svc.Current(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, 12.5, old.TemperatureC)
	require.EqualValues(t, 0, calls.Load())
	for _, value := range []any{old} {
		for _, field := range []string{"humidity_pct", "wind_speed_kmh", "temperature_min_c", "sunset", "hourly", "daily"} {
			require.NotContains(t, weatherJSON(t, value), field)
		}
	}
	fresh, err := svc.Current(context.Background(), now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 0.0, fresh.TemperatureC)
	require.Equal(t, 0, fresh.ConditionCode)
	require.EqualValues(t, 1, calls.Load())
	require.NotContains(t, weatherJSON(t, fresh), "daily")
}

func TestWeatherMissingCurrentKeepsCachedForecast(t *testing.T) {
	for _, current := range []string{`{}`, `{"temperature_2m":null,"weather_code":0}`, `{"temperature_2m":19}`, `{"temperature_2m":19,"weather_code":null}`, `{"temperature_2m":"NaN","weather_code":0}`, `{"temperature_2m":19,"weather_code":-1}`, `{"temperature_2m":19,"weather_code":0.5}`} {
		t.Run(current, func(t *testing.T) {
			db := testutil.NewDB(t)
			var broken atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if broken.Load() {
					_, _ = fmt.Fprintf(w, `{"current":%s}`, current)
					return
				}
				_, _ = w.Write([]byte(`{"timezone":"UTC","current":{"temperature_2m":18,"weather_code":3},"daily":{"time":[1793746800],"temperature_2m_min":[12],"temperature_2m_max":[20],"weather_code":[3]}}`))
			}))
			defer srv.Close()
			svc := widget.NewWeatherService(weatherConfig(), db, &widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()})
			now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
			first, err := svc.Current(context.Background(), now)
			require.NoError(t, err)
			firstWire := weatherJSON(t, first)
			require.Contains(t, firstWire, "daily")
			broken.Store(true)
			later, err := svc.Current(context.Background(), now.Add(3*time.Hour))
			require.NoError(t, err)
			require.NotNil(t, later)
			require.Equal(t, first.TemperatureC, later.TemperatureC)
			require.Equal(t, firstWire["daily"], weatherJSON(t, later)["daily"])
			require.Equal(t, first.FetchedAt, later.FetchedAt)
			require.True(t, later.Stale)
		})
	}
}

func TestWeatherMalformedOptionalForecastDoesNotHideCurrent(t *testing.T) {
	for _, forecast := range []string{
		`"hourly":[],"daily":"unavailable"`,
		`"hourly":{"time":"invalid"},"daily":{"time":{}}`,
		`"hourly":{"time":[null,999999999999999999],"temperature_2m":[12,13]},"daily":{"time":[null,999999999999999999]}`,
		`"hourly":{"time":[]},"daily":{"time":[]}`,
	} {
		t.Run(forecast, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"current":{"temperature_2m":12,"weather_code":3,"relative_humidity_2m":0,"wind_speed_10m":0,"wind_direction_10m":0},%s}`, forecast)
			}))
			defer srv.Close()
			reading, err := (&widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()}).Fetch(context.Background(), 0, 0)
			require.NoError(t, err)
			got := weatherJSON(t, reading)
			require.Equal(t, 12.0, got["temperature_c"])
			require.Equal(t, 0.0, got["humidity_pct"])
			require.Equal(t, 0.0, got["wind_speed_kmh"])
			require.Equal(t, 0.0, got["wind_direction_deg"])
			require.NotContains(t, got, "hourly")
			require.NotContains(t, got, "daily")
		})
	}
}

func TestWeatherHourlyRetainsCurrentLocalHourAcrossHalfHourOffsetsAndDST(t *testing.T) {
	for _, test := range []struct {
		name, timezone, current, first string
		hours                          []string
	}{
		{
			name: "half hour offset", timezone: "Asia/Kolkata", current: "2026-11-04T01:45:00+05:30", first: "2026-11-04T01:00:00+05:30",
			hours: []string{"2026-11-04T00:00:00+05:30", "2026-11-04T01:00:00+05:30", "2026-11-04T02:00:00+05:30"},
		},
		{
			name: "repeated DST hour", timezone: "America/New_York", current: "2026-11-01T01:30:00-04:00", first: "2026-11-01T01:00:00-04:00",
			hours: []string{"2026-11-01T00:00:00-04:00", "2026-11-01T01:00:00-04:00", "2026-11-01T01:00:00-05:00", "2026-11-01T02:00:00-05:00"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			current, err := time.Parse(time.RFC3339, test.current)
			require.NoError(t, err)
			var times []int64
			var temperatures []float64
			for i, hour := range test.hours {
				at, err := time.Parse(time.RFC3339, hour)
				require.NoError(t, err)
				times = append(times, at.Unix())
				temperatures = append(temperatures, float64(i))
			}
			body, err := json.Marshal(map[string]any{
				"timezone": test.timezone, "current": map[string]any{"time": current.Unix(), "temperature_2m": 12, "weather_code": 3},
				"hourly": map[string]any{"time": times, "temperature_2m": temperatures},
			})
			require.NoError(t, err)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
			defer srv.Close()
			reading, err := (&widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()}).Fetch(context.Background(), 0, 0)
			require.NoError(t, err)
			hours := weatherJSON(t, reading)["hourly"].([]any)
			require.Len(t, hours, len(test.hours)-1)
			require.Equal(t, test.first, hours[0].(map[string]any)["time"])
			for i, hour := range hours {
				require.Equal(t, test.hours[i+1], hour.(map[string]any)["time"])
			}
		})
	}
}

func TestWeatherDailyDatesUseProviderOffsetAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	fixed := time.FixedZone("provider-fixed", -4*3600)
	current := time.Date(2026, 10, 31, 12, 0, 0, 0, loc)
	var times, sunsets []int64
	var mins, maxs []int
	want := []string{"2026-10-31", "2026-11-01", "2026-11-02", "2026-11-03", "2026-11-04"}
	for i := range 5 {
		times = append(times, time.Date(2026, 10, 31+i, 0, 0, 0, 0, fixed).Unix())
		sunsets = append(sunsets, time.Date(2026, 10, 31+i, 18, 0, 0, 0, loc).Unix())
		mins, maxs = append(mins, 10+i), append(maxs, 20+i)
	}
	for _, test := range []struct {
		withOffset bool
		current    time.Time
		firstDay   int
	}{
		{true, current, 0}, {false, current, 0},
		{true, time.Date(2026, 11, 1, 23, 30, 0, 0, loc), 1},
		{false, time.Date(2026, 11, 1, 23, 30, 0, 0, loc), 1},
	} {
		t.Run(fmt.Sprintf("offset_present=%t,current_day=%d", test.withOffset, test.firstDay), func(t *testing.T) {
			response := map[string]any{
				"timezone": "America/New_York", "current": map[string]any{"time": test.current.Unix(), "temperature_2m": 17, "weather_code": 3},
				"daily": map[string]any{"time": times, "temperature_2m_min": mins, "temperature_2m_max": maxs, "sunset": sunsets},
			}
			if test.withOffset {
				response["utc_offset_seconds"] = -4 * 3600
			}
			body, err := json.Marshal(response)
			require.NoError(t, err)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
			defer srv.Close()
			reading, err := (&widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()}).Fetch(context.Background(), 0, 0)
			require.NoError(t, err)
			require.Len(t, reading.Daily, 5-test.firstDay)
			for i, day := range reading.Daily {
				index := i + test.firstDay
				require.Equal(t, want[index], day.Date)
				require.Equal(t, float64(10+index), *day.MinC)
				require.Equal(t, float64(20+index), *day.MaxC)
				require.Equal(t, time.Unix(sunsets[index], 0).In(loc).Format(time.RFC3339), day.Sunset, "sunset keeps actual DST-aware offset")
			}
			require.Equal(t, float64(10+test.firstDay), *reading.TemperatureMinC)
		})
	}
}

func TestWeatherCachedForecastSummaryTracksLocationDate(t *testing.T) {
	for _, test := range []struct {
		name, timezone string
		withOffset     bool
		offset         int
	}{
		{name: "IANA and offset", timezone: "Asia/Singapore", withOffset: true, offset: 8 * 3600},
		{name: "IANA without offset", timezone: "Asia/Singapore", offset: 8 * 3600},
		{name: "invalid IANA with offset", timezone: "Unknown/Zone", withOffset: true, offset: 8 * 3600},
		{name: "offset only", withOffset: true, offset: 8 * 3600},
		{name: "no timezone defaults UTC"},
	} {
		t.Run(test.name, func(t *testing.T) {
			loc := time.FixedZone("provider-fixed", test.offset)
			first := time.Date(2026, 11, 4, 23, 50, 0, 0, loc)
			response := map[string]any{
				"timezone": test.timezone, "current": map[string]any{"time": first.Unix(), "temperature_2m": 17, "weather_code": 3},
				"daily": map[string]any{
					"time":               []int64{time.Date(2026, 11, 4, 0, 0, 0, 0, loc).Unix(), time.Date(2026, 11, 5, 0, 0, 0, 0, loc).Unix()},
					"temperature_2m_min": []any{10, nil}, "temperature_2m_max": []int{20, 0},
					"sunset": []any{time.Date(2026, 11, 4, 18, 0, 0, 0, loc).Unix(), nil},
				},
			}
			if test.withOffset {
				response["utc_offset_seconds"] = test.offset
			}
			body, err := json.Marshal(response)
			require.NoError(t, err)
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = w.Write(body) }))
			defer srv.Close()
			cfg := weatherConfig()
			cfg.RefreshInterval = config.Duration(72 * time.Hour)
			svc := widget.NewWeatherService(cfg, testutil.NewDB(t), &widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()})
			initial, err := svc.Current(context.Background(), first)
			require.NoError(t, err)
			require.Equal(t, 10.0, *initial.TemperatureMinC)
			require.Equal(t, 20.0, *initial.TemperatureMaxC)
			require.NotEmpty(t, initial.Sunset)
			today, err := svc.Current(context.Background(), first.Add(20*time.Minute))
			require.NoError(t, err)
			require.Nil(t, today.TemperatureMinC, "do not inherit yesterday's minimum for today's missing reading")
			require.NotNil(t, today.TemperatureMaxC)
			require.Equal(t, 0.0, *today.TemperatureMaxC)
			require.Empty(t, today.Sunset)
			require.Equal(t, initial.FetchedAt, today.FetchedAt)
			require.False(t, today.Stale)
			outdated, err := svc.Current(context.Background(), first.Add(48*time.Hour))
			require.NoError(t, err)
			require.Nil(t, outdated.TemperatureMinC)
			require.Nil(t, outdated.TemperatureMaxC)
			require.Empty(t, outdated.Sunset)
			require.True(t, outdated.Stale)
			require.EqualValues(t, 1, calls.Load(), "summary projection must reuse the unexpired cache")
		})
	}
}

func TestWeatherCachedSummaryUsesRealLocalDateAfterFallDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	fixed := time.FixedZone("provider-fixed", -4*3600)
	first := time.Date(2026, 10, 31, 12, 0, 0, 0, loc)
	var times []int64
	for i := range 3 {
		times = append(times, time.Date(2026, 10, 31+i, 0, 0, 0, 0, fixed).Unix())
	}
	body, err := json.Marshal(map[string]any{
		"timezone": "America/New_York", "utc_offset_seconds": -4 * 3600,
		"current": map[string]any{"time": first.Unix(), "temperature_2m": 17, "weather_code": 3},
		"daily":   map[string]any{"time": times, "temperature_2m_min": []int{10, 11, 12}, "temperature_2m_max": []int{20, 21, 22}},
	})
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()
	cfg := weatherConfig()
	cfg.RefreshInterval = config.Duration(72 * time.Hour)
	svc := widget.NewWeatherService(cfg, testutil.NewDB(t), &widget.OpenMeteo{BaseURL: srv.URL, Client: srv.Client()})
	_, err = svc.Current(context.Background(), first)
	require.NoError(t, err)
	// The fixed provider offset has already reached November 2, but real local
	// civil time is still November 1 after the fall clock change.
	now := time.Date(2026, 11, 1, 23, 30, 0, 0, loc)
	today, err := svc.Current(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, 11.0, *today.TemperatureMinC)
	require.Equal(t, 21.0, *today.TemperatureMaxC)
}
