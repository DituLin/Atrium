package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/clock"
	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/httpapi"
	"github.com/DituLin/Atritum/internal/logging"
	"github.com/DituLin/Atritum/internal/store"
	"github.com/DituLin/Atritum/internal/store/testutil"
	"github.com/DituLin/Atritum/internal/widget"
)

// harness is a running API backed by a temporary database and a fake clock.
type harness struct {
	t        *testing.T
	db       *store.DB
	server   *httptest.Server
	clock    *clock.Fake
	cfg      *config.Config
	composer *widget.Composer
	// deps mirrors what the API was built with, so tests can reach the cache
	// or the queue the handlers use.
	deps httpapi.Deps
}

const testOrigin = "https://192.168.1.10:8443"

func newHarness(t *testing.T, mutate ...func(*config.Config)) *harness {
	t.Helper()
	return newHarnessWith(t, nil, mutate...)
}

// newHarnessWith builds the API and lets a test attach the V0.2 collaborators
// (source manager, queue, cache, indexer) before the routes are wired.
func newHarnessWith(t *testing.T, wire func(*harness, *httpapi.Deps), mutate ...func(*config.Config)) *harness {
	t.Helper()
	db := testutil.NewDB(t)
	fake := clock.NewFake(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))

	cfg := config.Defaults()
	cfg.Home.Timezone = "Asia/Singapore"
	cfg.Home.Name = "Test home"
	cfg.Server.PublicURL = testOrigin
	cfg.Server.Listen = "0.0.0.0:8443"
	for _, m := range mutate {
		m(cfg)
	}
	require.NoError(t, cfg.Validate())

	home, err := clock.NewHome(cfg.Home.Timezone, fake)
	require.NoError(t, err)
	now := func() time.Time { return fake.Now() }

	weather := widget.NewWeatherService(cfg.Widgets.Weather, db, nil)
	composer := widget.NewComposer(cfg, db, home, weather)
	h := &harness{t: t, db: db, clock: fake, cfg: cfg, composer: composer}
	deps := httpapi.Deps{
		Config:    cfg,
		DB:        db,
		Logger:    logging.Discard(),
		Auth:      auth.NewAuthenticator(db, auth.NewOriginPolicy(cfg.DefaultAllowedOrigins()), now),
		Pairing:   auth.NewPairingService(db, now),
		Home:      home,
		Snapshot:  composer,
		Now:       now,
		Insecure:  cfg.Server.TLS.Mode == config.TLSOff,
		StartedAt: fake.Now(),
	}
	if wire != nil {
		wire(h, &deps)
	}
	h.deps = deps
	srv := httptest.NewServer(httpapi.New(deps).Handler())
	t.Cleanup(srv.Close)
	h.server = srv
	return h
}

// request issues a request against the harness and returns the response.
func (h *harness) request(method, path string, body string, mutate ...func(*http.Request)) *http.Response {
	h.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, reader)
	require.NoError(h.t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mutate {
		m(req)
	}
	resp, err := h.server.Client().Do(req)
	require.NoError(h.t, err)
	h.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// decode reads a JSON body into v.
func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &v), "body: %s", string(raw))
	return v
}

// errorCode reads the stable code from an error response.
func errorCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	body := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, resp)
	return body.Error.Code
}

func bearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func cookie(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: auth.ScreenCookieName, Value: token})
	}
}

func origin(o string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Origin", o) }
}

// newAdminToken issues an admin credential directly in the store.
func (h *harness) newAdminToken() string {
	h.t.Helper()
	token, _, err := auth.NewAdminService(h.db).Issue(context.Background(), "test", h.clock.Now())
	require.NoError(h.t, err)
	return token
}

// pairScreen runs the full pairing round trip and returns the screen token.
func (h *harness) pairScreen(adminToken, screenID, name string) string {
	h.t.Helper()
	start := h.request(http.MethodPost, "/api/v1/pair/start", `{"client_hint":"test tv"}`)
	require.Equal(h.t, http.StatusCreated, start.StatusCode)
	started := decode[map[string]any](h.t, start)
	pairingID := started["pairing_id"].(string)

	approve := h.request(http.MethodPost, "/api/v1/pairings/"+pairingID+"/approve",
		`{"screen_id":"`+screenID+`","name":"`+name+`"}`, bearer(adminToken))
	require.Equal(h.t, http.StatusOK, approve.StatusCode)

	claim := h.request(http.MethodPost, "/api/v1/pair/"+pairingID+"/claim", "")
	require.Equal(h.t, http.StatusOK, claim.StatusCode)
	claimed := decode[map[string]any](h.t, claim)
	return claimed["token"].(string)
}

// decodeRaw returns the raw response body of an authenticated GET.
func decodeRaw(t *testing.T, h *harness, path, token string) string {
	t.Helper()
	resp := h.request(http.MethodGet, path, "", bearer(token))
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(raw)
}
