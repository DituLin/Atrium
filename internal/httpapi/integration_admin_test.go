package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/google/jsonschema-go/jsonschema"
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

func TestAdminHouseholdDiscoveryAndExplicitArchiveGrant(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	resp := h.request(http.MethodGet, "/api/v1/admin/household", "", bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	h.pairScreen(admin, "living_room_tv", "Living room")
	household := decode[struct {
		ID string `json:"id"`
	}](t, resp)
	require.NotEmpty(t, household.ID)
	p, token := integrationToken(t, h, domain.IntegrationPolicy{Permissions: []string{"home.read", "screens.read", "screens.control", "family.read", "family.write"}, ScreenIDs: []string{"living_room_tv"}, SourceIDs: []string{photoSourceID}})
	require.Equal(t, http.StatusForbidden, h.request(http.MethodGet, "/api/v1/admin/household", "", bearer(token)).StatusCode)
	path := "/api/v1/admin/integrations/" + p.ID + "/grant-archives"
	body := `{"permissions":["archive.read","archive.ingest"],"household_ids":["` + household.ID + `"],"expected_policy_version":1}`
	resp = h.request(http.MethodPost, path, body, bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := decode[domain.IntegrationPrincipal](t, resp)
	require.EqualValues(t, 2, got.PolicyVersion)
	require.Equal(t, p.Policy.ScreenIDs, got.Policy.ScreenIDs)
	require.Equal(t, p.Policy.SourceIDs, got.Policy.SourceIDs)
	require.Equal(t, p.Policy.MemberIDs, got.Policy.MemberIDs)
	for _, permission := range p.Policy.Permissions {
		require.True(t, got.Policy.Allows(permission))
	}
	require.True(t, got.Policy.Allows("archive.ingest"))
	require.True(t, got.Policy.Allows("archive.read"))
	require.False(t, got.Policy.Allows("archive.organize"))
	require.False(t, got.Policy.Allows("archive.analyze"))
	require.Equal(t, []string{household.ID}, got.Policy.HouseholdIDs)
	require.Equal(t, http.StatusConflict, h.request(http.MethodPost, path, body, bearer(admin)).StatusCode)
	require.Equal(t, http.StatusForbidden, h.request(http.MethodPost, path, body, bearer(token)).StatusCode)
	// Complete replacement remains complete replacement: omitted archives are revoked.
	resp = h.request(http.MethodPut, "/api/v1/admin/integrations/"+p.ID+"/policy", `{"permissions":["home.read"]}`, bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	replaced := decode[domain.IntegrationPrincipal](t, resp)
	require.Empty(t, replaced.Policy.HouseholdIDs)
}

func TestPublishedArchivePolicyAndGrantContract(t *testing.T) {
	raw, err := os.ReadFile("../../docs/api/integrations.openapi.json")
	require.NoError(t, err)
	var doc struct {
		Paths      map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	for _, name := range []string{"Policy", "ArchiveGrant"} {
		var schema jsonschema.Schema
		require.NotEmpty(t, doc.Components.Schemas[name])
		require.NoError(t, json.Unmarshal(doc.Components.Schemas[name], &schema))
		resolved, err := schema.Resolve(nil)
		require.NoError(t, err)
		input := map[string]any{"permissions": []any{"archive.read", "archive.ingest"}, "household_ids": []any{"stable-home"}}
		if name == "ArchiveGrant" {
			input["expected_policy_version"] = float64(1)
		}
		require.NoError(t, resolved.Validate(input))
		if name == "ArchiveGrant" {
			delete(input, "expected_policy_version")
			require.Error(t, resolved.Validate(input))
		}
	}
	require.NotEmpty(t, doc.Paths["/api/v1/admin/household"])
	require.NotEmpty(t, doc.Paths["/api/v1/admin/integrations/{id}/grant-archives"])
}
