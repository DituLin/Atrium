package brain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

const receiptInvocation = "38e76224-95c8-4c42-9b77-e05d283ce0c6"

func receiptFixture(t *testing.T) (*Receipt, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	path := filepath.Join(dir, "evidence.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0600))
	receipt, err := OpenReceipt(path, receiptInvocation)
	require.NoError(t, err)
	t.Cleanup(func() { _ = receipt.Close() })
	return receipt, path
}
func TestReceiptRecordsVerifiedActionAndSafeFailure(t *testing.T) {
	receipt, path := receiptFixture(t)
	ledger, _ := executorLedger(t)
	caller := &fakeCaller{reply: receiptCommandReply("accepted")}
	runtime := NewRuntimeWithReceipt(NewExecutor(caller, ledger), receipt)
	defer runtime.Close()
	require.NoError(t, runtime.Start(context.Background(), "run"))
	_, _, err := runtime.Call("run", "call", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.NoError(t, err)
	caller.err = errors.New("private secret /nas/path")
	_, _, err = runtime.Call("run", "call", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.Error(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private secret")
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, lines, 2)
	var first, second map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &second))
	require.Equal(t, receiptInvocation, first["invocation_id"])
	toolResult := first["tool_result"].(map[string]any)
	require.Len(t, toolResult, 1)
	require.Contains(t, toolResult, "structuredContent")
	require.NotContains(t, toolResult, "content")
	require.Equal(t, "accepted", first["report"].(map[string]any)["Status"])
	require.NotEmpty(t, second["error"].(map[string]any)["operation_id"])
	require.Nil(t, second["tool_result"])
}
func TestReceiptRefusesUnsafeFilesAndForeignInvocation(t *testing.T) {
	_, path := receiptFixture(t)
	require.NoError(t, os.Chmod(path, 0644))
	_, err := OpenReceipt(path, receiptInvocation)
	require.Error(t, err)
	require.NoError(t, os.Chmod(path, 0600))
	link := filepath.Join(filepath.Dir(path), "link")
	require.NoError(t, os.Symlink(path, link))
	_, err = OpenReceipt(link, receiptInvocation)
	require.Error(t, err)
	_, err = OpenReceipt(path, "not-uuid")
	require.Error(t, err)
}
func TestReceiptCapacityPreventsNinthNetworkCallAcrossRuns(t *testing.T) {
	receipt, path := receiptFixture(t)
	ledger, _ := executorLedger(t)
	result := &mcp.CallToolResult{StructuredContent: map[string]any{"schema_version": "1", "observed_at": "2026-09-07T00:00:00Z", "availability": "available", "data": []any{}}}
	caller := &fakeCaller{reply: result}
	runtime := NewRuntimeWithReceipt(NewExecutor(caller, ledger), receipt)
	defer runtime.Close()
	require.NoError(t, runtime.Start(context.Background(), "run"))
	for range 8 {
		_, _, err := runtime.Call("run", "read", "home_list_screens", nil)
		require.NoError(t, err)
	}
	require.NoError(t, runtime.Start(context.Background(), "next"))
	_, _, err := runtime.Call("next", "read", "home_list_screens", nil)
	require.Error(t, err)
	require.Len(t, caller.names, 8)
	raw, _ := os.ReadFile(path)
	require.Equal(t, 8, strings.Count(string(raw), "\n"))
}

func TestReceiptWriteFailureRetainsSubmittedIdentityAndClosesRuntime(t *testing.T) {
	receipt, _ := receiptFixture(t)
	ledger, _ := executorLedger(t)
	caller := callbackCaller(func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		require.NoError(t, receipt.Close())
		return receiptCommandReply("accepted"), nil
	})
	runtime := NewRuntimeWithReceipt(NewExecutor(caller, ledger), receipt)
	defer runtime.Close()
	require.NoError(t, runtime.Start(context.Background(), "run"))
	result, report, err := runtime.Call("run", "action", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrReceipt)
	var outcome *ActionOutcomeError
	require.ErrorAs(t, err, &outcome)
	require.NotEmpty(t, outcome.OperationID)
	require.Equal(t, "cmd1", outcome.CommandID)
	require.Equal(t, "unconfirmed", report.Status)
	require.ErrorIs(t, runtime.Start(context.Background(), "next"), ErrRunClosed)
}
func TestReceiptDropsUnverifiedReadContent(t *testing.T) {
	receipt, path := receiptFixture(t)
	ledger, _ := executorLedger(t)
	caller := &fakeCaller{reply: &mcp.CallToolResult{StructuredContent: map[string]any{"schema_version": "1", "observed_at": "2026-09-07T00:00:00Z", "availability": "available", "data": map[string]any{"id": "other", "path": "/private/secret"}}, Content: []mcp.Content{&mcp.TextContent{Text: "model prose secret"}}}}
	runtime := NewRuntimeWithReceipt(NewExecutor(caller, ledger), receipt)
	defer runtime.Close()
	require.NoError(t, runtime.Start(context.Background(), "run"))
	result, _, err := runtime.Call("run", "read", "home_get_photo", map[string]any{"photo_id": "photo"})
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrInvalidOutcome)
	raw, _ := os.ReadFile(path)
	require.NotContains(t, string(raw), "secret")
	require.Contains(t, string(raw), `"tool_result":null`)
}
func TestReceiptRefusesForeignOrPartialExistingEvidence(t *testing.T) {
	for _, raw := range []string{`{"schema_version":1,"invocation_id":"foreign"}` + "\n", `{"partial":`} {
		receipt, path := receiptFixture(t)
		require.NoError(t, os.WriteFile(path, []byte(raw), 0600))
		release, err := receipt.acquire(context.Background())
		require.ErrorIs(t, err, ErrReceipt)
		require.Nil(t, release)
		after, _ := os.ReadFile(path)
		require.Equal(t, raw, string(after))
	}
}
func TestReceiptBusinessUncertaintyRetainsHostOperation(t *testing.T) {
	receipt, path := receiptFixture(t)
	ledger, _ := executorLedger(t)
	caller := &fakeCaller{reply: &mcp.CallToolResult{IsError: true, StructuredContent: map[string]any{"schema_version": "1", "observed_at": "2026-09-07T00:00:00Z", "availability": "unknown", "data": nil, "error": map[string]any{"code": "outcome_unknown", "message": "private body", "operation_id": "forged"}}}}
	runtime := NewRuntimeWithReceipt(NewExecutor(caller, ledger), receipt)
	defer runtime.Close()
	require.NoError(t, runtime.Start(context.Background(), "run"))
	_, report, err := runtime.Call("run", "action", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.NoError(t, err)
	require.NotEmpty(t, report.OperationID)
	raw, _ := os.ReadFile(path)
	require.NotContains(t, string(raw), "forged")
	require.NotContains(t, string(raw), "private body")
	require.Contains(t, string(raw), report.OperationID)
}

func TestReceiptSeparateDescriptorsShareCancelableAdmission(t *testing.T) {
	first, path := receiptFixture(t)
	second, err := OpenReceipt(path, receiptInvocation)
	require.NoError(t, err)
	defer second.Close()
	release, err := first.acquire(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	blocked := make(chan error, 1)
	go func() {
		unlock, err := second.acquire(ctx)
		if unlock != nil {
			unlock()
		}
		blocked <- err
	}()
	cancel()
	require.ErrorIs(t, <-blocked, ErrReceipt)
	release()
	unlock, err := second.acquire(context.Background())
	require.NoError(t, err)
	unlock()
}
func TestReceiptRejectsOversizedFileWithoutTruncation(t *testing.T) {
	receipt, path := receiptFixture(t)
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", 512*1024+1)), 0600))
	unlock, err := receipt.acquire(context.Background())
	require.Nil(t, unlock)
	require.ErrorIs(t, err, ErrReceipt)
	stat, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, int64(512*1024+1), stat.Size())
}

func receiptCommandReply(status string) *mcp.CallToolResult {
	result := commandReply(status)
	data := result.StructuredContent.(map[string]any)["data"].(map[string]any)
	data["sequence"] = 1
	data["issued_at"] = "2026-09-07T00:00:00Z"
	data["expires_at"] = "2026-09-07T00:00:10Z"
	return result
}
func TestReceiptRejectsOmittedFactsRatherThanDefaultingFalse(t *testing.T) {
	result := &mcp.CallToolResult{StructuredContent: map[string]any{"schema_version": "1", "observed_at": "2026-09-07T00:00:00Z", "availability": "available", "data": map[string]any{"id": "tv"}}}
	_, err := receiptResult("home_get_screen", map[string]any{"screen_id": "tv"}, result)
	require.ErrorIs(t, err, ErrInvalidOutcome)
}

func TestReceiptInvalidPollCannotRetainAppliedReport(t *testing.T) {
	receipt, path := receiptFixture(t)
	ledger, _ := executorLedger(t)
	caller := &fakeCaller{reply: receiptCommandReply("accepted")}
	runtime := NewRuntimeWithReceipt(NewExecutor(caller, ledger), receipt)
	defer runtime.Close()
	require.NoError(t, runtime.Start(context.Background(), "run"))
	_, _, err := runtime.Call("run", "show", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.NoError(t, err)
	caller.reply = commandReply("applied") // missing required command metadata
	result, report, err := runtime.Call("run", "poll", "home_get_command", map[string]any{"command_id": "cmd1"})
	require.ErrorIs(t, err, ErrInvalidOutcome)
	require.Nil(t, result)
	require.Equal(t, "unconfirmed", report.Status)
	raw, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, lines, 2)
	require.NotContains(t, lines[1], `"Status":"applied"`)
}
