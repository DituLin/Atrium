package brain

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type traceCaller struct{ calls []*mcp.CallToolParams }

func (c *traceCaller) CallTool(_ context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	c.calls = append(c.calls, p)
	return nil, errors.New("private-token https://private/core/path")
}
func TestExecutorCarriesOpaqueTraceAndNeverLogsArguments(t *testing.T) {
	ledger, _ := executorLedger(t)
	caller := &traceCaller{}
	var logs bytes.Buffer
	e := NewExecutorWithLogger(caller, ledger, slog.New(slog.NewJSONHandler(&logs, nil)))
	turn := e.StartTurn(context.Background())
	defer turn.Close()
	_, err := turn.Call("private-label", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.Error(t, err)
	_, err = turn.Call("private-label", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.Error(t, err)
	require.Len(t, caller.calls, 2)
	for _, call := range caller.calls {
		require.Equal(t, turn.id, call.Meta[integration.TurnRefMeta])
		require.True(t, integration.ValidTraceRef(call.Meta[integration.CallRefMeta].(string)))
		require.NotContains(t, call.Arguments, integration.TurnRefMeta)
	}
	require.NotEqual(t, caller.calls[0].Meta[integration.CallRefMeta], caller.calls[1].Meta[integration.CallRefMeta])
	for _, secret := range []string{"private-token", "https://private", "private-label", "\"screen_id\""} {
		require.NotContains(t, logs.String(), secret)
	}
	require.Contains(t, logs.String(), turn.id)
	pending, err := ledger.Pending(context.Background())
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Contains(t, logs.String(), pending[0].OperationID)
	_, _, err = e.RecoverPage(context.Background(), "")
	require.Error(t, err)
	require.Equal(t, turn.id, caller.calls[2].Meta[integration.TurnRefMeta])
}
