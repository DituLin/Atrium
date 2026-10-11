package httpapi_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"github.com/DituLin/Atrium/internal/brain"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/homemcp"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFamilyMCPThroughBrainAndActualCore(t *testing.T) {
	ctx := context.Background()
	h := newPhotoHarness(t)
	require.NoError(t, h.db.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, h.clock.Now()))
	_, token := integrationToken(t, h, domain.IntegrationPolicy{Permissions: []string{"family.read", "family.write"}, MemberIDs: []string{"abu"}})
	core := httptest.NewTLSServer(httpapi.New(h.deps).Handler())
	defer core.Close()
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	secret := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: core.Certificate().Raw}), 0600))
	require.NoError(t, os.WriteFile(secret, []byte(token), 0600))
	server, err := homemcp.NewServer(homemcp.Config{CoreURL: core.URL, CAFile: ca, TokenFile: secret})
	require.NoError(t, err)
	a, b := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, b, nil)
	require.NoError(t, err)
	defer ss.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "family-contract", Version: "1"}, nil).Connect(ctx, a, nil)
	require.NoError(t, err)
	defer client.Close()
	tools, err := client.ListTools(ctx, nil)
	require.NoError(t, err)
	modelTools, err := brain.ModelTools(tools.Tools)
	require.NoError(t, err)
	require.Len(t, modelTools, 20)
	require.NoError(t, os.Chmod(dir, 0700))
	ledger, err := brain.OpenLedger(filepath.Join(dir, "ledger.db"), "test", nil)
	require.NoError(t, err)
	defer ledger.Close()
	turn := brain.NewExecutor(client, ledger).StartTurn(ctx)
	defer turn.Close()
	result, err := turn.Call("members", "home_list_family_members", map[string]any{})
	require.NoError(t, err)
	require.False(t, result.IsError)
	input := map[string]any{"member_id": "abu", "category": "growth", "occurred_at": "2026-10-03T08:00:00+08:00", "title": "测试身高", "measurements": []any{map[string]any{"metric": "height", "value": 70, "unit": "cm"}}, "display_on_tv": true}
	first, err := turn.Call("record1", "home_record_family_entry", input)
	require.NoError(t, err)
	require.False(t, first.IsError)
	raw, _ := json.Marshal(first.StructuredContent)
	require.Contains(t, string(raw), `"nas_status":"pending"`)
	again, err := turn.Call("record1", "home_record_family_entry", input)
	require.NoError(t, err)
	require.False(t, again.IsError)
	input["title"] = "改标题"
	conflict, err := turn.Call("record1", "home_record_family_entry", input)
	require.NoError(t, err)
	require.True(t, conflict.IsError)
	records, err := h.db.FamilyRecords().List(ctx, "abu", false, 50)
	require.NoError(t, err)
	require.Len(t, records, 1)
	queried, err := turn.Call("query", "home_list_family_records", map[string]any{"member_id": "abu", "limit": 1})
	require.NoError(t, err)
	require.False(t, queried.IsError)
	raw, _ = json.Marshal(queried.StructuredContent)
	require.Contains(t, string(raw), records[0].ID)
	input["supersedes_id"] = records[0].ID
	input["revision_reason"] = "更正测试值"
	input["measurements"] = []any{map[string]any{"metric": "height", "value": 71, "unit": "cm"}}
	corrected, err := turn.Call("correction", "home_record_family_entry", input)
	require.NoError(t, err)
	require.False(t, corrected.IsError)
	tv, err := h.db.FamilyRecords().List(ctx, "abu", true, 50)
	require.NoError(t, err)
	require.Len(t, tv, 1)
	require.Equal(t, float64(71), tv[0].Measurements[0].Value)
	input["supersedes_id"] = tv[0].ID
	input["revision_reason"] = "撤回测试"
	input["retracted"] = true
	input["display_on_tv"] = false
	retracted, err := turn.Call("retraction", "home_record_family_entry", input)
	require.NoError(t, err)
	require.False(t, retracted.IsError)
	tv, err = h.db.FamilyRecords().List(ctx, "abu", true, 50)
	require.NoError(t, err)
	require.Empty(t, tv)
	queried, err = turn.Call("history", "home_list_family_records", map[string]any{"member_id": "abu", "limit": 50})
	require.NoError(t, err)
	require.False(t, queried.IsError)
	raw, _ = json.Marshal(queried.StructuredContent)
	require.Contains(t, string(raw), `"superseded_by"`)
	require.Contains(t, string(raw), `"retracted":true`)
	input["request_id"] = "forged"
	_, err = turn.Call("spoof", "home_record_family_entry", input)
	require.Error(t, err)
}
