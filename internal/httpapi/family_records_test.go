package httpapi_test

import (
	"context"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
	"net/http"
	"strings"
	"testing"
)

func TestFamilyRecordsIntegrationAndTV(t *testing.T) {
	h := newPhotoHarness(t)
	ctx := context.Background()
	for _, id := range []string{"abu", "parent"} {
		require.NoError(t, h.db.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: id, Name: id}, h.clock.Now()))
	}
	_, token := integrationToken(t, h, domain.IntegrationPolicy{Permissions: []string{"family.read", "family.write"}, MemberIDs: []string{"abu"}})
	input := `{"request_id":"im-message-1","member_id":"abu","category":"growth","occurred_at":"2026-10-03T08:00:00Z","title":"身高","measurements":[{"metric":"height","value":70,"unit":"cm"}],"display_on_tv":true}`
	post := func(body string) *http.Response {
		return h.request(http.MethodPost, "/api/v1/integrations/family/records", body, bearer(token))
	}
	first := post(input)
	require.Equal(t, http.StatusOK, first.StatusCode)
	data := decode[map[string]any](t, first)["data"].(map[string]any)
	require.Equal(t, false, data["replayed"])
	require.Equal(t, "pending", data["nas_status"])
	replay := post(input)
	require.Equal(t, http.StatusOK, replay.StatusCode)
	require.Equal(t, true, decode[map[string]any](t, replay)["data"].(map[string]any)["replayed"])
	conflict := post(strings.Replace(input, "身高", "改了标题", 1))
	require.Equal(t, http.StatusConflict, conflict.StatusCode)
	conflict.Body.Close()
	denied := post(strings.Replace(input, `"abu"`, `"parent"`, 1))
	require.Equal(t, http.StatusNotFound, denied.StatusCode)
	denied.Body.Close()
	unknown := post(strings.TrimSuffix(input, "}") + `,"principal_id":"forged"}`)
	require.Equal(t, http.StatusBadRequest, unknown.StatusCode)
	unknown.Body.Close()
	private := post(strings.Replace(strings.Replace(input, "im-message-1", "im-message-2", 1), `"display_on_tv":true`, `"display_on_tv":false`, 1))
	require.Equal(t, http.StatusOK, private.StatusCode)
	private.Body.Close()
	get := h.request(http.MethodGet, "/api/v1/integrations/family/records?member_id=abu", "", bearer(token))
	require.Equal(t, http.StatusOK, get.StatusCode)
	records := decode[map[string]any](t, get)["data"].(map[string]any)["records"].([]any)
	require.Len(t, records, 2)
	members := h.request(http.MethodGet, "/api/v1/integrations/family/members", "", bearer(token))
	require.Equal(t, http.StatusOK, members.StatusCode)
	require.Len(t, decode[map[string]any](t, members)["data"].(map[string]any)["members"], 1)
	admin := h.newAdminToken()
	screen := h.pairScreen(admin, "living_room_tv", "TV")
	tv := h.request(http.MethodGet, "/api/v1/family/records?member_id=abu", "", bearer(screen))
	require.Equal(t, http.StatusOK, tv.StatusCode)
	require.Len(t, decode[map[string]any](t, tv)["records"], 1)
	screenWrite := h.request(http.MethodPost, "/api/v1/integrations/family/records", input, bearer(screen))
	require.Equal(t, http.StatusForbidden, screenWrite.StatusCode)
	screenWrite.Body.Close()
	_, readOnly := integrationToken(t, h, domain.IntegrationPolicy{Permissions: []string{"family.read"}, MemberIDs: []string{"abu"}})
	blocked := h.request(http.MethodPost, "/api/v1/integrations/family/records", input, bearer(readOnly))
	require.Equal(t, http.StatusForbidden, blocked.StatusCode)
	blocked.Body.Close()
	for _, query := range []string{"member_id=parent", "member_id=missing", ""} {
		resp := h.request(http.MethodGet, "/api/v1/integrations/family/records?"+query, "", bearer(token))
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		resp.Body.Close()
	}
}

func TestFamilyMemberAdminProvisioning(t *testing.T) {
	h := newPhotoHarness(t)
	admin := h.newAdminToken()
	resp := h.request(http.MethodPut, "/api/v1/admin/family/members/abu", `{"name":"阿布"}`, bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
	members, err := h.db.FamilyRecords().Members(context.Background())
	require.NoError(t, err)
	raw, _ := json.Marshal(members)
	require.Contains(t, string(raw), "阿布")
	resp = h.request(http.MethodGet, "/api/v1/family/members", "", bearer(admin))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}
