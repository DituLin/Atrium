package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/widget"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func overviewConfig(c *config.Config) {
	src := config.SourceDefaults()
	src.ID, src.Name, src.Root = "family", "Family photos", "/private/nas/family"
	c.Sources = []config.Source{src}
}

func TestOverviewPublicProjectionAndAuthorization(t *testing.T) {
	h := newHarness(t, overviewConfig)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "house_tv", "House TV")
	_, err := widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
	require.NoError(t, err)
	require.NoError(t, h.db.Sources().SetHealth(t.Context(), "family", domain.HealthDegraded, "private mount detail", true, h.clock.Now()))
	require.NoError(t, h.db.Sources().SetShareStats(t.Context(), "family", 123, 456, h.clock.Now()))

	var first map[string]any
	for _, token := range []string{screen, admin} {
		response := h.request(http.MethodGet, "/api/v1/family/overview", "", bearer(token))
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
		body := decode[map[string]any](t, response)
		if first == nil {
			first = body
		} else {
			require.Equal(t, first, body, "admin receives the same public projection")
		}
		require.Equal(t, float64(1), body["schema_version"])
		require.Equal(t, map[string]any{"name": "Test home", "timezone": "Asia/Singapore"}, body["home"])
		core := body["sources"].(map[string]any)["core"].(map[string]any)
		require.Equal(t, "available", core["availability"])
		require.Equal(t, body["generated_at"], core["observed_at"])
		at, err := time.Parse(time.RFC3339, core["observed_at"].(string))
		require.NoError(t, err)
		expires, err := time.Parse(time.RFC3339, core["expires_at"].(string))
		require.NoError(t, err)
		require.Equal(t, time.Minute, expires.Sub(at))
		require.Equal(t, []any{map[string]any{"id": "response", "responding": true, "updated_at": nil, "valid_from": nil, "valid_until": nil}}, core["items"])
		for _, name := range []string{"profile", "environment", "calendar", "notice"} {
			snapshot := body["sources"].(map[string]any)[name].(map[string]any)
			require.Equal(t, "not_connected", snapshot["availability"])
			require.Equal(t, []any{}, snapshot["items"])
			require.Nil(t, snapshot["observed_at"])
			require.Nil(t, snapshot["expires_at"])
		}
		require.Equal(t, "not_provided", body["sources"].(map[string]any)["profile"].(map[string]any)["reason"])
		require.Equal(t, "not_supported", body["sources"].(map[string]any)["environment"].(map[string]any)["reason"])
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		for _, private := range []string{"/private/", "health_detail", "private mount", "identity", "share_free_bytes", "share_total_bytes", "share_stats_at", "root_path", "stuck_ops", "screens"} {
			require.NotContains(t, string(raw), private)
		}
	}
	require.Equal(t, http.StatusUnauthorized, h.request(http.MethodGet, "/api/v1/family/overview", "").StatusCode)
	_, token, err := auth.NewIntegrationService(h.db, h.clock.Now).Issue(t.Context(), "test", domain.IntegrationPolicy{Permissions: []string{"home.read", "nas.read"}, SourceIDs: []string{"family"}}, h.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, h.request(http.MethodGet, "/api/v1/family/overview", "", bearer(token)).StatusCode)
	require.NoError(t, h.db.Screens().Revoke(t.Context(), "house_tv", h.clock.Now()))
	require.Equal(t, http.StatusGone, h.request(http.MethodGet, "/api/v1/family/overview", "", bearer(screen)).StatusCode)
}

func TestOverviewSurvivesPhotoQueriesAndSourceAuthorizationReadFailure(t *testing.T) {
	h := newHarness(t, overviewLoadedConfig(t))
	screen := h.pairScreen(h.newAdminToken(), "house_tv", "House TV")
	_, err := widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
	require.NoError(t, err)
	require.NoError(t, h.db.Sources().SetHealth(t.Context(), "family", domain.HealthOffline, "private detail", false, h.clock.Now()))
	_, err = h.db.SQL().ExecContext(t.Context(), "DROP TABLE photos")
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, h.request(http.MethodGet, "/api/v1/home", "", bearer(screen)).StatusCode)
	response := h.request(http.MethodGet, "/api/v1/family/overview", "", bearer(screen))
	require.Equal(t, http.StatusOK, response.StatusCode)
	body := decode[map[string]any](t, response)
	nas := body["sources"].(map[string]any)["nas"].([]any)[0].(map[string]any)
	require.Equal(t, "available", nas["availability"])
	require.Equal(t, "offline", nas["items"].([]any)[0].(map[string]any)["health"])
	_, err = h.db.SQL().ExecContext(t.Context(), "ALTER TABLE data_sources RENAME TO unavailable_sources")
	require.NoError(t, err)
	response = h.request(http.MethodGet, "/api/v1/family/overview", "", bearer(screen))
	require.Equal(t, http.StatusOK, response.StatusCode)
	body = decode[map[string]any](t, response)
	require.Equal(t, "available", body["sources"].(map[string]any)["core"].(map[string]any)["availability"])
	require.Len(t, body["sources"].(map[string]any)["notice"].(map[string]any)["items"], 1)
	require.Len(t, body["entries"], 2)
	nas = body["sources"].(map[string]any)["nas"].([]any)[0].(map[string]any)
	require.Equal(t, map[string]any{"source_id": "family", "source_label": "Family photos", "observed_at": nil, "expires_at": nil, "availability": "failed", "reason": "read_failed", "items": []any{}}, nas)
}

func TestOverviewResponseMatchesOpenAPI(t *testing.T) {
	raw, err := os.ReadFile("../../docs/api/openapi.yaml")
	require.NoError(t, err)
	var doc struct {
		Paths      map[string]map[string]any `yaml:"paths"`
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Paths["/api/v1/family/overview"]["get"])
	require.NotEmpty(t, doc.Components.Schemas["OverviewResponse"])
	definitions := map[string]any{}
	for name, schema := range doc.Components.Schemas {
		if strings.HasPrefix(name, "House") || strings.HasPrefix(name, "Family") || strings.HasPrefix(name, "Overview") {
			definitions[name] = schema
		}
	}
	schemaJSON, err := json.Marshal(map[string]any{"$ref": "#/$defs/OverviewResponse", "$defs": definitions})
	require.NoError(t, err)
	schemaJSON = bytes.ReplaceAll(schemaJSON, []byte("#/components/schemas/"), []byte("#/$defs/"))
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	h := newHarness(t, overviewLoadedConfig(t))
	token := h.pairScreen(h.newAdminToken(), "house_tv", "House TV")
	validate := func() map[string]any {
		t.Helper()
		response := h.request(http.MethodGet, "/api/v1/family/overview", "", bearer(token))
		require.Equal(t, http.StatusOK, response.StatusCode)
		body := decode[map[string]any](t, response)
		require.NoError(t, resolved.Validate(body))
		return body
	}
	validate() // successful empty source collection
	_, err = widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
	require.NoError(t, err)
	validate() // not observed
	require.NoError(t, h.db.Sources().SetHealth(t.Context(), "family", domain.HealthOnline, "", true, h.clock.Now()))
	body := validate()
	body["sources"].(map[string]any)["nas"].([]any)[0].(map[string]any)["root_path"] = "/private/source"
	require.Error(t, resolved.Validate(body), "public schema excludes maintenance fields")
	h.clock.Advance(time.Minute)
	validate() // expired, with prior observed content
	_, err = h.db.SQL().ExecContext(t.Context(), "ALTER TABLE data_sources RENAME TO unavailable_sources")
	require.NoError(t, err)
	validate() // failed authorization read with explicit null times and [] items
}

func overviewLoadedConfig(t *testing.T) func(*config.Config) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte("home:\n  timezone: Asia/Singapore\n  name: Test home\nserver:\n  public_url: "+testOrigin+"\nwidgets:\n  notice:\n    enabled: true\n    text: Configured notice\n    valid_from: '2026-09-05T20:00:00+08:00'\n    valid_until: '2026-09-05T12:01:00Z'\n"), 0600))
	loaded, err := config.Load(p)
	require.NoError(t, err)
	return func(c *config.Config) { *c = *loaded; overviewConfig(c) }
}
func TestOverviewNoticeBoundariesAndSourceRevocation(t *testing.T) {
	h := newHarness(t, overviewLoadedConfig(t))
	token := h.pairScreen(h.newAdminToken(), "overview_tv", "Overview TV")
	_, err := widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
	require.NoError(t, err)
	require.NoError(t, h.db.Sources().SetHealth(t.Context(), "family", domain.HealthOffline, "", false, h.clock.Now()))
	read := func() map[string]any {
		return decode[map[string]any](t, h.request(http.MethodGet, "/api/v1/family/overview", "", bearer(token)))
	}
	first := read()
	sources := first["sources"].(map[string]any)
	notice := sources["notice"].(map[string]any)
	require.Equal(t, "available", notice["availability"])
	require.Nil(t, notice["expires_at"])
	require.Nil(t, notice["reason"])
	require.Equal(t, []any{map[string]any{"id": "notice", "text": "Configured notice", "updated_at": nil, "valid_from": "2026-09-05T20:00:00+08:00", "valid_until": "2026-09-05T12:01:00Z"}}, notice["items"])
	require.Len(t, first["entries"], 2)
	require.NoError(t, h.db.Sources().Revoke(t.Context(), "family", "admin_revoked", h.clock.Now()))
	revoked := read()
	require.Equal(t, []any{}, revoked["sources"].(map[string]any)["nas"])
	require.Len(t, revoked["entries"], 1)
	h.clock.Advance(time.Minute)
	expired := read()
	after := expired["sources"].(map[string]any)["notice"].(map[string]any)
	require.Equal(t, "available", after["availability"])
	require.Equal(t, notice["observed_at"], after["observed_at"])
	require.Equal(t, []any{}, after["items"])
	require.Equal(t, []any{}, expired["entries"])
}
