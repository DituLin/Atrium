package video

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestToolProcessBoundsAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires Unix")
	}
	f, err := os.CreateTemp(t.TempDir(), "input")
	require.NoError(t, err)
	defer f.Close()
	_, err = runTool(context.Background(), "/bin/sh", []string{"-c", "while :; do printf 1234567890; done"}, f, 32)
	require.ErrorIs(t, err, ErrOutputLimit)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = runTool(ctx, "/bin/sh", []string{"-c", "exec sleep 10"}, f, 32)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	require.Less(t, time.Since(start), 2*time.Second)
	_, err = runTool(context.Background(), filepath.Join(t.TempDir(), "absent"), nil, f, 32)
	require.ErrorIs(t, err, ErrToolUnavailable)
	_, err = runTool(context.Background(), "/bin/sh", []string{"-c", "echo private-path >&2; exit 1"}, f, 32)
	require.ErrorIs(t, err, ErrToolFailed)
	require.NotContains(t, err.Error(), "private-path")
}

func TestWaitKeepsCapacityUntilProcessActuallyExits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	exit := make(chan struct{})
	released := make(chan struct{})
	cancel()
	err := waitProcess(ctx, func() error { <-exit; return nil }, func() { close(released) })
	require.ErrorIs(t, err, context.Canceled)
	select {
	case <-released:
		t.Fatal("capacity released while process still alive")
	default:
	}
	close(exit)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("process not reaped")
	}
}
