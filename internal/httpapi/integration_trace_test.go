package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/integration"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestIntegrationTraceBindsAuthenticatedAuditAndDropsArbitraryText(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	h.pairScreen(h.newAdminToken(), "living_room_tv", "Living room")
	p, token, err := auth.NewIntegrationService(h.db, h.clock.Now).Issue(context.Background(), "agent", domain.IntegrationPolicy{Permissions: []string{"screens.control", "screens.read"}, ScreenIDs: []string{"living_room_tv"}}, h.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	var logs bytes.Buffer
	h.deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	h.server = httptest.NewServer(httpapi.New(h.deps).Handler())
	t.Cleanup(h.server.Close)
	turn := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	call := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	trace := func(r *http.Request) {
		r.Header.Set(integration.TurnRefHeader, turn)
		r.Header.Set(integration.CallRefHeader, call)
	}
	op := ulid.MustNew(ulid.Timestamp(h.clock.Now()), rand.Reader).String()
	body := fmt.Sprintf(`{"operation_id":%q,"kind":"refresh","payload":{}}`, op)
	resp := h.request(http.MethodPost, "/api/v1/integrations/screens/living_room_tv/commands", body, bearer(token), trace)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	requestID := resp.Header.Get("X-Request-Id")
	require.NotEmpty(t, requestID)
	entries, err := h.db.Audit().List(context.Background(), 20)
	require.NoError(t, err)
	found := false
	for _, entry := range entries {
		if entry.Action == "integration.command" {
			found = true
			require.Equal(t, "integration:"+p.ID, entry.Actor)
			for _, id := range []string{turn, call, requestID, op} {
				require.Contains(t, entry.Detail, id)
			}
		}
	}
	require.True(t, found)
	for _, id := range []string{turn, call, p.ID, requestID} {
		require.Contains(t, logs.String(), id)
	}
	bad := func(r *http.Request) {
		r.Header.Set(integration.TurnRefHeader, "private-family-content")
		r.Header.Set(integration.CallRefHeader, "https://private/path")
	}
	require.Equal(t, http.StatusOK, h.request(http.MethodGet, "/api/v1/integrations/screens", "", bearer(token), bad).StatusCode)
	require.Equal(t, http.StatusUnauthorized, h.request(http.MethodGet, "/api/v1/integrations/screens", "", trace).StatusCode)
	require.NotContains(t, logs.String(), "private-family-content")
	require.NotContains(t, logs.String(), "https://private/path")
	require.NotContains(t, logs.String(), token)
}
