package httpapi_test

import (
	"bytes"
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"gopkg.in/yaml.v3"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/widget"
	"github.com/stretchr/testify/require"
)

func houseConfig(c *config.Config) {
	src := config.SourceDefaults()
	src.ID, src.Name, src.Root = "family", "Family photos", "/private/nas/family"
	c.Sources = []config.Source{src}
}

func TestHousePublicProjectionAndAuthorization(t *testing.T) {
	h := newHarness(t, houseConfig)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "house_tv", "House TV")
	_, err := widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
	require.NoError(t, err)
	require.NoError(t, h.db.Sources().SetHealth(t.Context(), "family", domain.HealthDegraded, "private mount detail", true, h.clock.Now()))
	require.NoError(t, h.db.Sources().SetShareStats(t.Context(), "family", 123, 456, h.clock.Now()))

	var first map[string]any
	for _, token := range []string{screen, admin} {
		response := h.request(http.MethodGet, "/api/v1/family/house", "", bearer(token))
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
		core := body["core"].(map[string]any)
		require.Equal(t, "available", core["availability"])
		require.Equal(t, body["generated_at"], core["observed_at"])
		at, err := time.Parse(time.RFC3339, core["observed_at"].(string))
		require.NoError(t, err)
		expires, err := time.Parse(time.RFC3339, core["expires_at"].(string))
		require.NoError(t, err)
		require.Equal(t, time.Minute, expires.Sub(at))
		require.Equal(t, []any{map[string]any{"id": "response", "responding": true, "updated_at": nil, "valid_from": nil, "valid_until": nil}}, core["items"])
		for _, name := range []string{"profile", "environment"} {
			snapshot := body[name].(map[string]any)
			require.Equal(t, "not_connected", snapshot["availability"])
			require.Equal(t, []any{}, snapshot["items"])
			require.Nil(t, snapshot["observed_at"])
			require.Nil(t, snapshot["expires_at"])
		}
		require.Equal(t, "not_provided", body["profile"].(map[string]any)["reason"])
		require.Equal(t, "not_supported", body["environment"].(map[string]any)["reason"])
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		for _, private := range []string{"/private/", "health_detail", "private mount", "identity", "share_free_bytes", "share_total_bytes", "share_stats_at", "root_path", "stuck_ops", "screens"} {
			require.NotContains(t, string(raw), private)
		}
	}
	require.Equal(t, http.StatusUnauthorized, h.request(http.MethodGet, "/api/v1/family/house", "").StatusCode)
	_, token, err := auth.NewIntegrationService(h.db, h.clock.Now).Issue(t.Context(), "test", domain.IntegrationPolicy{Permissions: []string{"home.read", "nas.read"}, SourceIDs: []string{"family"}}, h.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, h.request(http.MethodGet, "/api/v1/family/house", "", bearer(token)).StatusCode)
	require.NoError(t, h.db.Screens().Revoke(t.Context(), "house_tv", h.clock.Now()))
	require.Equal(t, http.StatusGone, h.request(http.MethodGet, "/api/v1/family/house", "", bearer(screen)).StatusCode)
}

func TestHouseSurvivesPhotoQueriesAndSourceAuthorizationReadFailure(t *testing.T) {
	h := newHarness(t, houseConfig)
	screen := h.pairScreen(h.newAdminToken(), "house_tv", "House TV")
	_, err := widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
	require.NoError(t, err)
	require.NoError(t, h.db.Sources().SetHealth(t.Context(), "family", domain.HealthOffline, "private detail", false, h.clock.Now()))
	_, err = h.db.SQL().ExecContext(t.Context(), "DROP TABLE photos")
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, h.request(http.MethodGet, "/api/v1/home", "", bearer(screen)).StatusCode)
	response := h.request(http.MethodGet, "/api/v1/family/house", "", bearer(screen))
	require.Equal(t, http.StatusOK, response.StatusCode)
	body := decode[map[string]any](t, response)
	nas := body["nas"].([]any)[0].(map[string]any)
	require.Equal(t, "available", nas["availability"])
	require.Equal(t, "offline", nas["items"].([]any)[0].(map[string]any)["health"])
	_, err = h.db.SQL().ExecContext(t.Context(), "ALTER TABLE data_sources RENAME TO unavailable_sources")
	require.NoError(t, err)
	response = h.request(http.MethodGet, "/api/v1/family/house", "", bearer(screen))
	require.Equal(t, http.StatusOK, response.StatusCode)
	body = decode[map[string]any](t, response)
	require.Equal(t, "available", body["core"].(map[string]any)["availability"])
	nas = body["nas"].([]any)[0].(map[string]any)
	require.Equal(t, map[string]any{"source_id": "family", "source_label": "Family photos", "observed_at": nil, "expires_at": nil, "availability": "failed", "reason": "read_failed", "items": []any{}}, nas)
}

func TestHouseResponseMatchesOpenAPI(t *testing.T) {
	raw, err := os.ReadFile("../../docs/api/openapi.yaml")
	require.NoError(t, err)
	var doc struct {
		Paths      map[string]map[string]any `yaml:"paths"`
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Paths["/api/v1/family/house"]["get"])
	require.NotEmpty(t, doc.Components.Schemas["HouseResponse"])
	definitions := map[string]any{}
	for name, schema := range doc.Components.Schemas {
		if strings.HasPrefix(name, "House") || strings.HasPrefix(name, "Family") {
			definitions[name] = schema
		}
	}
	schemaJSON, err := json.Marshal(map[string]any{"$ref": "#/$defs/HouseResponse", "$defs": definitions})
	require.NoError(t, err)
	schemaJSON = bytes.ReplaceAll(schemaJSON, []byte("#/components/schemas/"), []byte("#/$defs/"))
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	h := newHarness(t, houseConfig)
	token := h.pairScreen(h.newAdminToken(), "house_tv", "House TV")
	validate := func() map[string]any {
		t.Helper()
		response := h.request(http.MethodGet, "/api/v1/family/house", "", bearer(token))
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
	body["nas"].([]any)[0].(map[string]any)["root_path"] = "/private/source"
	require.Error(t, resolved.Validate(body), "public schema excludes maintenance fields")
	h.clock.Advance(time.Minute)
	validate() // expired, with prior observed content
	_, err = h.db.SQL().ExecContext(t.Context(), "ALTER TABLE data_sources RENAME TO unavailable_sources")
	require.NoError(t, err)
	validate() // failed authorization read with explicit null times and [] items
}

func TestHouseReauthorizationWaitsForNewObservation(t *testing.T) {
	for _, change := range []string{"restore", "reconcile_revoked", "replace_root"} {
		t.Run(change, func(t *testing.T) {
			h := newHarness(t, houseConfig)
			token := h.pairScreen(h.newAdminToken(), "house_tv", "House TV")
			_, err := widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
			require.NoError(t, err)
			require.NoError(t, h.db.Sources().SetHealth(t.Context(), "family", domain.HealthOnline, "", true, h.clock.Now()))
			require.NoError(t, h.db.Sources().SetShareStats(t.Context(), "family", 100, 50, h.clock.Now()))
			if change != "replace_root" {
				require.NoError(t, h.db.Sources().Revoke(t.Context(), "family", "admin_revoked", h.clock.Now()))
			}
			switch change {
			case "restore":
				require.NoError(t, h.db.Sources().Restore(t.Context(), "family", h.clock.Now()))
			case "reconcile_revoked":
				_, err = widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
				require.NoError(t, err)
			case "replace_root":
				h.cfg.Sources[0].Root = "/private/new-root"
				_, err = widget.ReconcileSources(t.Context(), h.db, h.cfg, h.clock.Now())
				require.NoError(t, err)
			}
			response := h.request(http.MethodGet, "/api/v1/family/house", "", bearer(token))
			require.Equal(t, http.StatusOK, response.StatusCode)
			body := decode[map[string]any](t, response)
			nas := body["nas"].([]any)[0].(map[string]any)
			require.Equal(t, "loading", nas["availability"])
			require.Equal(t, "not_observed", nas["reason"])
			require.Nil(t, nas["observed_at"])
			require.Nil(t, nas["expires_at"])
			require.Empty(t, nas["items"])
			row, err := h.db.Sources().Get(t.Context(), "family")
			require.NoError(t, err)
			require.Nil(t, row.LastSuccessAt)
			require.Nil(t, row.ShareStatsAt)
			require.Nil(t, row.ShareFreeBytes)
			require.Nil(t, row.ShareTotalBytes)
		})
	}
}
