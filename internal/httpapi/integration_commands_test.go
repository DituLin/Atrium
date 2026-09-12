package httpapi_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/integration"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestIntegrationHTTPCommandDurableLifecycle(t *testing.T) {
	for _, online := range []bool{true, false} {
		t.Run(fmt.Sprint(online), func(t *testing.T) {
			h, _ := newCommandHarness(t, online)
			admin := h.newAdminToken()
			h.pairScreen(admin, "living_room_tv", "Living room")
			service := auth.NewIntegrationService(h.db, h.clock.Now)
			policy := domain.IntegrationPolicy{Permissions: []string{"screens.control", "commands.read"}, ScreenIDs: []string{"living_room_tv"}}
			_, token, err := service.Issue(context.Background(), "agent", policy, h.clock.Now().Add(time.Hour))
			require.NoError(t, err)
			_, other, err := service.Issue(context.Background(), "other", policy, h.clock.Now().Add(time.Hour))
			require.NoError(t, err)
			op := ulid.MustNew(ulid.Timestamp(h.clock.Now()), rand.Reader).String()
			body := fmt.Sprintf(`{"operation_id":%q,"kind":"navigate","payload":{"route":"dashboard"}}`, op)
			path := "/api/v1/integrations/screens/living_room_tv/commands"
			resp := h.request(http.MethodPost, path, body, bearer(token))
			expected := http.StatusAccepted
			if !online {
				expected = http.StatusConflict
			}
			require.Equal(t, expected, resp.StatusCode)
			first := decode[integration.Response[integration.Command]](t, resp)
			require.NotEmpty(t, first.Data.ID)
			resp = h.request(http.MethodPost, path, body, bearer(token))
			require.Equal(t, expected, resp.StatusCode)
			second := decode[integration.Response[integration.Command]](t, resp)
			require.Equal(t, first.Data.ID, second.Data.ID)
			require.EqualValues(t, 1, second.Data.Sequence)
			resultPath := "/api/v1/integrations/operations/" + op
			require.Equal(t, http.StatusOK, h.request(http.MethodGet, resultPath, "", bearer(token)).StatusCode)
			require.Equal(t, http.StatusNotFound, h.request(http.MethodGet, resultPath, "", bearer(other)).StatusCode)
			require.Equal(t, http.StatusNotFound, h.request(http.MethodGet, "/api/v1/integrations/commands/"+first.Data.ID, "", bearer(other)).StatusCode)
		})
	}
}

func TestIntegrationPhotosNavigationRequiresCollection(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")
	_, token, err := auth.NewIntegrationService(h.db, h.clock.Now).Issue(context.Background(), "agent", domain.IntegrationPolicy{Permissions: []string{"screens.control"}, ScreenIDs: []string{"living_room_tv"}}, h.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	op := ulid.MustNew(ulid.Timestamp(h.clock.Now()), rand.Reader).String()
	body := fmt.Sprintf(`{"operation_id":%q,"kind":"navigate","payload":{"route":"photos"}}`, op)
	require.Equal(t, http.StatusBadRequest, h.request(http.MethodPost, "/api/v1/integrations/screens/living_room_tv/commands", body, bearer(token)).StatusCode)
}

func TestIntegrationCommandResultRedactsUnrelatedTVObservation(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")
	_, token, err := auth.NewIntegrationService(h.db, h.clock.Now).Issue(context.Background(), "agent", domain.IntegrationPolicy{Permissions: []string{"screens.control", "commands.read", "screens.read"}, ScreenIDs: []string{"living_room_tv"}}, h.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	op := ulid.MustNew(ulid.Timestamp(h.clock.Now()), rand.Reader).String()
	body := fmt.Sprintf(`{"operation_id":%q,"kind":"navigate","payload":{"route":"dashboard"}}`, op)
	resp := h.request(http.MethodPost, "/api/v1/integrations/screens/living_room_tv/commands", body, bearer(token))
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	cmd := decode[integration.Response[integration.Command]](t, resp).Data
	_, err = h.db.Commands().Resolve(context.Background(), cmd.ID, domain.CommandApplied, "private-error-code", &domain.CommandResult{Route: &domain.RouteState{Name: domain.RoutePhoto, PhotoID: "private-photo", Collection: "private-collection"}, ResourceID: "private-resource", ClientVersion: "private-version", Observed: &domain.ObservedState{Route: &domain.RouteState{Name: domain.RoutePhoto, PhotoID: "private-observed"}}}, h.clock.Now())
	require.NoError(t, err)
	raw := decodeRaw(t, h, "/api/v1/integrations/commands/"+cmd.ID, token)
	for _, private := range []string{"private-photo", "private-resource", "private-version", "private-observed", "private-error-code", "private-collection", "issued_by"} {
		require.NotContains(t, raw, private)
	}
	require.Contains(t, raw, `"content_visible":false`)
	resp = h.request(http.MethodPost, "/api/v1/integrations/screens/living_room_tv/commands", body, bearer(token))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "applied", decode[integration.Response[integration.Command]](t, resp).Data.Status)
	conflict := fmt.Sprintf(`{"operation_id":%q,"kind":"refresh","payload":{}}`, op)
	resp = h.request(http.MethodPost, "/api/v1/integrations/screens/living_room_tv/commands", conflict, bearer(token))
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Equal(t, "idempotency_conflict", errorCode(t, resp))
}

func TestIntegrationRejectsMultipleJSONValues(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	admin := h.newAdminToken()
	h.pairScreen(admin, "living_room_tv", "Living room")
	_, token, err := auth.NewIntegrationService(h.db, h.clock.Now).Issue(context.Background(), "agent", domain.IntegrationPolicy{Permissions: []string{"screens.control"}, ScreenIDs: []string{"living_room_tv"}}, h.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	op := ulid.MustNew(ulid.Timestamp(h.clock.Now()), rand.Reader).String()
	body := fmt.Sprintf(`{"operation_id":%q,"kind":"navigate","payload":{"route":"dashboard"}} {"unexpected":true}`, op)
	require.Equal(t, http.StatusBadRequest, h.request(http.MethodPost, "/api/v1/integrations/screens/living_room_tv/commands", body, bearer(token)).StatusCode)
}

func TestIntegrationScreenSanitizesTVStrings(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	h.pairScreen(h.newAdminToken(), "living_room_tv", "Living room")
	_, token, err := auth.NewIntegrationService(h.db, h.clock.Now).Issue(context.Background(), "agent", domain.IntegrationPolicy{Permissions: []string{"screens.read"}, ScreenIDs: []string{"living_room_tv"}}, h.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, h.db.Screens().SetRoute(context.Background(), "living_room_tv", &domain.RouteState{Name: "/private/route", Collection: "https://private/collection"}, 0, h.clock.Now()))
	raw := decodeRaw(t, h, "/api/v1/integrations/screens/living_room_tv", token)
	require.NotContains(t, raw, "private")
	require.Contains(t, raw, `"name":"unknown"`)
}
