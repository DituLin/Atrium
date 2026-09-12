package brainhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/brain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type waitingCaller struct{ entered chan struct{} }

func (f *waitingCaller) CallTool(ctx context.Context, _ *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	close(f.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestServerCanCancelInFlightCall(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	ledger, err := brain.OpenLedger(filepath.Join(dir, "brain.db"), "test", nil)
	require.NoError(t, err)
	defer ledger.Close()
	caller := &waitingCaller{entered: make(chan struct{})}
	runtime := brain.NewRuntime(brain.NewExecutor(caller, ledger))
	input, send := io.Pipe()
	output, receive := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), input, receive, runtime, nil); receive.Close() }()
	encoder := json.NewEncoder(send)
	decoder := json.NewDecoder(output)
	require.NoError(t, encoder.Encode(map[string]any{"id": "1", "method": "start", "run_id": "run"}))
	var response map[string]any
	require.NoError(t, decoder.Decode(&response))
	require.Nil(t, response["error"])
	require.NoError(t, encoder.Encode(map[string]any{"id": "2", "method": "call", "run_id": "run", "call_id": "call", "tool": "home_refresh_screen", "arguments": map[string]any{"screen_id": "tv"}}))
	<-caller.entered
	require.NoError(t, encoder.Encode(map[string]any{"id": "3", "method": "cancel", "run_id": "run"}))
	for range 2 {
		require.NoError(t, decoder.Decode(&response))
		if response["id"] == "2" {
			require.NotNil(t, response["error"])
		}
	}
	require.NoError(t, send.Close())
	require.NoError(t, <-done)
	pending, err := ledger.Pending(context.Background())
	require.NoError(t, err)
	require.Len(t, pending, 1)
}
func TestServerRejectsUnknownRequestFields(t *testing.T) {
	var out bytes.Buffer
	err := Serve(context.Background(), io.NopCloser(bytes.NewBufferString("{\"id\":\"1\",\"method\":\"tools\",\"token\":\"secret\"}\n")), nopWriteCloser{&out}, nil, nil)
	require.Error(t, err)
	require.NotContains(t, out.String(), "secret")
}

func TestServerRejectsOversizedInput(t *testing.T) {
	var out bytes.Buffer
	input := io.NopCloser(bytes.NewBufferString(strings.Repeat("x", 65537) + "\n"))
	require.Error(t, Serve(context.Background(), input, nopWriteCloser{&out}, nil, nil))
	require.Empty(t, out.String())
}

func TestServerCancellationUnblocksOutput(t *testing.T) {
	input, send := io.Pipe()
	output, receive := io.Pipe()
	defer output.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, input, receive, nil, nil) }()
	_, err := io.WriteString(send, "{\"id\":\"1\",\"method\":\"tools\"}\n")
	require.NoError(t, err)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("output backpressure prevented cancellation")
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestRecoveryOutputPreservesPartialProgressOnFailure(t *testing.T) {
	var out bytes.Buffer
	op := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	failure := &brain.ActionOutcomeError{OperationID: op, CommandID: "cmd1", Cause: errors.New("private secret")}
	results := []*mcp.CallToolResult{{StructuredContent: map[string]any{"observed_at": "2026-09-07T00:00:00Z"}}}
	require.Error(t, WriteRecovery(&out, results, "previous-operation", failure))
	require.Contains(t, out.String(), op)
	require.Contains(t, out.String(), "previous-operation")
	require.Contains(t, out.String(), "2026-09-07T00:00:00Z")
	require.NotContains(t, out.String(), "private secret")
}
