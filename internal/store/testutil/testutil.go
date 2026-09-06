// Package testutil provides a migrated temporary database for tests.
package testutil

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DituLin/Atrium/internal/store"
)

// NewDB opens a migrated SQLite database in a temporary directory and closes it
// when the test finishes.
func NewDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "atrium.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	return db
}
