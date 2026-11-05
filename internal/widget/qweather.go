package widget

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sys/unix"

	"github.com/DituLin/Atrium/internal/config"
)

// QWeather adapts the coordinate Weather v1 API to the provider-neutral widget.
// Credentials stay on Core. BaseURL and Client permit isolated HTTP fixtures;
// production constructs this provider with its dedicated HTTPS APIHost.
type QWeather struct {
	Config  config.QWeather
	Client  *http.Client
	BaseURL string
	Now     func() time.Time
	mu      sync.Mutex
	blocked map[string]time.Time
}

func (q *QWeather) Name() string { return "qweather" }

func (q *QWeather) now() time.Time {
	if q.Now != nil {
		return q.Now()
	}
	return time.Now()
}

// Fetch requires valid current conditions. Optional forecasts have a short,
// shared budget and cannot erase a valid current response when they time out.
func (q *QWeather) Fetch(ctx context.Context, lat, lon float64) (*Reading, error) {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 || math.IsNaN(lon) || math.IsInf(lon, 0) || lon < -180 || lon > 180 {
		return nil, errors.New("weather: invalid coordinates")
	}
	loc, err := time.LoadLocation(q.Config.Timezone)
	if err != nil || q.Config.Timezone == "" {
		return nil, errors.New("weather: invalid forecast timezone")
	}
	headers, err := q.authHeaders()
	if err != nil {
		return nil, err
	}
	coordinates := strconv.FormatFloat(lat, 'f', 2, 64) + "/" + strconv.FormatFloat(lon, 'f', 2, 64)
	current, err := q.request(ctx, "current", coordinates, nil, headers)
	if err != nil {
		return nil, err
	}
	temp := qweatherQuantity(current["temperature"], "°C", 1, -273.15, 100)
	var condition struct {
		Code string `json:"code"`
		Text string `json:"text"`
	}
	if temp == nil || json.Unmarshal(current["condition"], &condition) != nil || !qweatherConditionCode.MatchString(condition.Code) {
		return nil, errors.New("weather: invalid current conditions")
	}
	r := &Reading{TemperatureC: *temp, ConditionCode: qweatherWMO(condition.Code), ConditionText: qweatherText(condition.Text, 128), ForecastTimezone: q.Config.Timezone}
	if r.ConditionText == "" {
		r.ConditionText = ConditionText(r.ConditionCode)
	}
	if humidity := weatherNumberInRange(current["humidity"], 0, 1); humidity != nil {
		value := *humidity * 100
		r.HumidityPct = &value
	}
	var wind map[string]json.RawMessage
	if json.Unmarshal(current["wind"], &wind) == nil {
		r.WindSpeedKMH = qweatherQuantity(wind["speed"], "m/s", 3.6, 0, 200)
		var direction map[string]json.RawMessage
		if json.Unmarshal(wind["direction"], &direction) == nil {
			r.WindDirectionDeg = weatherNumberInRange(direction["degree"], 0, 360)
		}
	}
	// Current v1 does not currently promise an observation timestamp. Preserve
	// one only when explicitly supplied; receipt time is never substituted.
	for _, field := range []string{"observationTime", "obsTime"} {
		var value string
		if json.Unmarshal(current[field], &value) == nil {
			if at, ok := qweatherTime(value); ok && !at.After(q.now().Add(5*time.Minute)) {
				r.ObservedAt = at.Format(time.RFC3339)
				break
			}
		}
	}
	r.Attributions = qweatherAttributions(current["metadata"])
	budget := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		// Home has its own request deadline. Leave room to persist the valid
		// current response and serialize it even if forecasts are unresponsive.
		budget = min(budget, max(0, time.Until(deadline)-150*time.Millisecond))
	}
	forecastCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	type result struct {
		name string
		body map[string]json.RawMessage
	}
	results := make(chan result, 2)
	for _, item := range []struct{ name, key, value string }{{"hourly", "hours", "24"}, {"daily", "days", "5"}} {
		go func(name, key, value string) {
			body, _ := q.request(forecastCtx, name, coordinates, url.Values{key: {value}}, headers)
			results <- result{name, body}
		}(item.name, item.key, item.value)
	}
	for range 2 {
		item := <-results
		if item.body == nil {
			continue
		}
		r.Attributions = qweatherMergeAttributions(r.Attributions, qweatherAttributions(item.body["metadata"]))
		if item.name == "hourly" {
			r.Hourly = qweatherHours(item.body["hours"])
		} else {
			r.Daily = qweatherDays(item.body["days"], loc)
		}
	}
	date := q.now().In(loc).Format(time.DateOnly)
	r.projectDailySummary(date)
	return r, nil
}

var qweatherConditionCode = regexp.MustCompile(`^[0-9]{3}$`)

func (q *QWeather) request(ctx context.Context, kind, coordinates string, params url.Values, headers http.Header) (map[string]json.RawMessage, error) {
	now := q.now()
	q.mu.Lock()
	until := q.blocked[kind]
	q.mu.Unlock()
	if now.Before(until) {
		return nil, errors.New("weather: provider retry deferred")
	}
	base := "https://" + q.Config.APIHost
	if q.BaseURL != "" && q.Client != nil {
		base = strings.TrimRight(q.BaseURL, "/")
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(q.Client != nil && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return nil, errors.New("weather: invalid provider endpoint")
	}
	u.Path = "/weather/v1/" + kind + "/" + coordinates
	if params == nil {
		params = url.Values{}
	}
	params.Set("localTime", "true")
	params.Set("lang", "zh")
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("weather: cannot build provider request")
	}
	req.Header = headers.Clone()
	client := http.Client{Timeout: 4 * time.Second}
	if q.Client != nil {
		client = *q.Client
		if client.Timeout == 0 || client.Timeout > 4*time.Second {
			client.Timeout = 4 * time.Second
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("weather: provider request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 429 {
			delay := 15 * time.Minute
			if resp.StatusCode == 429 {
				delay = time.Minute
				if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
					delay = time.Duration(min(seconds, 21600)) * time.Second
				} else if at, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
					delay = at.Sub(now)
				}
				if delay < time.Minute {
					delay = time.Minute
				}
				if delay > 6*time.Hour {
					delay = 6 * time.Hour
				}
			}
			q.mu.Lock()
			if q.blocked == nil {
				q.blocked = map[string]time.Time{}
			}
			q.blocked[kind] = now.Add(delay)
			q.mu.Unlock()
		}
		return nil, fmt.Errorf("weather: provider returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("weather: invalid provider response")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil || body["error"] != nil {
		return nil, errors.New("weather: invalid provider response")
	}
	return body, nil
}

func (q *QWeather) authHeaders() (http.Header, error) {
	headers := http.Header{}
	if q.Config.APIKeyFile != "" {
		if q.Config.JWT != (config.QWeatherJWT{}) {
			return nil, errors.New("weather: ambiguous credentials")
		}
		raw, err := qweatherSecret(q.Config.APIKeyFile, 4097)
		if err != nil {
			return nil, err
		}
		key := strings.TrimSuffix(string(raw), "\n")
		if len(key) < 8 || len(key) > 4096 || !regexp.MustCompile(`^[A-Za-z0-9._~+/-]+$`).MatchString(key) {
			return nil, errors.New("weather: invalid API key file")
		}
		headers.Set("X-QW-Api-Key", key)
		return headers, nil
	}
	cfg := q.Config.JWT
	if cfg.DeveloperID == "" || cfg.ProjectID == "" || cfg.CredentialID == "" || cfg.PrivateKeyFile == "" {
		return nil, errors.New("weather: missing credentials")
	}
	raw, err := qweatherSecret(cfg.PrivateKeyFile, 16384)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("weather: invalid signing key file")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("weather: invalid signing key file")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("weather: signing key must be Ed25519")
	}
	encode := func(value any) string { b, _ := json.Marshal(value); return base64.RawURLEncoding.EncodeToString(b) }
	now := q.now()
	unsigned := encode(map[string]string{"alg": "EdDSA", "kid": cfg.CredentialID}) + "." + encode(map[string]any{"iss": cfg.DeveloperID, "sub": cfg.ProjectID, "iat": now.Add(-30 * time.Second).Unix(), "exp": now.Add(10 * time.Minute).Unix()})
	token := unsigned + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(unsigned)))
	headers.Set("Authorization", "Bearer "+token)
	return headers, nil
}

func qweatherSecret(path string, maxBytes int) ([]byte, error) {
	fail := func() ([]byte, error) {
		return nil, errors.New("weather: credential file must be an owned 0600 regular file")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return fail()
	}
	f := os.NewFile(uintptr(fd), "weather-credential")
	defer func() { _ = f.Close() }()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || int(st.Uid) != os.Geteuid() || st.Nlink != 1 || st.Size <= 0 || st.Size > int64(maxBytes) {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(maxBytes+1)))
	if err != nil || len(raw) > maxBytes {
		return fail()
	}
	return raw, nil
}

func qweatherQuantity(raw json.RawMessage, unit string, factor, minValue, maxValue float64) *float64 {
	var quantity map[string]json.RawMessage
	if json.Unmarshal(raw, &quantity) != nil {
		return nil
	}
	var got string
	if json.Unmarshal(quantity["unit"], &got) != nil || got != unit {
		return nil
	}
	value := weatherNumberInRange(quantity["value"], minValue, maxValue)
	if value == nil {
		return nil
	}
	converted := *value * factor
	return &converted
}

func qweatherTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04Z07:00"} {
		if at, err := time.Parse(layout, value); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

func qweatherHours(raw json.RawMessage) []WeatherHour {
	var entries []map[string]json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var hours []WeatherHour
	for _, entry := range entries {
		var value string
		if json.Unmarshal(entry["forecastTime"], &value) != nil {
			continue
		}
		at, ok := qweatherTime(value)
		if !ok {
			continue
		}
		hours = append(hours, WeatherHour{Time: at.Format(time.RFC3339), TemperatureC: qweatherQuantity(entry["temperature"], "°C", 1, -273.15, 100)})
	}
	sort.SliceStable(hours, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339, hours[i].Time)
		b, _ := time.Parse(time.RFC3339, hours[j].Time)
		return a.Before(b)
	})
	var result []WeatherHour
	seen := map[int64]bool{}
	for _, hour := range hours {
		at, _ := time.Parse(time.RFC3339, hour.Time)
		if seen[at.Unix()] {
			continue
		}
		seen[at.Unix()] = true
		result = append(result, hour)
		if len(result) == 24 {
			break
		}
	}
	return result
}

func qweatherDays(raw json.RawMessage, loc *time.Location) []WeatherDay {
	var entries []map[string]json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var days []WeatherDay
	for _, entry := range entries {
		var value string
		if json.Unmarshal(entry["forecastStartTime"], &value) != nil {
			continue
		}
		at, ok := qweatherTime(value)
		if !ok {
			continue
		}
		day := WeatherDay{Date: at.In(loc).Format(time.DateOnly), MinC: qweatherQuantity(entry["temperatureMin"], "°C", 1, -273.15, 100), MaxC: qweatherQuantity(entry["temperatureMax"], "°C", 1, -273.15, 100)}
		if day.MinC != nil && day.MaxC != nil && *day.MinC > *day.MaxC {
			day.MinC, day.MaxC = nil, nil
		}
		var daytime struct {
			Condition struct {
				Code string `json:"code"`
			} `json:"condition"`
		}
		if json.Unmarshal(entry["daytime"], &daytime) == nil && qweatherConditionCode.MatchString(daytime.Condition.Code) {
			code := qweatherWMO(daytime.Condition.Code)
			day.ConditionCode = &code
		}
		var astro struct {
			Sunset string `json:"sunset"`
		}
		if json.Unmarshal(entry["astro"], &astro) == nil {
			if at, ok := qweatherTime(astro.Sunset); ok {
				day.Sunset = at.In(loc).Format(time.RFC3339)
			}
		}
		days = append(days, day)
	}
	sort.SliceStable(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	var result []WeatherDay
	seen := map[string]bool{}
	for _, day := range days {
		if seen[day.Date] {
			continue
		}
		seen[day.Date] = true
		result = append(result, day)
		if len(result) == 5 {
			break
		}
	}
	return result
}

func qweatherText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) > maxRunes {
		return ""
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return text
}

func qweatherAttributions(raw json.RawMessage) []string {
	var metadata struct {
		Attributions []string `json:"attributions"`
	}
	_ = json.Unmarshal(raw, &metadata)
	return qweatherMergeAttributions([]string{"https://www.qweather.com", "https://developer.qweather.com/attribution.html"}, metadata.Attributions)
}

func qweatherMergeAttributions(groups ...[]string) []string {
	var result []string
	seen := map[string]bool{}
	for _, group := range groups {
		for _, raw := range group {
			value := qweatherText(raw, 2048)
			if value == "" || seen[value] {
				continue
			}
			if regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:[^\s]`).MatchString(value) {
				u, err := url.Parse(value)
				if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
					continue
				}
			}
			seen[value] = true
			result = append(result, value)
			if len(result) == 16 {
				return result
			}
		}
	}
	return result
}

// Provider-specific codes never enter the WMO wire field. Unknown/new codes
// retain their supplied description and use the unknown icon rather than sun.
func qweatherWMO(code string) int {
	switch code {
	case "100", "150":
		return 0
	case "102":
		return 1
	case "101", "103", "151", "152", "153":
		return 2
	case "104":
		return 3
	case "300", "301", "350", "351":
		return 80
	case "302", "303":
		return 95
	case "304":
		return 99
	case "309":
		return 51
	case "313":
		return 66
	case "305", "314":
		return 61
	case "306", "315":
		return 63
	case "307", "308", "310", "311", "312", "316", "317", "318", "399":
		return 65
	case "400", "408":
		return 71
	case "401", "409":
		return 73
	case "402", "403", "410", "499":
		return 75
	case "404", "405", "406", "407", "456", "457":
		return 85
	case "500", "501", "502", "509", "510", "511", "512", "513", "514", "515":
		return 45
	default:
		return 999
	}
}
