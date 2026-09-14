package source

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProbeWaitsForConcurrentProbeWithinDeadline(t *testing.T) {
	f, err := NewOSFS(OSFSOptions{Root: t.TempDir(), IOTimeout: time.Second, MaxInflight: 1})
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- f.do(context.Background(), callProbe, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	second := make(chan error, 1)
	go func() { second <- f.do(context.Background(), callProbe, func() error { return nil }) }()
	select {
	case err := <-second:
		close(release)
		require.NoError(t, err)
		t.Fatal("second probe should wait for the first")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-first)
	require.NoError(t, <-second)
	require.Zero(t, f.Stats().StuckOps)
}
