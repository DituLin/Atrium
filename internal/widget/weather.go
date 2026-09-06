package widget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
)

// Reading is one provider observation.
type Reading struct {
	TemperatureC  float64 `json:"temperature_c"`
	ConditionCode int     `json:"condition_code"`
	ConditionText string  `json:"condition_text"`
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
	q.Set("current", "temperature_2m,weather_code")
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
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			WeatherCode int     `json:"weather_code"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("weather: decode response: %w", err)
	}
	return &Reading{
		TemperatureC:  body.Current.Temperature,
		ConditionCode: body.Current.WeatherCode,
		ConditionText: ConditionText(body.Current.WeatherCode),
	}, nil
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
		provider = &OpenMeteo{BaseURL: cfg.BaseURL}
	}
	return &WeatherService{cfg: cfg, db: db, provider: provider}
}

// cachedPayload is what the service stores in widget_cache.
type cachedPayload struct {
	Reading   Reading `json:"reading"`
	FetchedAt string  `json:"fetched_at"`
}

// Current returns the widget payload, refreshing when the cache is due. A
// provider failure keeps the previous payload and only marks the error.
func (s *WeatherService) Current(ctx context.Context, now time.Time) (*Weather, error) {
	if s == nil {
		return nil, nil
	}
	cached, err := s.db.WidgetCache().Get(ctx, domain.WidgetWeather)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	due := cached == nil || !now.Before(cached.ExpiresAt)
	if due {
		if fresh, ferr := s.refresh(ctx, now); ferr == nil {
			cached = fresh
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
	return &Weather{
		Provider:      s.provider.Name(),
		LocationLabel: s.cfg.LocationLabel,
		TemperatureC:  payload.Reading.TemperatureC,
		ConditionCode: payload.Reading.ConditionCode,
		ConditionText: payload.Reading.ConditionText,
		FetchedAt:     cached.FetchedAt.UTC().Format(time.RFC3339),
		Stale:         now.Sub(cached.FetchedAt) > s.cfg.StaleAfter.D(),
	}, nil
}

// Refresh fetches and stores a reading regardless of the cache expiry.
func (s *WeatherService) Refresh(ctx context.Context, now time.Time) error {
	if s == nil {
		return nil
	}
	_, err := s.refresh(ctx, now)
	return err
}

func (s *WeatherService) refresh(ctx context.Context, now time.Time) (*store.CachedWidget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reading, err := s.provider.Fetch(ctx, s.cfg.Latitude, s.cfg.Longitude)
	if err != nil {
		_ = s.db.WidgetCache().SetError(ctx, domain.WidgetWeather, "provider_unreachable")
		return nil, err
	}
	blob, err := json.Marshal(cachedPayload{Reading: *reading, FetchedAt: now.UTC().Format(time.RFC3339)})
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

// RefreshInterval exposes the configured cadence for the background refresher.
func (s *WeatherService) RefreshInterval() time.Duration {
	if s == nil {
		return 0
	}
	return s.cfg.RefreshInterval.D()
}
