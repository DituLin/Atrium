package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func integrationToken(t *testing.T, h *photoHarness, policy domain.IntegrationPolicy) (*domain.IntegrationPrincipal, string) {
	t.Helper()
	p, token, err := auth.NewIntegrationService(h.db, h.clock.Now).Issue(context.Background(), "test agent", policy, h.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	return p, token
}

func TestIntegrationReadScopeAndProjection(t *testing.T) {
	h := newPhotoHarness(t)
	photo := h.seedPhoto(t, seedOptions{RelPath: "private-family-name.jpg", IsBaseline: true})
	_, token := integrationToken(t, h, domain.IntegrationPolicy{Permissions: []string{"home.read", "photos.read", "nas.read"}, SourceIDs: []string{photoSourceID}})
	resp := h.request(http.MethodGet, "/api/v1/integrations/home", "", bearer(token))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decode[map[string]any](t, resp)
	require.Equal(t, "1", body["schema_version"])
	require.Equal(t, "available", body["availability"])
	data := body["data"].(map[string]any)
	require.EqualValues(t, 1, data["photos"].(map[string]any)["ready"])
	raw := decodeRaw(t, h.harness, "/api/v1/integrations/photos?collection=all", token)
	require.Contains(t, raw, photo.ID)
	for _, secret := range []string{"private-family-name.jpg", "/media/", "urls", "rel_path", "root_path", "atr_int_"} {
		require.NotContains(t, raw, secret)
	}
	for _, path := range []string{"/api/v1/photos", "/api/v1/screens", "/api/v1/diagnostics", "/api/v1/media/photos/" + photo.ID} {
		require.Equal(t, http.StatusForbidden, h.request(http.MethodGet, path, "", bearer(token)).StatusCode, path)
	}
	_, empty := integrationToken(t, h, domain.IntegrationPolicy{Permissions: []string{"photos.read"}})
	raw = decodeRaw(t, h.harness, "/api/v1/integrations/photos?collection=all", empty)
	require.NotContains(t, raw, photo.ID)
	require.Equal(t, http.StatusNotFound, h.request(http.MethodGet, "/api/v1/integrations/photos/"+photo.ID, "", bearer(empty)).StatusCode)
	require.Equal(t, http.StatusForbidden, h.request(http.MethodGet, "/api/v1/integrations/home", "", bearer(empty)).StatusCode)
}

func TestIntegrationCursorCannotCrossPrincipalOrPolicy(t *testing.T) {
	h := newPhotoHarness(t)
	h.seedPhoto(t, seedOptions{RelPath: "a.jpg"})
	h.seedPhoto(t, seedOptions{RelPath: "b.jpg"})
	policy := domain.IntegrationPolicy{Permissions: []string{"photos.read"}, SourceIDs: []string{photoSourceID}}
	p, token := integrationToken(t, h, policy)
	_, other := integrationToken(t, h, policy)
	resp := h.request(http.MethodGet, "/api/v1/integrations/photos?collection=all&limit=1", "", bearer(token))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decode[map[string]any](t, resp)
	cursor := body["data"].(map[string]any)["next_cursor"].(string)
	path := "/api/v1/integrations/photos?collection=all&limit=1&cursor=" + url.QueryEscape(cursor)
	require.Equal(t, http.StatusBadRequest, h.request(http.MethodGet, path, "", bearer(other)).StatusCode)
	require.Equal(t, http.StatusOK, h.request(http.MethodGet, path, "", bearer(token)).StatusCode)
	policy.SourceIDs = nil
	_, err := auth.NewIntegrationService(h.db, h.clock.Now).UpdatePolicy(context.Background(), p.ID, policy)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, h.request(http.MethodGet, path, "", bearer(token)).StatusCode)
}

func TestIntegrationHomeReadDoesNotImplyOtherPermissions(t *testing.T) {
	h := newPhotoHarness(t)
	h.seedPhoto(t, seedOptions{RelPath: "hidden.jpg"})
	_, token := integrationToken(t, h, domain.IntegrationPolicy{Permissions: []string{"home.read"}, SourceIDs: []string{photoSourceID}})
	body := decode[map[string]any](t, h.request(http.MethodGet, "/api/v1/integrations/home", "", bearer(token)))
	data := body["data"].(map[string]any)
	require.NotContains(t, data, "photos")
	require.NotContains(t, data, "nas")
	require.NotContains(t, data, "screens")
}
