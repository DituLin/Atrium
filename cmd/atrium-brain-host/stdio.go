package main

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

// contextPipe avoids blocking macOS FIFO reads that Close cannot interrupt.
// Only duplicated process stdio descriptors use this adapter; no model paths.
type contextPipe struct {
	ctx    context.Context
	mu     sync.Mutex
	fd     int
	closed bool
}

func duplicatePipe(ctx context.Context, fd int) (*contextPipe, error) {
	dup, err := syscall.Dup(fd)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(dup)
	if err = syscall.SetNonblock(dup, true); err != nil {
		_ = syscall.Close(dup)
		return nil, err
	}
	return &contextPipe{ctx: ctx, fd: dup}, nil
}
func (p *contextPipe) wait() error {
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (p *contextPipe) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	for {
		if err := p.ctx.Err(); err != nil {
			return 0, err
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return 0, os.ErrClosed
		}
		n, err := syscall.Read(p.fd, b)
		p.mu.Unlock()
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			if err = p.wait(); err != nil {
				return 0, err
			}
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, io.EOF
		}
		return n, nil
	}
}
func (p *contextPipe) Write(b []byte) (int, error) {
	written := 0
	for written < len(b) {
		if err := p.ctx.Err(); err != nil {
			return written, err
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return written, os.ErrClosed
		}
		n, err := syscall.Write(p.fd, b[written:])
		p.mu.Unlock()
		if n > 0 {
			written += n
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			if err = p.wait(); err != nil {
				return written, err
			}
			continue
		}
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}
func (p *contextPipe) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return syscall.Close(p.fd)
}
