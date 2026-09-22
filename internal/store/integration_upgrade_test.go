package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestIntegrationUpgradePreservesVersionOneData(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "v1.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	old, err := migrationFS.ReadFile("migrations/0001_init.sql")
	require.NoError(t, err)
	_, err = db.SQL().ExecContext(ctx, string(old))
	require.NoError(t, err)
	_, err = db.SQL().ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL);INSERT INTO schema_migrations VALUES(1,'0001_init.sql','2026-09-05T00:00:00Z')`)
	require.NoError(t, err)
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Settings().Set(ctx, "existing-setting", "preserved"))
	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{ID: "existing-tv", Name: "Original TV", TokenHash: "original-screen-hash", Status: domain.ScreenActive, CreatedAt: now, ApprovedAt: now}))
	cmd := &domain.Command{ScreenID: "existing-tv", Kind: domain.CommandNavigate, Payload: domain.CommandPayload{Route: domain.RouteDashboard}, IssuedBy: "admin:old", IssuedAt: now, ExpiresAt: now.Add(time.Second), Status: domain.CommandAccepted}
	require.NoError(t, db.Commands().Issue(ctx, cmd))
	n, err := db.Migrate(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, n)
	value, err := db.Settings().Get(ctx, "existing-setting")
	require.NoError(t, err)
	require.Equal(t, "preserved", value)
	sc, err := db.Screens().Get(ctx, "existing-tv")
	require.NoError(t, err)
	require.Equal(t, "original-screen-hash", sc.TokenHash)
	require.EqualValues(t, 1, sc.LastSequence)
	saved, err := db.Commands().Get(ctx, cmd.ID)
	require.NoError(t, err)
	require.Equal(t, "admin:old", saved.IssuedBy)
	principals, err := db.Integrations().List(ctx)
	require.NoError(t, err)
	require.Empty(t, principals)
	n, err = db.Migrate(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}
