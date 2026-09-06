package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Default I/O bounds when the configuration leaves them unset.
const (
	DefaultIOTimeout   = 20 * time.Second
	DefaultMaxInflight = 8
)

// OSFS reads a real directory tree through the hardened FS contract.
//
// Every call runs in a helper goroutine because a blocked SMB syscall cannot
// be interrupted: the caller returns ErrStuck at the deadline while the helper
// stays parked in the kernel and keeps holding its semaphore slot. Once
// max_inflight slots leak the source is marked degraded and refuses further
// work until a probe succeeds, which is what keeps the API responsive when a
// share hangs (design §6.1, D11).
type OSFS struct {
	root    string
	timeout time.Duration

	// slots bounds normal I/O; probeSlots reserves capacity so a health probe
	// can still run while every normal slot is leaked to a stuck syscall.
	slots      chan struct{}
	probeSlots chan struct{}

	inflight        atomic.Int64
	stuckOps        atomic.Int64
	skippedSymlinks atomic.Int64
	errorOps        atomic.Int64
	degraded        atomic.Bool
}

// OSFSOptions configures an OSFS.
type OSFSOptions struct {
	Root        string
	IOTimeout   time.Duration
	MaxInflight int
}

// NewOSFS builds a filesystem rooted at an absolute directory.
func NewOSFS(opts OSFSOptions) (*OSFS, error) {
	if opts.Root == "" {
		return nil, errors.New("source: root is required")
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("source: resolve root: %w", err)
	}
	if opts.IOTimeout <= 0 {
		opts.IOTimeout = DefaultIOTimeout
	}
	if opts.MaxInflight <= 0 {
		opts.MaxInflight = DefaultMaxInflight
	}
	return &OSFS{
		root:       filepath.Clean(root),
		timeout:    opts.IOTimeout,
		slots:      make(chan struct{}, opts.MaxInflight),
		probeSlots: make(chan struct{}, 1),
	}, nil
}

// Root returns the absolute root path. It is never sent to clients or logged
// above debug level.
func (f *OSFS) Root() string { return f.root }

// Stats snapshots the operational counters.
func (f *OSFS) Stats() Stats {
	return Stats{
		StuckOps:        f.stuckOps.Load(),
		SkippedSymlinks: f.skippedSymlinks.Load(),
		ErrorOps:        f.errorOps.Load(),
		Inflight:        int(f.inflight.Load()),
		Degraded:        f.degraded.Load(),
	}
}

// Degraded reports whether the inflight cap has tripped.
func (f *OSFS) Degraded() bool { return f.degraded.Load() }

// ClearDegraded re-enables normal I/O after a successful probe.
func (f *OSFS) ClearDegraded() { f.degraded.Store(false) }

// abs resolves a validated relative path inside the root.
func (f *OSFS) abs(rel string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	if clean == "" {
		return f.root, nil
	}
	full := filepath.Join(f.root, filepath.FromSlash(clean))
	// filepath.Join already cleans, but containment is asserted explicitly so
	// a future change to the cleaning rules cannot silently widen the scope.
	if full != f.root && !strings.HasPrefix(full, f.root+string(os.PathSeparator)) {
		return "", ErrUnsafePath
	}
	return full, nil
}

// callKind selects which semaphore an operation draws from.
type callKind int

const (
	callNormal callKind = iota
	callProbe
)

// do runs fn under the I/O deadline in a helper goroutine.
//
// The helper releases the semaphore slot itself, so a syscall that never
// returns keeps its slot forever. That is intentional: the leak is bounded by
// max_inflight and is exactly the signal that trips the source into degraded.
func (f *OSFS) do(ctx context.Context, kind callKind, fn func() error) error {
	if kind == callNormal && f.degraded.Load() {
		return ErrDegraded
	}
	slots := f.slots
	if kind == callProbe {
		slots = f.probeSlots
	}
	select {
	case slots <- struct{}{}:
	default:
		if kind == callNormal {
			f.degraded.Store(true)
		}
		return ErrDegraded
	}
	f.inflight.Add(1)

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		defer func() {
			f.inflight.Add(-1)
			<-slots
		}()
		done <- fn()
	}()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			f.errorOps.Add(1)
		}
		return err
	case <-ctx.Done():
		f.stuckOps.Add(1)
		f.errorOps.Add(1)
		return ErrStuck
	}
}

// lstat returns metadata without following the final symlink.
func (f *OSFS) lstat(ctx context.Context, kind callKind, rel string) (fs.FileInfo, error) {
	full, err := f.abs(rel)
	if err != nil {
		return nil, err
	}
	var info fs.FileInfo
	err = f.do(ctx, kind, func() error {
		var lerr error
		info, lerr = os.Lstat(full)
		return lerr
	})
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		f.skippedSymlinks.Add(1)
		return nil, ErrSymlink
	}
	return info, nil
}

// Stat implements FS.
func (f *OSFS) Stat(ctx context.Context, rel string) (fs.FileInfo, error) {
	return f.lstat(ctx, callNormal, rel)
}

// ReadDir implements FS. Entries are returned unsorted-by-kind but sorted by
// name, as os.ReadDir does; symlinked entries are kept in the listing so the
// caller can count them, and refusing to follow them happens on Stat/Open.
func (f *OSFS) ReadDir(ctx context.Context, rel string) ([]fs.DirEntry, error) {
	full, err := f.abs(rel)
	if err != nil {
		return nil, err
	}
	var entries []fs.DirEntry
	err = f.do(ctx, callNormal, func() error {
		var rerr error
		entries, rerr = os.ReadDir(full)
		return rerr
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// Open implements FS. The file is opened read-only with O_NOFOLLOW where the
// platform supports it, which closes the window between the symlink check and
// the open.
func (f *OSFS) Open(ctx context.Context, rel string) (io.ReadSeekCloser, error) {
	full, err := f.abs(rel)
	if err != nil {
		return nil, err
	}
	var file *os.File
	err = f.do(ctx, callNormal, func() error {
		var oerr error
		//nolint:gosec // full is validated by abs(): contained in root, no "..", no NUL
		file, oerr = os.OpenFile(full, os.O_RDONLY|openNoFollow, 0)
		return oerr
	})
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("source: stat opened file: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, ErrSymlink
	}
	return file, nil
}

// Statfs implements FS using the probe semaphore so capacity can still be
// read while normal I/O is degraded.
func (f *OSFS) Statfs(ctx context.Context) (VolumeStats, error) {
	var stats VolumeStats
	err := f.do(ctx, callProbe, func() error {
		var serr error
		stats, serr = statfs(f.root)
		return serr
	})
	if err != nil {
		return VolumeStats{}, err
	}
	return stats, nil
}

// ProbeStat stats the root through the reserved probe slot, bypassing the
// degraded gate so a recovered share can be detected.
func (f *OSFS) ProbeStat(ctx context.Context) (fs.FileInfo, error) {
	return f.lstat(ctx, callProbe, "")
}

// PathResolver is implemented by filesystems backed by real files. It exists
// so an external tool such as sips can be pointed at the file directly
// instead of streaming a 20 MB HEIC through a temporary copy. The returned
// path is a NAS path and must never be logged above debug or sent to a client.
type PathResolver interface {
	Realpath(rel string) (string, error)
}

// Realpath implements PathResolver.
func (f *OSFS) Realpath(rel string) (string, error) { return f.abs(rel) }
