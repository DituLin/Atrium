package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestVideoUpgradePreservesExistingMediaAndScreen(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "v3.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.SQL().ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL)`)
	require.NoError(t, err)
	migrations, err := Migrations()
	require.NoError(t, err)
	for _, m := range migrations {
		if m.Version <= 3 {
			require.NoError(t, db.applyMigration(ctx, m))
		}
	}
	now := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Sources().Upsert(ctx, "mixed", "family", "/authorized", now))
	photo := &domain.Photo{ID: "existing-photo", SourceID: "mixed", RelPath: "a.jpg", Ext: "jpg", SizeBytes: 123, Status: domain.PhotoReady, FirstSeenAt: now, LastSeenAt: now, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Photos().Insert(ctx, photo))
	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: "preserved", Status: domain.ScreenActive, CreatedAt: now, ApprovedAt: now}))
	before, err := db.Photos().Get(ctx, photo.ID)
	require.NoError(t, err)
	n, err := db.Migrate(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	after, err := db.Photos().Get(ctx, photo.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	screen, err := db.Screens().Get(ctx, "tv")
	require.NoError(t, err)
	require.Equal(t, "preserved", screen.TokenHash)
	var count int
	require.NoError(t, db.SQL().QueryRowContext(ctx, `SELECT count(*) FROM videos`).Scan(&count))
	require.Zero(t, count)
	n, err = db.Migrate(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}
