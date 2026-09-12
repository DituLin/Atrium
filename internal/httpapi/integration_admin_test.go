package httpapi_test

import (
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"net/http"
	"os"
	"testing"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestAdminIntegrationLifecycle(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	request := `{"label":"home agent","expires_at":"2026-09-06T12:00:00Z","policy":{"permissions":["home.read"],"source_ids":[],"screen_ids":[]}}`
	resp := h.request(http.MethodPost, "/api/v1/admin/integrations", request, bearer(admin))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	result := decode[struct {
		Principal domain.IntegrationPrincipal `json:"principal"`
		Token     string                      `json:"token"`
	}](t, resp)
	require.NotEmpty(t, result.Principal.ID)
	require.NotEmpty(t, result.Token)
	path := "/api/v1/admin/integrations/" + result.Principal.ID
	require.Equal(t, http.StatusOK, h.request(http.MethodGet, "/api/v1/integrations/home", "", bearer(result.Token)).StatusCode)
	require.Equal(t, http.StatusForbidden, h.request(http.MethodGet, "/api/v1/admin/integrations", "", bearer(result.Token)).StatusCode)
	raw := decodeRaw(t, h.harness, "/api/v1/admin/integrations", admin)
	require.NotContains(t, raw, result.Token)
	require.Contains(t, raw, result.Principal.ID)
	update := `{"permissions":[],"source_ids":[],"screen_ids":[]}`
	require.Equal(t, http.StatusOK, h.request(http.MethodPut, path+"/policy", update, bearer(admin)).StatusCode)
	require.Equal(t, http.StatusForbidden, h.request(http.MethodGet, "/api/v1/integrations/home", "", bearer(result.Token)).StatusCode)
	resp = h.request(http.MethodPost, path+"/rotate", `{}`, bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	rotated := decode[struct {
		Principal domain.IntegrationPrincipal `json:"principal"`
		Token     string                      `json:"token"`
	}](t, resp)
	require.Equal(t, result.Principal.ID, rotated.Principal.ID)
	require.NotEqual(t, result.Token, rotated.Token)
	require.Equal(t, http.StatusUnauthorized, h.request(http.MethodGet, "/api/v1/integrations/home", "", bearer(result.Token)).StatusCode)
	require.Equal(t, http.StatusNoContent, h.request(http.MethodDelete, path, "", bearer(admin)).StatusCode)
	require.Equal(t, http.StatusUnauthorized, h.request(http.MethodGet, "/api/v1/integrations/home", "", bearer(rotated.Token)).StatusCode)
}

func TestAdminEmptyPolicyMatchesPublishedContract(t *testing.T) {
	h := newHarness(t)
	resp := h.request(http.MethodPost, "/api/v1/admin/integrations", `{"label":"empty","expires_at":"2026-09-06T12:00:00Z","policy":{}}`, bearer(h.newAdminToken()))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	response := decode[map[string]any](t, resp)
	raw, err := os.ReadFile("../../docs/api/integrations.openapi.json")
	require.NoError(t, err)
	var doc struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(doc.Components.Schemas["Policy"], &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	require.NoError(t, resolved.Validate(response["principal"].(map[string]any)["policy"]))
}
