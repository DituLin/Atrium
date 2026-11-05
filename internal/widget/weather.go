package widget

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
)

// Reading is one provider observation.
type Reading struct {
	TemperatureC     float64       `json:"temperature_c"`
	ConditionCode    int           `json:"condition_code"`
	ConditionText    string        `json:"condition_text"`
	HumidityPct      *float64      `json:"humidity_pct,omitempty"`
	WindSpeedKMH     *float64      `json:"wind_speed_kmh,omitempty"`
	WindDirectionDeg *float64      `json:"wind_direction_deg,omitempty"`
	TemperatureMinC  *float64      `json:"temperature_min_c,omitempty"`
	TemperatureMaxC  *float64      `json:"temperature_max_c,omitempty"`
	Sunset           string        `json:"sunset,omitempty"`
	ForecastTimezone string        `json:"forecast_timezone,omitempty"`
	Hourly           []WeatherHour `json:"hourly,omitempty"`
	Daily            []WeatherDay  `json:"daily,omitempty"`
	ObservedAt       string        `json:"observed_at,omitempty"`
	Attributions     []string      `json:"attributions,omitempty"`
	// Retained only in the internal widget cache when no usable IANA zone is
	// available. It is not part of the screen's weather payload.
	forecastUTCOffsetSeconds *int
}

// Provider fetches the current conditions for a location. The interface keeps
// the HTTP call replaceable by a stub in tests.
type Provider interface {
	// Name identifies the provider in the payload.
	Name() string
	// Fetch returns the current conditions or an error.
	Fetch(ctx context.Context, lat, lon float64) (*Reading, error)
}

// DefaultOpenMeteoURL is the free forecast endpoint (no API key).
const DefaultOpenMeteoURL = "https://api.open-meteo.com/v1/forecast"

// OpenMeteo implements Provider against the Open-Meteo forecast API.
type OpenMeteo struct {
	BaseURL string
	Client  *http.Client
}

// Name identifies the provider.
func (o *OpenMeteo) Name() string { return "open-meteo" }

// Fetch reads the current conditions for a coordinate.
func (o *OpenMeteo) Fetch(ctx context.Context, lat, lon float64) (*Reading, error) {
	base := o.BaseURL
	if base == "" {
		base = DefaultOpenMeteoURL
	}
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("current", "temperature_2m,weather_code,relative_humidity_2m,wind_speed_10m,wind_direction_10m")
	q.Set("daily", "temperature_2m_max,temperature_2m_min,weather_code,sunset")
	q.Set("hourly", "temperature_2m")
	q.Set("forecast_days", "5")
	q.Set("timezone", "auto")
	q.Set("timeformat", "unixtime")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("weather: build request: %w", err)
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weather: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weather: provider returned %d", resp.StatusCode)
	}
	var body struct {
		Current   json.RawMessage `json:"current"`
		Hourly    json.RawMessage `json:"hourly"`
		Daily     json.RawMessage `json:"daily"`
		Timezone  json.RawMessage `json:"timezone"`
		UTCOffset json.RawMessage `json:"utc_offset_seconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("weather: decode response: %w", err)
	}
	var current map[string]json.RawMessage
	if err := json.Unmarshal(body.Current, &current); err != nil {
		return nil, fmt.Errorf("weather: invalid current conditions: %w", err)
	}
	temperature := weatherNumber(current["temperature_2m"])
	code := weatherCode(current["weather_code"])
	if temperature == nil || code == nil {
		return nil, errors.New("weather: current temperature or weather code is missing or invalid")
	}
	reading := &Reading{
		TemperatureC: *temperature, ConditionCode: *code, ConditionText: ConditionText(*code),
		HumidityPct:      weatherNumberInRange(current["relative_humidity_2m"], 0, 100),
		WindSpeedKMH:     weatherNumberInRange(current["wind_speed_10m"], 0, math.MaxFloat64),
		WindDirectionDeg: weatherNumberInRange(current["wind_direction_10m"], 0, 360),
	}
	loc, timezone := weatherLocation(body.Timezone, body.UTCOffset)
	reading.ForecastTimezone = timezone
	currentTime := weatherUnixTime(current["time"])
	dailyLoc, offset := weatherDailyLocation(body.UTCOffset, loc, currentTime, body.Daily)
	reading.forecastUTCOffsetSeconds = &offset
	reading.Hourly = weatherHours(body.Hourly, loc, currentTime)
	reading.Daily = weatherDays(body.Daily, dailyLoc, loc, currentTime)
	if len(reading.Daily) > 0 {
		date := reading.Daily[0].Date
		if currentTime != nil {
			date = currentTime.In(loc).Format(time.DateOnly)
		}
		reading.projectDailySummary(date)
	}
	return reading, nil
}

// Optional provider cells are decoded individually: a bad forecast cell must
// not erase an otherwise valid observation, or turn a missing value into zero.
func weatherNumber(raw json.RawMessage) *float64 {
	var value *float64
	if json.Unmarshal(raw, &value) != nil || value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return value
}

func weatherNumberInRange(raw json.RawMessage, min, max float64) *float64 {
	value := weatherNumber(raw)
	if value == nil || *value < min || *value > max {
		return nil
	}
	return value
}

func weatherCode(raw json.RawMessage) *int {
	var code *int
	if json.Unmarshal(raw, &code) != nil || code == nil || *code < 0 {
		return nil
	}
	return code
}

func weatherUnixTime(raw json.RawMessage) *time.Time {
	var seconds *int64
	if json.Unmarshal(raw, &seconds) != nil || seconds == nil {
		return nil
	}
	value := time.Unix(*seconds, 0).UTC()
	if value.Year() < 1 || value.Year() > 9999 {
		return nil
	}
	return &value
}

func weatherLocation(rawTimezone, rawOffset json.RawMessage) (*time.Location, string) {
	var name string
	if json.Unmarshal(rawTimezone, &name) == nil && name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc, name
		}
	}
	if offset := weatherUTCOffset(rawOffset); offset != nil {
		return time.FixedZone("", *offset), ""
	}
	return time.UTC, ""
}

func weatherUTCOffset(raw json.RawMessage) *int {
	var offset *int
	if json.Unmarshal(raw, &offset) != nil || offset == nil || *offset <= -86400 || *offset >= 86400 {
		return nil
	}
	return offset
}

// Open-Meteo daily boundaries use the response's fixed UTC offset even when
// the forecast spans a DST transition. Applying the IANA zone to those epoch
// values can move a midnight into the preceding date. Actual instants (hourly
// forecasts and sunset) continue to use the DST-aware location.
func weatherDailyLocation(rawOffset json.RawMessage, loc *time.Location, currentTime *time.Time, rawDaily json.RawMessage) (*time.Location, int) {
	if offset := weatherUTCOffset(rawOffset); offset != nil {
		return time.FixedZone("", *offset), *offset
	}
	// For older or partial responses without utc_offset_seconds, freeze the
	// IANA offset at the first daily boundary. A current observation after a
	// fall clock change may already use another offset and shift earlier rows.
	var anchor *time.Time
	var fields map[string]json.RawMessage
	var times []json.RawMessage
	if json.Unmarshal(rawDaily, &fields) == nil && json.Unmarshal(fields["time"], &times) == nil {
		for _, rawTime := range times {
			if at := weatherUnixTime(rawTime); at != nil {
				anchor = at
				break
			}
		}
	}
	if anchor == nil {
		anchor = currentTime
	}
	if anchor == nil {
		fallback := time.Unix(0, 0)
		anchor = &fallback
	}
	_, offset := anchor.In(loc).Zone()
	return time.FixedZone("", offset), offset
}

func (r *Reading) projectDailySummary(date string) {
	r.TemperatureMinC, r.TemperatureMaxC, r.Sunset = nil, nil, ""
	for _, day := range r.Daily {
		if day.Date == date {
			r.TemperatureMinC, r.TemperatureMaxC, r.Sunset = day.MinC, day.MaxC, day.Sunset
			return
		}
	}
}

func weatherHours(raw json.RawMessage, loc *time.Location, currentTime *time.Time) []WeatherHour {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	var times, temperatures []json.RawMessage
	if json.Unmarshal(fields["time"], &times) != nil {
		return nil
	}
	_ = json.Unmarshal(fields["temperature_2m"], &temperatures)
	type hour struct {
		time time.Time
		temp *float64
	}
	var hours []hour
	for i, rawTime := range times {
		at := weatherUnixTime(rawTime)
		if at == nil {
			continue
		}
		var temp *float64
		if i < len(temperatures) {
			temp = weatherNumber(temperatures[i])
		}
		hours = append(hours, hour{time: *at, temp: temp})
	}
	sort.SliceStable(hours, func(i, j int) bool { return hours[i].time.Before(hours[j].time) })
	start := 0
	if currentTime != nil {
		for i := range hours {
			if !hours[i].time.After(*currentTime) {
				start = i
			}
		}
	}
	var result []WeatherHour
	for i := start; i < len(hours) && len(result) < 24; i++ {
		if hours[i].time.Sub(hours[start].time) >= 24*time.Hour {
			break
		}
		if i > start && hours[i].time.Equal(hours[i-1].time) {
			continue
		}
		result = append(result, WeatherHour{Time: hours[i].time.In(loc).Format(time.RFC3339), TemperatureC: hours[i].temp})
	}
	return result
}

func weatherDays(raw json.RawMessage, dateLoc, instantLoc *time.Location, currentTime *time.Time) []WeatherDay {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	var times, mins, maxs, codes, sunsets []json.RawMessage
	if json.Unmarshal(fields["time"], &times) != nil {
		return nil
	}
	_ = json.Unmarshal(fields["temperature_2m_min"], &mins)
	_ = json.Unmarshal(fields["temperature_2m_max"], &maxs)
	_ = json.Unmarshal(fields["weather_code"], &codes)
	_ = json.Unmarshal(fields["sunset"], &sunsets)
	var days []WeatherDay
	for i, rawTime := range times {
		at := weatherUnixTime(rawTime)
		if at == nil {
			continue
		}
		day := WeatherDay{Date: at.In(dateLoc).Format(time.DateOnly)}
		if currentTime != nil && day.Date < currentTime.In(instantLoc).Format(time.DateOnly) {
			continue
		}
		if i < len(mins) {
			day.MinC = weatherNumber(mins[i])
		}
		if i < len(maxs) {
			day.MaxC = weatherNumber(maxs[i])
		}
		if i < len(codes) {
			day.ConditionCode = weatherCode(codes[i])
		}
		if i < len(sunsets) {
			if sunset := weatherUnixTime(sunsets[i]); sunset != nil {
				day.Sunset = sunset.In(instantLoc).Format(time.RFC3339)
			}
		}
		days = append(days, day)
	}
	sort.SliceStable(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	var result []WeatherDay
	for _, day := range days {
		if len(result) > 0 && day.Date == result[len(result)-1].Date {
			continue
		}
		result = append(result, day)
		if len(result) == 5 {
			break
		}
	}
	return result
}

// WeatherService caches provider readings in the database and applies the
// refresh and staleness rules of design §6.7.
type WeatherService struct {
	cfg      config.Weather
	db       *store.DB
	provider Provider
	mu       sync.Mutex
}

// NewWeatherService builds the weather widget service. It returns nil when the
// widget is disabled, so callers can treat nil as "hidden".
func NewWeatherService(cfg config.Weather, db *store.DB, provider Provider) *WeatherService {
	if !cfg.Enabled {
		return nil
	}
	if provider == nil {
		if cfg.Provider == "qweather" {
			provider = &QWeather{Config: cfg.QWeather}
		} else {
			provider = &OpenMeteo{BaseURL: cfg.BaseURL}
		}
	}
	return &WeatherService{cfg: cfg, db: db, provider: provider}
}

// cachedPayload is what the service stores in widget_cache.
type cachedPayload struct {
	Identity         string  `json:"identity,omitempty"`
	Reading          Reading `json:"reading"`
	FetchedAt        string  `json:"fetched_at"`
	UTCOffsetSeconds *int    `json:"utc_offset_seconds,omitempty"`
}

// Current returns the widget payload, refreshing when the cache is due. A
// provider failure keeps the previous payload and only marks the error.
func (s *WeatherService) Current(ctx context.Context, now time.Time) (*Weather, error) {
	if s == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cached, err := s.db.WidgetCache().Get(ctx, domain.WidgetWeather)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	legacy := false
	if cached != nil {
		var payload cachedPayload
		if json.Unmarshal([]byte(cached.Payload), &payload) != nil {
			cached = nil
		} else if payload.Identity == "" {
			// Before identities existed only Open-Meteo could write this cache.
			// Always try to replace it first, and never relabel it QWeather.
			legacy = s.provider.Name() == "open-meteo" && (s.cfg.Provider == "open-meteo" || s.cfg.Provider == "")
			if !legacy {
				cached = nil
			}
		} else if payload.Identity != s.cacheIdentity() {
			cached = nil
		}
	}
	due := cached == nil || legacy || !now.Before(cached.ExpiresAt)
	if due {
		if fresh, ferr := s.refresh(ctx, now, false); ferr == nil {
			cached = fresh
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		} else if cached == nil {
			// Never fetched successfully: the widget stays hidden.
			return nil, nil
		}
	}
	if cached == nil {
		return nil, nil
	}
	var payload cachedPayload
	if err := json.Unmarshal([]byte(cached.Payload), &payload); err != nil {
		return nil, nil //nolint:nilerr // a corrupt cache entry simply hides the widget
	}
	if len(payload.Reading.Daily) > 0 {
		loc := time.UTC
		if zone, err := time.LoadLocation(payload.Reading.ForecastTimezone); err == nil && payload.Reading.ForecastTimezone != "" {
			loc = zone
		} else if offset := payload.UTCOffsetSeconds; offset != nil && *offset > -86400 && *offset < 86400 {
			loc = time.FixedZone("", *offset)
		}
		// A still-fresh cache may span local midnight. Project today's nullable
		// summary without another provider request or replaying yesterday's data.
		payload.Reading.projectDailySummary(now.In(loc).Format(time.DateOnly))
	}
	stale := now.Sub(cached.FetchedAt) > s.cfg.StaleAfter.D()
	if observed, err := time.Parse(time.RFC3339, payload.Reading.ObservedAt); err == nil && now.Sub(observed) > s.cfg.StaleAfter.D() {
		stale = true
	}
	return &Weather{
		Provider: s.provider.Name(), LocationLabel: s.cfg.LocationLabel,
		TemperatureC: payload.Reading.TemperatureC, ConditionCode: payload.Reading.ConditionCode,
		ConditionText: payload.Reading.ConditionText,
		HumidityPct:   payload.Reading.HumidityPct, WindSpeedKMH: payload.Reading.WindSpeedKMH,
		WindDirectionDeg: payload.Reading.WindDirectionDeg,
		TemperatureMinC:  payload.Reading.TemperatureMinC, TemperatureMaxC: payload.Reading.TemperatureMaxC,
		Sunset: payload.Reading.Sunset, ForecastTimezone: payload.Reading.ForecastTimezone,
		Hourly: payload.Reading.Hourly, Daily: payload.Reading.Daily,
		ObservedAt: payload.Reading.ObservedAt, Attributions: payload.Reading.Attributions,
		FetchedAt: cached.FetchedAt.UTC().Format(time.RFC3339),
		Stale:     stale,
	}, nil
}

// Refresh fetches and stores a reading regardless of the cache expiry.
func (s *WeatherService) Refresh(ctx context.Context, now time.Time) error {
	if s == nil {
		return nil
	}
	_, err := s.refresh(ctx, now, true)
	return err
}

func (s *WeatherService) refresh(ctx context.Context, now time.Time, force bool) (*store.CachedWidget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !force {
		// Another Home request may have refreshed while this one waited.
		if cached, err := s.db.WidgetCache().Get(ctx, domain.WidgetWeather); err == nil && now.Before(cached.ExpiresAt) {
			var payload cachedPayload
			if json.Unmarshal([]byte(cached.Payload), &payload) == nil && payload.Identity == s.cacheIdentity() {
				return cached, nil
			}
		}
	}
	reading, err := s.provider.Fetch(ctx, s.cfg.Latitude, s.cfg.Longitude)
	if err != nil {
		_ = s.db.WidgetCache().SetError(ctx, domain.WidgetWeather, "provider_unreachable")
		return nil, err
	}
	if reading == nil {
		return nil, errors.New("weather: empty provider reading")
	}
	blob, err := json.Marshal(cachedPayload{Identity: s.cacheIdentity(), Reading: *reading, FetchedAt: now.UTC().Format(time.RFC3339), UTCOffsetSeconds: reading.forecastUTCOffsetSeconds})
	if err != nil {
		return nil, fmt.Errorf("weather: encode payload: %w", err)
	}
	entry := store.CachedWidget{
		Widget:    domain.WidgetWeather,
		Payload:   string(blob),
		FetchedAt: now,
		ExpiresAt: now.Add(s.cfg.RefreshInterval.D()),
	}
	if err := s.db.WidgetCache().Put(ctx, entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// Only the actual request's effective coordinate precision participates in the
// identity. This hash contains no credentials and is never exposed to screens.
func (s *WeatherService) cacheIdentity() string {
	precision := 4
	host, timezone := s.cfg.BaseURL, ""
	if s.provider.Name() == "qweather" {
		precision = 2
		host, timezone = strings.ToLower(s.cfg.QWeather.APIHost), s.cfg.QWeather.Timezone
	}
	parts := []string{s.provider.Name(), strconv.FormatFloat(s.cfg.Latitude, 'f', precision, 64), strconv.FormatFloat(s.cfg.Longitude, 'f', precision, 64), host, timezone}
	raw, _ := json.Marshal(parts)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// RefreshInterval exposes the configured cadence for the background refresher.
func (s *WeatherService) RefreshInterval() time.Duration {
	if s == nil {
		return 0
	}
	return s.cfg.RefreshInterval.D()
}
