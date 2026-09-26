package brain

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeRequiresStartedRunAndPreservesTurnBudget(t *testing.T) {
	ledger, _ := executorLedger(t)
	caller := &fakeCaller{reply: commandReply("accepted")}
	runtime := NewRuntime(NewExecutor(caller, ledger))
	defer runtime.Close()
	_, _, err := runtime.Call("run1", "call1", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.ErrorIs(t, err, ErrRunClosed)
	require.NoError(t, runtime.Start(context.Background(), "run1"))
	require.NoError(t, runtime.Start(context.Background(), "run1"))
	for range 2 {
		result, report, err := runtime.Call("run1", "call1", "home_refresh_screen", map[string]any{"screen_id": "tv"})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, "accepted", report.Status)
	}
	require.Equal(t, []string{"home_refresh_screen", "home_get_operation"}, caller.names)
	require.NoError(t, runtime.End("run1"))
	require.ErrorIs(t, runtime.Start(context.Background(), "run1"), ErrRunClosed)
	_, _, err = runtime.Call("run1", "call1", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.ErrorIs(t, err, ErrRunClosed)
}

func TestRuntimeRestartCannotResubmitSameRun(t *testing.T) {
	ledger, path := executorLedger(t)
	first := NewRuntime(NewExecutor(&fakeCaller{reply: commandReply("accepted")}, ledger))
	require.NoError(t, first.Start(context.Background(), "runtime-run"))
	_, _, err := first.Call("runtime-run", "first", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.NoError(t, err)
	first.Close()
	require.NoError(t, ledger.Close())
	reopened, err := OpenLedger(path, "test", nil)
	require.NoError(t, err)
	defer reopened.Close()
	caller := &fakeCaller{reply: commandReply("applied")}
	second := NewRuntime(NewExecutor(caller, reopened))
	defer second.Close()
	require.ErrorIs(t, second.Start(context.Background(), "runtime-run"), ErrRunClosed)
	require.Empty(t, caller.names)
	require.NoError(t, second.Start(context.Background(), "new-user-run"))
}

func TestRuntimeCancelKeepsDurableUnknownAction(t *testing.T) {
	ledger, _ := executorLedger(t)
	caller := &fakeCaller{err: errors.New("transport failed")}
	runtime := NewRuntime(NewExecutor(caller, ledger))
	defer runtime.Close()
	require.NoError(t, runtime.Start(context.Background(), "run"))
	_, report, err := runtime.Call("run", "call", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.Error(t, err)
	require.NotEmpty(t, report.OperationID)
	require.NoError(t, runtime.End("run"))
	rows, err := ledger.Pending(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "unconfirmed", rows[0].Status)
}

func TestRuntimeCancelBeforeStartSurvivesRestart(t *testing.T) {
	ledger, path := executorLedger(t)
	runtime := NewRuntime(NewExecutor(&fakeCaller{}, ledger))
	require.NoError(t, runtime.End("cancelled-before-start"))
	require.ErrorIs(t, runtime.Start(context.Background(), "cancelled-before-start"), ErrRunClosed)
	runtime.Close()
	require.NoError(t, ledger.Close())
	reopened, err := OpenLedger(path, "test", nil)
	require.NoError(t, err)
	defer reopened.Close()
	next := NewRuntime(NewExecutor(&fakeCaller{}, reopened))
	defer next.Close()
	require.ErrorIs(t, next.Start(context.Background(), "cancelled-before-start"), ErrRunClosed)
}

func TestRuntimeRepeatedStartCannotResetBudgets(t *testing.T) {
	ledger, _ := executorLedger(t)
	runtime := NewRuntime(NewExecutor(&fakeCaller{reply: commandReply("accepted")}, ledger))
	defer runtime.Close()
	for i := 0; i < 8; i++ {
		require.NoError(t, runtime.Start(context.Background(), "same-run"))
		_, _, err := runtime.Call("same-run", "read", "home_get_status", nil)
		require.NoError(t, err)
	}
	_, _, err := runtime.Call("same-run", "read", "home_get_status", nil)
	require.ErrorIs(t, err, ErrToolBudget)
	require.NoError(t, runtime.Start(context.Background(), "actions"))
	for _, call := range []string{"first", "second"} {
		_, _, err = runtime.Call("actions", call, "home_refresh_screen", map[string]any{"screen_id": "tv"})
		require.NoError(t, err)
	}
	_, _, err = runtime.Call("actions", "third", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.ErrorIs(t, err, ErrActionBudget)
}

func TestRuntimeCancellationPersistenceFailureClosesAllRuns(t *testing.T) {
	ledger, _ := executorLedger(t)
	runtime := NewRuntime(NewExecutor(&fakeCaller{}, ledger))
	defer runtime.Close()
	require.NoError(t, runtime.Start(context.Background(), "active"))
	require.NoError(t, ledger.Close())
	require.Error(t, runtime.End("not-started"))
	_, _, err := runtime.Call("active", "read", "home_get_status", nil)
	require.ErrorIs(t, err, ErrRunClosed)
	require.ErrorIs(t, runtime.Start(context.Background(), "another"), ErrRunClosed)
}

func TestRuntimeModelCommandPollUpdatesOriginalAction(t *testing.T) {
	ledger, _ := executorLedger(t)
	caller := &fakeCaller{reply: commandReply("accepted")}
	runtime := NewRuntime(NewExecutor(caller, ledger))
	defer runtime.Close()
	require.NoError(t, runtime.Start(context.Background(), "polling-run"))
	_, _, err := runtime.Call("polling-run", "action", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	require.NoError(t, err)
	caller.reply = commandReply("applied")
	_, report, err := runtime.Call("polling-run", "query", "home_get_command", map[string]any{"command_id": "cmd1"})
	require.NoError(t, err)
	require.NotNil(t, report)
	require.Equal(t, "applied", report.Status)
	pending, err := ledger.Pending(context.Background())
	require.NoError(t, err)
	require.Empty(t, pending)
	require.Equal(t, []string{"home_refresh_screen", "home_get_command"}, caller.names)
}
