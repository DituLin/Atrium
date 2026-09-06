// Package app wires the dependencies together and owns the process lifecycle.
package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// Subdirectories of the data directory (design §4.2 step 2).
const (
	CacheDir   = "cache"
	LogsDir    = "logs"
	TLSDir     = "tls"
	BackupsDir = "backups"
	DBFileName = "atrium.db"
)

// Layout resolves the paths under a data directory.
type Layout struct {
	Root string
}

// NewLayout builds a layout for a data directory.
func NewLayout(root string) Layout { return Layout{Root: root} }

// Cache is the preview cache directory.
func (l Layout) Cache() string { return filepath.Join(l.Root, CacheDir) }

// Logs is the log directory.
func (l Layout) Logs() string { return filepath.Join(l.Root, LogsDir) }

// TLS is the certificate directory.
func (l Layout) TLS() string { return filepath.Join(l.Root, TLSDir) }

// Backups is the backup directory.
func (l Layout) Backups() string { return filepath.Join(l.Root, BackupsDir) }

// DB is the SQLite database path.
func (l Layout) DB() string { return filepath.Join(l.Root, DBFileName) }

// Ensure creates the directory tree with restrictive permissions and verifies
// that an existing tree is not world readable.
func (l Layout) Ensure() error {
	dirs := []string{l.Root, l.Cache(), l.Logs(), l.TLS(), l.Backups()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("app: create %s: %w", d, err)
		}
		// 0700 is deliberate: these are directories, so the execute bit is
		// required for the owner to traverse them.
		if err := os.Chmod(d, 0o700); err != nil { //nolint:gosec // directory, not a file
			return fmt.Errorf("app: set permissions on %s: %w", d, err)
		}
	}
	return nil
}
