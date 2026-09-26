package main

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStandardPipeCancellationWithPeerOpen(t *testing.T) {
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "write"}[write], func(t *testing.T) {
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			defer func() { _ = reader.Close() }()
			defer func() { _ = writer.Close() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fd := reader.Fd()
			if write {
				fd = writer.Fd()
			}
			pipe, err := duplicatePipe(ctx, int(fd))
			require.NoError(t, err)
			defer func() { _ = pipe.Close() }()
			done := make(chan error, 1)
			go func() {
				var err error
				if write {
					_, err = pipe.Write(make([]byte, 1024*1024))
				} else {
					_, err = pipe.Read(make([]byte, 1))
				}
				done <- err
			}()
			cancel()
			select {
			case err := <-done:
				require.True(t, errors.Is(err, context.Canceled))
			case <-time.After(time.Second):
				t.Fatal("pipe cancellation stuck")
			}
		})
	}
}
func TestStandardPipeEOF(t *testing.T) {
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	pipe, err := duplicatePipe(context.Background(), int(reader.Fd()))
	require.NoError(t, err)
	defer func() { _ = pipe.Close() }()
	require.NoError(t, writer.Close())
	_, err = pipe.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
}
