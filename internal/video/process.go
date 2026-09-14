package video

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"time"
)

// Tool errors contain no source paths, command output, or subprocess arguments.
var (
	ErrOutputLimit     = errors.New("video: tool output limit exceeded")
	ErrToolUnavailable = errors.New("video: tool unavailable")
	ErrToolFailed      = errors.New("video: tool failed")
	ErrToolBusy        = errors.New("video: tool capacity exhausted")
)

// Hung kernel reads continue occupying capacity until the child is reaped.
var processSlots = make(chan struct{}, 2)

type limitedOutput struct {
	data     bytes.Buffer
	limit    int
	exceeded atomic.Bool
	cancel   context.CancelFunc
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.data.Len() {
		b.exceeded.Store(true)
		b.cancel()
		return 0, ErrOutputLimit
	}
	return b.data.Write(p)
}

// runTool inherits only the already authorized input descriptor. The caller
// keeps ownership and must open a fresh descriptor for each operation because
// the subprocess shares its seek position. No shell is used by production calls.
func runTool(ctx context.Context, binary string, args []string, input *os.File, limit int) ([]byte, error) {
	if input == nil || limit <= 0 {
		return nil, ErrToolFailed
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := bounded.Err(); err != nil {
		return nil, err
	}
	select {
	case processSlots <- struct{}{}:
	default:
		return nil, ErrToolBusy
	}
	release := func() { <-processSlots }
	stdout := &limitedOutput{limit: limit, cancel: cancel}
	stderr := &limitedOutput{limit: 64 * 1024, cancel: cancel}
	cmd := exec.CommandContext(bounded, binary, args...) //nolint:gosec // configured executable; fixed arguments, no shell
	cmd.WaitDelay = time.Second
	cmd.ExtraFiles = []*os.File{input}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Start()
	if err != nil {
		release()
	} else {
		err = waitProcess(bounded, cmd.Wait, release)
	}
	if stdout.exceeded.Load() || stderr.exceeded.Load() {
		return nil, ErrOutputLimit
	}
	if bounded.Err() != nil {
		return nil, bounded.Err()
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) {
			return nil, ErrToolUnavailable
		}
		return nil, ErrToolFailed
	}
	return stdout.data.Bytes(), nil
}

// waitProcess bounds the caller independently of Process.Wait. The wait
// goroutine owns the slot until the child actually exits, even after timeout.
func waitProcess(ctx context.Context, wait func() error, release func()) error {
	done := make(chan error, 1)
	go func() { err := wait(); release(); done <- err }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
