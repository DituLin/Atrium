package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPhotoReadIndexesUpgradeAndVisibility(t *testing.T) {
	ctx := context.Background()
	db, err := OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.SQL().ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL)`)
	require.NoError(t, err)
	migrations, err := Migrations()
	require.NoError(t, err)
	for _, m := range migrations {
		if m.Version <= 5 {
			require.NoError(t, db.applyMigration(ctx, m))
		}
	}
	now := time.Now()
	require.NoError(t, db.Sources().Upsert(ctx, "mixed", "family", "/authorized", now))
	photo := &domain.Photo{ID: "retained", SourceID: "mixed", RelPath: "existing.jpg", Ext: "jpg", SizeBytes: 42, Status: domain.PhotoReady, PreviewStatus: domain.PreviewReady, CapturedConfidence: domain.CapturedUnknown, FirstSeenAt: now, LastSeenAt: now, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Photos().Insert(ctx, photo))
	before, err := db.Photos().Get(ctx, photo.ID)
	require.NoError(t, err)
	n, err := db.Migrate(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "v5 needs the photo read migration")
	after, err := db.Photos().Get(ctx, photo.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	// Guard the large-library access shape without a flaky wall-clock threshold:
	// candidate IDs must be covered and ordered by an index; the count must not
	// fetch every full photo row. These are the screen-facing repository queries.
	queries := []string{
		`SELECT p.id FROM photos p JOIN data_sources s ON s.id=p.source_id WHERE p.status='ready' AND s.status='active' AND p.preview_status='ready' ORDER BY p.id LIMIT 100000`,
		`SELECT COUNT(*) FROM photos p JOIN data_sources s ON s.id=p.source_id WHERE p.status='ready' AND s.status='active' AND p.captured_confidence='unknown'`,
	}
	for _, query := range queries {
		rows, err := db.SQL().QueryContext(ctx, "EXPLAIN QUERY PLAN "+query)
		require.NoError(t, err)
		var plans []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
			plans = append(plans, detail)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		plan := strings.Join(plans, "\n")
		require.Contains(t, plan, "p USING COVERING INDEX")
		require.NotContains(t, plan, "TEMP B-TREE")
	}
	ids, err := db.Photos().ListEligibleIDs(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, []string{photo.ID}, ids)
	count, err := db.Photos().CountUnknownCaptured(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Photos().SetPreviewStatus(ctx, photo.ID, domain.PreviewEvicted, "", now))
	ids, err = db.Photos().ListEligibleIDs(ctx, 100)
	require.NoError(t, err)
	require.Empty(t, ids)
	require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
	count, err = db.Photos().CountUnknownCaptured(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	n, err = db.Migrate(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}
