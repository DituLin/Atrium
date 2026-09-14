package video

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type streamFixture struct {
	*bytes.Reader
	blockRead  <-chan struct{}
	blockClose <-chan struct{}
	closed     atomic.Bool
}

func (f *streamFixture) Read(p []byte) (int, error) {
	if f.blockRead != nil {
		<-f.blockRead
	}
	return f.Reader.Read(p)
}
func (f *streamFixture) Close() error {
	if f.blockClose != nil {
		<-f.blockClose
	}
	f.closed.Store(true)
	return nil
}

func TestStreamReadSeekAndClose(t *testing.T) {
	pool, err := NewReadPool(1, time.Second)
	require.NoError(t, err)
	file := &streamFixture{Reader: bytes.NewReader([]byte("abcdef"))}
	stream, err := pool.Open(context.Background(), func(context.Context) (io.ReadSeekCloser, error) { return file, nil }, func(context.Context) error { return nil })
	require.NoError(t, err)
	offset, err := stream.Seek(2, io.SeekStart)
	require.NoError(t, err)
	require.EqualValues(t, 2, offset)
	data, err := io.ReadAll(stream)
	require.NoError(t, err)
	require.Equal(t, "cdef", string(data))
	require.NoError(t, stream.Close())
	require.Eventually(t, func() bool { return file.closed.Load() && len(pool.slots) == 0 }, time.Second, time.Millisecond)
}

func TestStreamTimeoutRetainsCapacityUntilActualReadEnds(t *testing.T) {
	pool, err := NewReadPool(1, 25*time.Millisecond)
	require.NoError(t, err)
	release := make(chan struct{})
	file := &streamFixture{Reader: bytes.NewReader([]byte("abc")), blockRead: release}
	opener := func(context.Context) (io.ReadSeekCloser, error) { return file, nil }
	stream, err := pool.Open(context.Background(), opener, func(context.Context) error { return nil })
	require.NoError(t, err)
	buffer := []byte("unchanged")
	_, err = stream.Read(buffer)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, "unchanged", string(buffer), "timed out I/O must never mutate caller buffer")
	_, err = pool.Open(context.Background(), opener, func(context.Context) error { return nil })
	require.ErrorIs(t, err, ErrStreamBusy)
	require.NoError(t, stream.Close())
	require.False(t, file.closed.Load(), "blocked read still owns file")
	close(release)
	require.Eventually(t, func() bool { return file.closed.Load() && len(pool.slots) == 0 }, time.Second, time.Millisecond)
	require.Equal(t, "unchanged", string(buffer))
}

func TestStreamRechecksAuthorityAfterRead(t *testing.T) {
	pool, err := NewReadPool(1, time.Second)
	require.NoError(t, err)
	file := &streamFixture{Reader: bytes.NewReader([]byte("private"))}
	calls := 0
	stream, err := pool.Open(context.Background(), func(context.Context) (io.ReadSeekCloser, error) { return file, nil }, func(context.Context) error {
		calls++
		if calls >= 2 {
			return ErrStale
		}
		return nil
	})
	require.NoError(t, err)
	data := make([]byte, 10)
	n, err := stream.Read(data)
	require.ErrorIs(t, err, ErrStale)
	require.Zero(t, n)
	require.Equal(t, make([]byte, 10), data)
	require.NoError(t, stream.Close())
}

func TestStreamOpenTimeoutClosesLateResult(t *testing.T) {
	pool, err := NewReadPool(1, 25*time.Millisecond)
	require.NoError(t, err)
	release := make(chan struct{})
	file := &streamFixture{Reader: bytes.NewReader(nil)}
	opener := func(context.Context) (io.ReadSeekCloser, error) { <-release; return file, nil }
	_, err = pool.Open(context.Background(), opener, func(context.Context) error { return nil })
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = pool.Open(context.Background(), opener, func(context.Context) error { return nil })
	require.ErrorIs(t, err, ErrStreamBusy)
	close(release)
	require.Eventually(t, func() bool { return file.closed.Load() && len(pool.slots) == 0 }, time.Second, time.Millisecond)
}

func TestStreamCloseRemainsCountedWhileKernelCleanupBlocks(t *testing.T) {
	pool, err := NewReadPool(1, time.Second)
	require.NoError(t, err)
	release := make(chan struct{})
	file := &streamFixture{Reader: bytes.NewReader(nil), blockClose: release}
	opener := func(context.Context) (io.ReadSeekCloser, error) { return file, nil }
	stream, err := pool.Open(context.Background(), opener, func(context.Context) error { return nil })
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	_, err = pool.Open(context.Background(), opener, func(context.Context) error { return nil })
	require.ErrorIs(t, err, ErrStreamBusy)
	close(release)
	require.Eventually(t, func() bool { return file.closed.Load() && len(pool.slots) == 0 }, time.Second, time.Millisecond)
}
