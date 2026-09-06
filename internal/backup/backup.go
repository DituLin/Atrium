// Package backup implements the daily SQLite snapshot of technical design
// §6.10: `VACUUM INTO` a directory outside the data dir and every source root,
// a copy of the configuration beside it, and a bounded retention.
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileStamp is the timestamp format used in backup file names.
const FileStamp = "20060102-150405"

// filePrefix and fileSuffix bracket a generated snapshot name.
const (
	filePrefix = "atrium-"
	fileSuffix = ".db"
)

// Options configures a Service.
type Options struct {
	// Dir is backup.dir; validation has already proved it sits outside the
	// data directory and every source root.
	Dir string
	// Keep is backup.keep (7).
	Keep int
	// ConfigPath is copied beside each snapshot so a restore has both halves.
	ConfigPath string
	DB         *sql.DB
	Logger     *slog.Logger
	Now        func() time.Time
}

// Service creates and lists snapshots.
type Service struct {
	opts Options
}

// New builds a backup service.
func New(opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Keep <= 0 {
		opts.Keep = 7
	}
	return &Service{opts: opts}
}

// Enabled reports whether a destination is configured.
func (s *Service) Enabled() bool { return s.opts.Dir != "" }

// Snapshot describes one stored backup.
type Snapshot struct {
	Name       string    `json:"name"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	ConfigCopy bool      `json:"config_copy"`
	// Path is only returned to local callers (the CLI), never over the API.
	Path string `json:"-"`
}

// Run takes one snapshot and applies retention. `VACUUM INTO` reads a
// consistent snapshot through the normal connection, which is what makes it
// safe while the server is writing (PRD §8.3).
func (s *Service) Run(ctx context.Context) (Snapshot, error) {
	if !s.Enabled() {
		return Snapshot{}, fmt.Errorf("backup: backup.dir is not configured")
	}
	if err := os.MkdirAll(s.opts.Dir, 0o700); err != nil {
		return Snapshot{}, fmt.Errorf("backup: create directory: %w", err)
	}
	name := filePrefix + s.opts.Now().Format(FileStamp) + fileSuffix
	path := filepath.Join(s.opts.Dir, name)
	if _, err := os.Stat(path); err == nil {
		return Snapshot{}, fmt.Errorf("backup: %s already exists", name)
	}

	// The destination is a literal in SQLite's grammar, so it is quoted here
	// rather than bound; the path comes from validated configuration.
	//nolint:gosec // VACUUM INTO takes a literal, not a bind parameter; the
	// path comes from validated configuration and single quotes are escaped.
	stmt := "VACUUM INTO '" + strings.ReplaceAll(path, "'", "''") + "'"
	if _, err := s.opts.DB.ExecContext(ctx, stmt); err != nil {
		return Snapshot{}, fmt.Errorf("backup: vacuum into snapshot: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("backup: stat snapshot: %w", err)
	}
	snap := Snapshot{Name: name, SizeBytes: info.Size(), CreatedAt: info.ModTime(), Path: path}
	snap.ConfigCopy = s.copyConfig(name) == nil

	if err := s.prune(); err != nil {
		s.opts.Logger.Warn("backup retention failed", "component", "backup",
			"event", "retention_failed", "error", err.Error())
	}
	s.opts.Logger.Info("backup written", "component", "backup", "event", "backup_written",
		"name", name, "size_bytes", snap.SizeBytes, "config_copy", snap.ConfigCopy)
	return snap, nil
}

// copyConfig stores the configuration next to the snapshot so a restore does
// not depend on the running host still having it.
func (s *Service) copyConfig(snapshotName string) error {
	if s.opts.ConfigPath == "" {
		return nil
	}
	src, err := os.Open(s.opts.ConfigPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	target := filepath.Join(s.opts.Dir, strings.TrimSuffix(snapshotName, fileSuffix)+".yaml")
	//nolint:gosec // target is built from backup.dir, which config validation
	// proved is outside the data dir and every source root
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Close()
}

// List returns the stored snapshots, newest first.
func (s *Service) List() ([]Snapshot, error) {
	if !s.Enabled() {
		return nil, nil
	}
	entries, err := os.ReadDir(s.opts.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("backup: read directory: %w", err)
	}
	out := make([]Snapshot, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(s.opts.Dir, name)
		cfg := filepath.Join(s.opts.Dir, strings.TrimSuffix(name, fileSuffix)+".yaml")
		_, cfgErr := os.Stat(cfg)
		out = append(out, Snapshot{
			Name: name, SizeBytes: info.Size(), CreatedAt: info.ModTime(),
			ConfigCopy: cfgErr == nil, Path: path,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// prune deletes the oldest snapshots past backup.keep, with their config copy.
func (s *Service) prune() error {
	snaps, err := s.List()
	if err != nil {
		return err
	}
	if len(snaps) <= s.opts.Keep {
		return nil
	}
	for _, old := range snaps[s.opts.Keep:] {
		if err := os.Remove(old.Path); err != nil {
			return fmt.Errorf("backup: remove %s: %w", old.Name, err)
		}
		_ = os.Remove(strings.TrimSuffix(old.Path, fileSuffix) + ".yaml")
	}
	return nil
}
