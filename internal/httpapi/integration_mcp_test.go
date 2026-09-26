package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/brain"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/homemcp"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

// This joins the actual Core/auth/SQLite implementation to the official MCP
// client. Only screen presence is a stub; it does not claim physical TV evidence.
func TestIntegrationMCPActualCoreContract(t *testing.T) {
	h, _ := newCommandHarness(t, true)
	h.pairScreen(h.newAdminToken(), "living_room_tv", "Living room")
	principal, token, err := auth.NewIntegrationService(h.db, h.clock.Now).Issue(context.Background(), "mcp contract", domain.IntegrationPolicy{Permissions: []string{"home.read", "nas.read", "photos.read", "screens.read", "screens.control", "commands.read"}, ScreenIDs: []string{"living_room_tv"}}, h.clock.Now().Add(time.Hour))
	require.NoError(t, err)
	var coreLogs, mcpLogs, brainLogs lockedTraceBuffer
	h.deps.Logger = slog.New(slog.NewJSONHandler(&coreLogs, nil))
	core := httptest.NewTLSServer(httpapi.New(h.deps).Handler())
	t.Cleanup(core.Close)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	secret := filepath.Join(dir, "service.token")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: core.Certificate().Raw}), 0600))
	require.NoError(t, os.WriteFile(secret, []byte(token), 0600))
	server, err := homemcp.NewServerWithLogger(homemcp.Config{CoreURL: core.URL, CAFile: ca, TokenFile: secret}, slog.New(slog.NewJSONHandler(&mcpLogs, nil)))
	require.NoError(t, err)
	a, b := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), b, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	client, err := mcp.NewClient(&mcp.Implementation{Name: "core contract", Version: "1"}, nil).Connect(context.Background(), a, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		require.NoError(t, err)
		return result
	}
	for _, name := range []string{"home_get_status", "home_get_nas_status", "home_list_screens", "home_list_photos"} {
		require.False(t, call(name, map[string]any{}).IsError, name)
	}
	op := ulid.MustNew(ulid.Timestamp(h.clock.Now()), rand.Reader).String()
	args := map[string]any{"screen_id": "living_room_tv", "route": "dashboard", "operation_id": op, "wait_ms": 0}
	first := call("home_navigate_screen", args)
	require.False(t, first.IsError)
	raw, err := json.Marshal(first.StructuredContent)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"status":"accepted"`)
	second := call("home_navigate_screen", args)
	require.False(t, second.IsError)
	rows, err := h.db.Commands().List(context.Background(), "living_room_tv", "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	_, err = h.db.Commands().Resolve(context.Background(), rows[0].ID, domain.CommandApplied, "", &domain.CommandResult{Route: &domain.RouteState{Name: domain.RouteDashboard}}, h.clock.Now())
	require.NoError(t, err)
	applied := call("home_get_operation", map[string]any{"operation_id": op})
	require.False(t, applied.IsError)
	raw, err = json.Marshal(applied.StructuredContent)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"status":"applied"`)
	require.NotContains(t, string(raw), token)

	// Exercise the host executor against the same real Core/MCP contract.
	ledgerDir := t.TempDir()
	require.NoError(t, os.Chmod(ledgerDir, 0700))
	scope, err := brain.ScopeFor(core.URL, principal.ID)
	require.NoError(t, err)
	ledger, err := brain.OpenLedger(filepath.Join(ledgerDir, "brain.db"), scope, h.clock.Now)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ledger.Close() })
	inventory, err := client.ListTools(context.Background(), nil)
	require.NoError(t, err)
	modelTools, err := brain.ModelTools(inventory.Tools)
	require.NoError(t, err)
	require.Len(t, modelTools, 10)
	for _, tool := range modelTools {
		require.NotEqual(t, "home_get_operation", tool.Name)
		raw, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "operation_id")
		require.NotContains(t, string(raw), "wait_ms")
	}
	executor := brain.NewExecutorWithLogger(client, ledger, slog.New(slog.NewJSONHandler(&brainLogs, nil)))
	turn := executor.StartTurn(context.Background())
	defer turn.Close()
	hostResult, err := turn.Call("host-call", "home_refresh_screen", map[string]any{"screen_id": "living_room_tv"})
	require.NoError(t, err)
	require.False(t, hostResult.IsError)
	action := brain.Action{Tool: "home_refresh_screen", ScreenID: "living_room_tv"}
	acceptedReport := brain.DescribeAction(action, hostResult, nil)
	require.Equal(t, "accepted", acceptedReport.Status)
	require.Contains(t, acceptedReport.TextZH, "尚未收到屏幕执行确认")
	require.Equal(t, h.clock.Now(), acceptedReport.ObservedAt)
	_, err = turn.Call("host-call", "home_refresh_screen", map[string]any{"screen_id": "living_room_tv"})
	require.NoError(t, err)
	rows, err = h.db.Commands().List(context.Background(), "living_room_tv", "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 2, "host duplicate must not send another command")
	for _, row := range rows {
		if row.Kind == domain.CommandRefresh {
			_, err = h.db.Commands().Resolve(context.Background(), row.ID, domain.CommandApplied, "", &domain.CommandResult{Route: &domain.RouteState{Name: domain.RouteDashboard}}, h.clock.Now())
			require.NoError(t, err)
		}
	}
	hostResult, err = turn.Call("host-call", "home_refresh_screen", map[string]any{"screen_id": "living_room_tv"})
	require.NoError(t, err)
	require.False(t, hostResult.IsError)
	appliedReport := brain.DescribeAction(action, hostResult, nil)
	require.Equal(t, "applied", appliedReport.Status)
	require.Contains(t, appliedReport.TextZH, "屏幕已确认刷新")
	require.Equal(t, acceptedReport.CommandID, appliedReport.CommandID)
	pending, err := ledger.Pending(context.Background())
	require.NoError(t, err)
	require.Empty(t, pending)
	entries, err := h.db.Audit().List(context.Background(), 20)
	require.NoError(t, err)
	traced := false
	for _, entry := range entries {
		if entry.Action != "integration.command" {
			continue
		}
		var fields map[string]string
		require.NoError(t, json.Unmarshal([]byte(entry.Detail), &fields))
		if fields["turn_ref"] == "" {
			continue
		}
		traced = true
		for _, id := range []string{fields["turn_ref"], fields["call_ref"], strings.TrimPrefix(fields["detail"], "operation:")} {
			require.Contains(t, brainLogs.String(), id)
			require.Contains(t, mcpLogs.String(), id)
			require.Contains(t, coreLogs.String(), id)
		}
		require.Contains(t, mcpLogs.String(), fields["request_id"])
		require.Contains(t, coreLogs.String(), fields["request_id"])
		require.Contains(t, mcpLogs.String(), entry.Target)
		require.Contains(t, coreLogs.String(), entry.Target)
		require.Equal(t, "integration:"+principal.ID, entry.Actor)
	}
	require.True(t, traced)
	for _, logs := range []string{brainLogs.String(), mcpLogs.String(), coreLogs.String()} {
		require.NotContains(t, logs, token)
		require.NotContains(t, logs, secret)
		require.NotContains(t, logs, core.URL)
	}
	turn.Close()
	_, err = turn.Call("after-cancel", "home_get_status", map[string]any{})
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, auth.NewIntegrationService(h.db, h.clock.Now).Revoke(context.Background(), principal.ID))
	require.True(t, call("home_get_status", map[string]any{}).IsError)
	// Removing the integration has no effect on the original administrator API.
	require.Equal(t, 200, h.request("GET", "/api/v1/screens", "", bearer(h.newAdminToken())).StatusCode)
}

// Logger writes can complete concurrently with a response; readers also lock.
type lockedTraceBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedTraceBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *lockedTraceBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
