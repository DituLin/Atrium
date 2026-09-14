package video

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// Stream errors are intentionally independent of source paths and OS messages.
var (
	ErrStreamBusy = errors.New("video: stream capacity exhausted")
	ErrStreamRead = errors.New("video: stream operation failed")
)

const streamChunkSize = 64 * 1024

// ReadPool bounds active streams, including abandoned kernel I/O and close.
// It does not grant authorization; the opener and guard must enforce it.
type ReadPool struct {
	slots   chan struct{}
	timeout time.Duration
}

// NewReadPool configures bounded concurrency and per-operation deadlines.
func NewReadPool(limit int, timeout time.Duration) (*ReadPool, error) {
	if limit < 1 || limit > 8 || timeout <= 0 || timeout > 30*time.Second {
		return nil, errors.New("video: invalid stream limits")
	}
	return &ReadPool{slots: make(chan struct{}, limit), timeout: timeout}, nil
}

type streamRequest struct {
	ctx    context.Context
	size   int
	offset int64
	whence int
	seek   bool
	reply  chan streamResult
}
type streamResult struct {
	data   []byte
	offset int64
	err    error
}

// Stream implements sequential Read/Seek while its actor owns the descriptor.
// Close cancels promptly; the actor releases capacity only after real cleanup.
type Stream struct {
	ctx      context.Context
	cancel   context.CancelFunc
	requests chan streamRequest
	timeout  time.Duration
	mu       sync.Mutex
}

// Open transfers cleanup responsibility immediately to the bounded actor.
// Guard runs before and after each operation, including reads already in flight.
func (p *ReadPool) Open(ctx context.Context, opener func(context.Context) (io.ReadSeekCloser, error), guard func(context.Context) error) (*Stream, error) {
	if opener == nil || guard == nil {
		return nil, ErrStreamRead
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case p.slots <- struct{}{}:
	default:
		return nil, ErrStreamBusy
	}
	life, cancel := context.WithCancel(ctx)
	s := &Stream{ctx: life, cancel: cancel, requests: make(chan streamRequest), timeout: p.timeout}
	opening, stopOpening := context.WithTimeout(life, p.timeout)
	defer stopOpening()
	opened := make(chan error)
	go func() {
		defer func() { <-p.slots }()
		file, err := opener(opening)
		if file != nil {
			defer func() { _ = file.Close() }()
		}
		if err == nil && file == nil {
			err = ErrStreamRead
		}
		select {
		case opened <- streamError(err):
		case <-opening.Done():
			return
		}
		if err != nil {
			return
		}
		for {
			select {
			case <-life.Done():
				return
			case request := <-s.requests:
				result := performStream(request, file, guard)
				request.reply <- result
			}
		}
	}()
	select {
	case err := <-opened:
		if err != nil {
			cancel()
			return nil, err
		}
		return s, nil
	case <-opening.Done():
		cancel()
		return nil, opening.Err()
	}
}

func streamError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, io.EOF):
		return io.EOF
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, ErrStale):
		return ErrStale
	default:
		return ErrStreamRead
	}
}

func performStream(r streamRequest, file io.ReadSeeker, guard func(context.Context) error) streamResult {
	if err := r.ctx.Err(); err != nil {
		return streamResult{err: err}
	}
	if err := guard(r.ctx); err != nil {
		return streamResult{err: streamError(err)}
	}
	result := streamResult{}
	if r.seek {
		result.offset, result.err = file.Seek(r.offset, r.whence)
	} else {
		buffer := make([]byte, r.size)
		n, err := file.Read(buffer)
		if n < 0 || n > len(buffer) {
			return streamResult{err: ErrStreamRead}
		}
		result.data, result.err = buffer[:n], err
	}
	if err := r.ctx.Err(); err != nil {
		return streamResult{err: err}
	}
	if err := guard(r.ctx); err != nil {
		return streamResult{err: streamError(err)}
	}
	result.err = streamError(result.err)
	return result
}

func (s *Stream) call(request streamRequest) streamResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, s.timeout)
	defer cancel()
	request.ctx = ctx
	request.reply = make(chan streamResult, 1)
	select {
	case s.requests <- request:
	case <-ctx.Done():
		s.cancel()
		return streamResult{err: ctx.Err()}
	}
	select {
	case result := <-request.reply:
		if err := ctx.Err(); err != nil {
			s.cancel()
			return streamResult{err: err}
		}
		return result
	case <-ctx.Done():
		s.cancel()
		return streamResult{err: ctx.Err()}
	}
}

// Read copies only completed and authorized bytes into the caller buffer.
func (s *Stream) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	result := s.call(streamRequest{size: min(len(buffer), streamChunkSize)})
	return copy(buffer, result.data), result.err
}

// Seek changes the descriptor offset inside the same bounded actor.
func (s *Stream) Seek(offset int64, whence int) (int64, error) {
	result := s.call(streamRequest{seek: true, offset: offset, whence: whence})
	return result.offset, result.err
}

// Close cancels immediately; the actor retains its slot until cleanup ends.
func (s *Stream) Close() error { s.cancel(); return nil }
