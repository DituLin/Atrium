// Package source owns every read of an authorized photo root. It exposes a
// deliberately narrow, read-only filesystem interface (technical design §6.1),
// hardens path handling, never follows symlinks, and bounds each syscall with
// a deadline so a hung network mount cannot stall the server.
package source

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"
)

// FS is the only way any Atrium package reads a source root. It has no write
// operation by construction, which is what makes "read-only NAS" enforceable
// rather than a convention.
type FS interface {
	// Stat returns metadata for rel without following symlinks.
	Stat(ctx context.Context, rel string) (fs.FileInfo, error)
	// ReadDir lists the directory at rel.
	ReadDir(ctx context.Context, rel string) ([]fs.DirEntry, error)
	// Open opens rel for reading only.
	Open(ctx context.Context, rel string) (io.ReadSeekCloser, error)
	// Statfs describes the volume carrying the root.
	Statfs(ctx context.Context) (VolumeStats, error)
}

// VolumeStats describes the filesystem a source root lives on.
type VolumeStats struct {
	// TotalBytes and FreeBytes are zero when the platform cannot report them.
	TotalBytes int64
	FreeBytes  int64
	// FSType is the filesystem name, e.g. "smbfs", "apfs".
	FSType string
	// MountFrom is the device or share the filesystem was mounted from. It is
	// a credential-adjacent string and is never logged or returned by the API;
	// only its hash reaches the database.
	MountFrom string
	// IsMountPoint reports whether the root sits on a different device than
	// its parent directory. It is informational only: an authorized photo
	// directory is normally a subdirectory of the share, so require_mount is
	// decided by FSType (see CheckIdentity), not by this flag.
	IsMountPoint bool
}

// Errors returned by the FS implementations.
var (
	// ErrStuck means a syscall exceeded io_timeout. The helper goroutine may
	// still be blocked in the kernel; it is accounted in Stats().StuckOps.
	ErrStuck = errors.New("source: operation timed out")
	// ErrDegraded means the inflight cap is exhausted, so no new I/O is issued
	// until a probe succeeds.
	ErrDegraded = errors.New("source: too many stuck operations, source degraded")
	// ErrSymlink means the path is a symbolic link, which is never followed.
	ErrSymlink = errors.New("source: symbolic links are not followed")
	// ErrUnsafePath means the relative path escaped the root or contained NUL.
	ErrUnsafePath = errors.New("source: unsafe relative path")
	// ErrNotSupported means the platform cannot answer the request.
	ErrNotSupported = errors.New("source: not supported on this platform")
)

// CleanRel validates a relative path and returns its canonical slash form.
// The empty string and "." both denote the root itself. Absolute paths, NUL
// bytes, Windows-style volume prefixes and any ".." element are rejected
// outright rather than cleaned away, so a traversal attempt is visible as an
// error instead of silently resolving to a different file.
func CleanRel(rel string) (string, error) {
	if strings.ContainsRune(rel, 0) {
		return "", ErrUnsafePath
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if rel == "" || rel == "." {
		return "", nil
	}
	if strings.HasPrefix(rel, "/") {
		return "", ErrUnsafePath
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", ErrUnsafePath
		}
	}
	cleaned := path.Clean(rel)
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", ErrUnsafePath
	}
	return cleaned, nil
}

// JoinRel appends a child name to a parent relative path.
func JoinRel(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

// Stats are the operational counters of one source's I/O layer.
type Stats struct {
	// StuckOps counts syscalls abandoned after io_timeout.
	StuckOps int64
	// SkippedSymlinks counts entries refused because they are symbolic links.
	SkippedSymlinks int64
	// ErrorOps counts failed syscalls other than "not exist".
	ErrorOps int64
	// Inflight is the number of helper goroutines currently in a syscall.
	Inflight int
	// Degraded reports whether the inflight cap tripped.
	Degraded bool
}
