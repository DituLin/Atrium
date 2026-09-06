package backup

import (
	"fmt"
	"os"
	"path/filepath"
)

// LockName is the advisory lock file the running server holds inside the data
// directory. `atrium restore` refuses while it is held, because replacing the
// database under a live process would leave both halves inconsistent.
const LockName = "atrium.lock"

// LockPath returns the lock file path for a data directory.
func LockPath(dataDir string) string { return filepath.Join(dataDir, LockName) }

// Lock is an exclusive advisory lock on a file descriptor. It is released when
// the process exits, including a crash, so a stale lock never blocks recovery.
type Lock struct {
	file *os.File
}

// Acquire takes the server lock. It returns an error if another process holds
// it, which is how a second `atrium serve` on the same data dir is caught.
func Acquire(dataDir string) (*Lock, error) {
	path := LockPath(dataDir)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // inside the 0700 data dir
	if err != nil {
		return nil, fmt.Errorf("backup: open lock file: %w", err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("backup: another atrium process holds %s: %w", LockName, err)
	}
	return &Lock{file: f}, nil
}

// Release drops the lock.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = unlockFile(l.file)
	err := l.file.Close()
	l.file = nil
	if err != nil {
		return fmt.Errorf("backup: close lock file: %w", err)
	}
	return nil
}

// Held reports whether a server currently holds the lock for a data directory.
// It probes by trying to take the lock itself and releasing it immediately.
func Held(dataDir string) bool {
	path := LockPath(dataDir)
	f, err := os.OpenFile(path, os.O_RDWR, 0o600) //nolint:gosec // inside the 0700 data dir
	if err != nil {
		// No lock file at all means no server has ever started here.
		return false
	}
	defer func() { _ = f.Close() }()
	if err := lockFile(f); err != nil {
		return true
	}
	_ = unlockFile(f)
	return false
}
