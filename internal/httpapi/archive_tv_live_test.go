package httpapi_test

import (
	"context"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/brain"
	"github.com/DituLin/Atrium/internal/homemcp"
	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Called only with the isolated Core and an actual browser/preview TV connected.
func TestT14LiveBrainArchiveShow(t *testing.T) {
	dir := os.Getenv("ATRIUM_T14_SHOW_DIR")
	if dir == "" {
		t.Skip("live browser opt in")
	}
	raw, e := os.ReadFile(filepath.Join(dir, "state.json"))
	require.NoError(t, e)
	var state map[string]string
	require.NoError(t, json.Unmarshal(raw, &state))
	work := t.TempDir()
	require.NoError(t, os.Chmod(work, 0700))
	token := filepath.Join(work, "token")
	require.NoError(t, os.WriteFile(token, []byte(state["integration_token"]), 0600))
	server, e := homemcp.NewServer(homemcp.Config{CoreURL: state["origin"], CAFile: filepath.Join(dir, "ca.crt"), TokenFile: token})
	require.NoError(t, e)
	ctx := context.Background()
	a, b := mcp.NewInMemoryTransports()
	ss, e := server.Connect(ctx, b, nil)
	require.NoError(t, e)
	defer ss.Close()
	client, e := mcp.NewClient(&mcp.Implementation{Name: "t14-live", Version: "1"}, nil).Connect(ctx, a, nil)
	require.NoError(t, e)
	defer client.Close()
	tools, e := client.ListTools(ctx, nil)
	require.NoError(t, e)
	_, e = brain.CompleteModelTools(tools.Tools)
	require.NoError(t, e)
	ledger, e := brain.OpenLedger(filepath.Join(work, "brain.db"), "t14-synthetic", nil)
	require.NoError(t, e)
	defer ledger.Close()
	turn := brain.NewExecutor(client, ledger).StartTurn(ctx)
	defer turn.Close()
	result, e := turn.Call("query", "home_search_archives", map[string]any{"domain": "health"})
	require.NoError(t, e)
	require.False(t, result.IsError)
	raw, e = json.Marshal(result.StructuredContent)
	require.NoError(t, e)
	var receipt integration.Response[archive.SearchPage]
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.NotEmpty(t, receipt.Data.QueryRef)
	result, e = turn.Call("show", "home_show_archive_query", map[string]any{"screen_id": "preview-browser", "query_ref": receipt.Data.QueryRef})
	require.NoError(t, e)
	require.False(t, result.IsError)
	raw, e = json.Marshal(result.StructuredContent)
	require.NoError(t, e)
	var command integration.Response[integration.Command]
	require.NoError(t, json.Unmarshal(raw, &command))
	require.Equal(t, "accepted", command.Data.Status)
	require.Eventually(t, func() bool {
		result, e = turn.Call("poll", "home_get_command", map[string]any{"command_id": command.Data.ID})
		if e != nil || result.IsError {
			return false
		}
		raw, e = json.Marshal(result.StructuredContent)
		if e != nil || json.Unmarshal(raw, &command) != nil {
			return false
		}
		return command.Data.Status == "applied"
	}, 15*time.Second, 200*time.Millisecond, "must come from actual DOM render acknowledgement")
	require.Equal(t, "archive", command.Data.Kind)
	require.NotEmpty(t, command.Data.Payload.ArchiveViewID)
	require.Equal(t, command.Data.Payload.ArchiveViewID, command.Data.Result.ResourceID)
	t.Log("real Core -> MCP -> Brain -> WebSocket -> rendered archive DOM -> applied confirmed")
	if out := os.Getenv("ATRIUM_T14_SHOW_EVIDENCE"); out != "" {
		require.NoError(t, os.WriteFile(out, raw, 0600))
	}
}
