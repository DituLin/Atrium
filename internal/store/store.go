// Package store owns the SQLite database: connection, migrations and
// repositories. Nothing outside this package writes SQL.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps the SQLite handle with Atrium-specific helpers.
type DB struct {
	sql  *sql.DB
	path string
}

// Open opens (creating if needed) the SQLite database at path and applies the
// pragmas required by the design (§4.2).
func Open(ctx context.Context, path string) (*DB, error) {
	// _txlock=immediate makes every transaction take the write lock at BEGIN.
	// A deferred transaction that reads and then writes fails with
	// SQLITE_BUSY_SNAPSHOT (517) as soon as another connection commits in
	// between, and no busy timeout can recover it; taking the lock up front
	// turns that into an ordinary wait bounded by busy_timeout.
	dsn := path + "?_txlock=immediate" +
		"&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", filepath.Base(path), err)
	}
	// SQLite tolerates concurrent readers but a single writer; a small pool with
	// WAL keeps write contention predictable.
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	db := &DB{sql: sqlDB, path: path}
	if err := db.verifyPragmas(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// OpenMemory opens a private in-memory database, used by tests.
func OpenMemory(ctx context.Context) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", "file::memory:?_txlock=immediate&_pragma=foreign_keys(ON)&cache=shared")
	if err != nil {
		return nil, fmt.Errorf("store: open memory: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("store: ping memory: %w", err)
	}
	return &DB{sql: sqlDB, path: ":memory:"}, nil
}

func (d *DB) verifyPragmas(ctx context.Context) error {
	var mode string
	if err := d.sql.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("store: read journal_mode: %w", err)
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("store: journal_mode is %q, want wal", mode)
	}
	var fk int
	if err := d.sql.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return fmt.Errorf("store: read foreign_keys: %w", err)
	}
	if fk != 1 {
		return fmt.Errorf("store: foreign_keys is off")
	}
	return nil
}

// SQL exposes the underlying handle for maintenance operations (backup, vacuum).
func (d *DB) SQL() *sql.DB { return d.sql }

// Path returns the database file path.
func (d *DB) Path() string { return d.path }

// Close closes the database, checkpointing the WAL first.
func (d *DB) Close() error {
	if d.path != ":memory:" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = d.sql.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	}
	if err := d.sql.Close(); err != nil {
		return fmt.Errorf("store: close: %w", err)
	}
	return nil
}

// Checkpoint truncates the WAL.
func (d *DB) Checkpoint(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("store: checkpoint: %w", err)
	}
	return nil
}

// Migration is one embedded SQL file.
type Migration struct {
	Version int
	Name    string
	Body    string
}

// Migrations lists the embedded migrations in application order.
func Migrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: read migrations: %w", err)
	}
	out := make([]Migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		name := e.Name()
		var version int
		if _, err := fmt.Sscanf(name, "%04d_", &version); err != nil {
			return nil, fmt.Errorf("store: migration %s has no NNNN_ prefix: %w", name, err)
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("store: read migration %s: %w", name, err)
		}
		out = append(out, Migration{Version: version, Name: name, Body: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Migrate applies all pending migrations and returns the number applied.
func (d *DB) Migrate(ctx context.Context) (int, error) {
	if _, err := d.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return 0, fmt.Errorf("store: create schema_migrations: %w", err)
	}
	applied := map[int]struct{}{}
	rows, err := d.sql.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return 0, fmt.Errorf("store: read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return 0, fmt.Errorf("store: scan schema_migrations: %w", err)
		}
		applied[v] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: iterate schema_migrations: %w", err)
	}

	migrations, err := Migrations()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, m := range migrations {
		if _, ok := applied[m.Version]; ok {
			continue
		}
		if err := d.applyMigration(ctx, m); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (d *DB) applyMigration(ctx context.Context, m Migration) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin migration %s: %w", m.Name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.Body); err != nil {
		return fmt.Errorf("store: apply migration %s: %w", m.Name, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
		m.Version, m.Name, FormatTime(time.Now())); err != nil {
		return fmt.Errorf("store: record migration %s: %w", m.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit migration %s: %w", m.Name, err)
	}
	return nil
}

// SchemaVersion returns the highest applied migration version.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := d.sql.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("store: schema version: %w", err)
	}
	return int(v.Int64), nil
}

// AppliedMigrations counts the applied migrations.
func (d *DB) AppliedMigrations(ctx context.Context) (int, error) {
	var n int
	if err := d.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count migrations: %w", err)
	}
	return n, nil
}

// InTx runs fn inside a transaction, rolling back on error.
func (d *DB) InTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
