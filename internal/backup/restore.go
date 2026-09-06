package backup

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // database/sql driver, for verifying a snapshot
)

// RestoreOptions describes one restore.
type RestoreOptions struct {
	// From is the snapshot file to install.
	From string
	// DataDir holds the live database and the server lock.
	DataDir string
	// DBPath is the database file to replace.
	DBPath string
	Now    func() time.Time
}

// RestoreResult reports what the operation moved.
type RestoreResult struct {
	// PreservedPath is where the previous database was moved, never deleted.
	PreservedPath string
	// Migrations is the schema version found in the snapshot.
	Migrations int
}

// Restore installs a snapshot as the live database. The server must be
// stopped: the advisory lock is checked first, and the current database is
// moved aside rather than removed so a mistaken restore is reversible
// (design §6.10).
func Restore(ctx context.Context, opts RestoreOptions) (RestoreResult, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	if Held(opts.DataDir) {
		return RestoreResult{}, fmt.Errorf(
			"backup: a server is running on %s; stop it before restoring", filepath.Base(opts.DataDir))
	}
	migrations, err := verifySnapshot(ctx, opts.From)
	if err != nil {
		return RestoreResult{}, err
	}

	stamp := now().UTC().Format(FileStamp)
	preserved := opts.DBPath + ".pre-restore-" + stamp
	if _, err := os.Stat(opts.DBPath); err == nil {
		if err := os.Rename(opts.DBPath, preserved); err != nil {
			return RestoreResult{}, fmt.Errorf("backup: preserve current database: %w", err)
		}
		// The write-ahead log and shared-memory files belong to the database
		// that just moved; leaving them behind would corrupt the restored one.
		for _, suffix := range []string{"-wal", "-shm"} {
			_ = os.Rename(opts.DBPath+suffix, preserved+suffix)
		}
	} else {
		preserved = ""
	}

	if err := copyFile(opts.From, opts.DBPath); err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{PreservedPath: preserved, Migrations: migrations}, nil
}

// verifySnapshot proves the file is a readable Atrium database before anything
// is moved, so a typo in --from cannot cost the live database.
func verifySnapshot(ctx context.Context, path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("backup: read snapshot: %w", err)
	}
	if info.IsDir() || info.Size() == 0 {
		return 0, fmt.Errorf("backup: %s is not a database file", filepath.Base(path))
	}
	db, err := sql.Open("sqlite", path+"?_pragma=query_only(1)")
	if err != nil {
		return 0, fmt.Errorf("backup: open snapshot: %w", err)
	}
	defer func() { _ = db.Close() }()

	var check string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); err != nil {
		return 0, fmt.Errorf("backup: integrity check: %w", err)
	}
	if check != "ok" {
		return 0, fmt.Errorf("backup: snapshot failed its integrity check: %s", check)
	}
	var migrations int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil {
		return 0, fmt.Errorf("backup: %s is not an atrium snapshot: %w", filepath.Base(path), err)
	}
	return migrations, nil
}

func copyFile(from, to string) error {
	src, err := os.Open(from) //nolint:gosec // an operator-supplied snapshot path
	if err != nil {
		return fmt.Errorf("backup: open snapshot: %w", err)
	}
	defer func() { _ = src.Close() }()
	//nolint:gosec // `to` is the configured database path, not user input
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("backup: create database: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return fmt.Errorf("backup: write database: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("backup: close database: %w", err)
	}
	return nil
}
