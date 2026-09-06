package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/backup"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
	"github.com/DituLin/Atritum/internal/store/testutil"
)

type stepClock struct{ at time.Time }

func (c *stepClock) Now() time.Time {
	c.at = c.at.Add(time.Second)
	return c.at
}

// seed writes n screens so a restored snapshot has something to compare.
func seed(t *testing.T, db *store.DB, n int) {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		require.NoError(t, db.Screens().Create(t.Context(), &domain.Screen{
			ID: "screen_" + string(rune('a'+i)), Name: "Screen", TokenHash: "hash_" + string(rune('a'+i)),
			Status: domain.ScreenActive, CreatedAt: now, ApprovedAt: now,
		}))
	}
}

func TestBackupProducesAReadableSnapshot(t *testing.T) {
	db := testutil.NewDB(t)
	seed(t, db, 3)
	dir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("home:\n  name: Home\n"), 0o600))

	svc := backup.New(backup.Options{
		Dir: dir, Keep: 7, ConfigPath: cfgPath, DB: db.SQL(),
		Now: (&stepClock{at: time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)}).Now,
	})
	snap, err := svc.Run(context.Background())
	require.NoError(t, err)
	assert.Positive(t, snap.SizeBytes)
	assert.True(t, snap.ConfigCopy, "the configuration is copied beside the snapshot")

	// The snapshot opens as an ordinary database with the same rows.
	restored, err := store.Open(context.Background(), filepath.Join(dir, snap.Name))
	require.NoError(t, err)
	defer func() { _ = restored.Close() }()
	rows, err := restored.Screens().List(context.Background())
	require.NoError(t, err)
	assert.Len(t, rows, 3)

	migrations, err := restored.AppliedMigrations(context.Background())
	require.NoError(t, err)
	original, err := db.AppliedMigrations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, original, migrations)
}

func TestRetentionKeepsTheNewestN(t *testing.T) {
	db := testutil.NewDB(t)
	dir := t.TempDir()
	clk := &stepClock{at: time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)}
	svc := backup.New(backup.Options{Dir: dir, Keep: 3, DB: db.SQL(), Now: clk.Now})

	for i := 0; i < 6; i++ {
		_, err := svc.Run(context.Background())
		require.NoError(t, err)
	}
	snaps, err := svc.List()
	require.NoError(t, err)
	require.Len(t, snaps, 3, "retention keeps exactly backup.keep snapshots")
	// Newest first, and the ones kept are the newest by name (name is the time).
	assert.Greater(t, snaps[0].Name, snaps[1].Name)
	assert.Greater(t, snaps[1].Name, snaps[2].Name)
}

func TestListIsEmptyBeforeTheFirstBackup(t *testing.T) {
	db := testutil.NewDB(t)
	svc := backup.New(backup.Options{Dir: filepath.Join(t.TempDir(), "never"), Keep: 7, DB: db.SQL()})
	snaps, err := svc.List()
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

func TestBackupWithoutADirectoryIsRefused(t *testing.T) {
	db := testutil.NewDB(t)
	svc := backup.New(backup.Options{DB: db.SQL()})
	assert.False(t, svc.Enabled())
	_, err := svc.Run(context.Background())
	require.Error(t, err)
}

// Replacing the database under a live server would leave both halves
// inconsistent, so restore checks the advisory lock first (design §6.10).
func TestRestoreRefusesWhileTheServerHoldsTheLock(t *testing.T) {
	dataDir := t.TempDir()
	lock, err := backup.Acquire(dataDir)
	require.NoError(t, err)
	assert.True(t, backup.Held(dataDir))

	_, err = backup.Restore(context.Background(), backup.RestoreOptions{
		From: filepath.Join(dataDir, "whatever.db"), DataDir: dataDir,
		DBPath: filepath.Join(dataDir, "atrium.db"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stop it before restoring")

	require.NoError(t, lock.Release())
	assert.False(t, backup.Held(dataDir))
}

func TestRestoreInstallsTheSnapshotAndPreservesTheOldDatabase(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "atrium.db")
	live, err := store.Open(context.Background(), dbPath)
	require.NoError(t, err)
	_, err = live.Migrate(context.Background())
	require.NoError(t, err)
	seed(t, live, 2)

	backupDir := t.TempDir()
	svc := backup.New(backup.Options{
		Dir: backupDir, Keep: 7, DB: live.SQL(),
		Now: (&stepClock{at: time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC)}).Now,
	})
	snap, err := svc.Run(context.Background())
	require.NoError(t, err)

	// More rows land after the snapshot; the restore must roll them back.
	require.NoError(t, live.Screens().Create(context.Background(), &domain.Screen{
		ID: "screen_z", Name: "Later", TokenHash: "hash_z",
		Status: domain.ScreenActive, CreatedAt: time.Now(), ApprovedAt: time.Now(),
	}))
	require.NoError(t, live.Close())

	res, err := backup.Restore(context.Background(), backup.RestoreOptions{
		From: snap.Path, DataDir: dataDir, DBPath: dbPath,
	})
	require.NoError(t, err)
	assert.Positive(t, res.Migrations)
	require.NotEmpty(t, res.PreservedPath)
	_, statErr := os.Stat(res.PreservedPath)
	require.NoError(t, statErr, "the previous database is preserved, never deleted")

	reopened, err := store.Open(context.Background(), dbPath)
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()
	rows, err := reopened.Screens().List(context.Background())
	require.NoError(t, err)
	assert.Len(t, rows, 2, "the restored database is the snapshot, not the live one")
}

func TestRestoreRejectsAFileThatIsNotASnapshot(t *testing.T) {
	dataDir := t.TempDir()
	bogus := filepath.Join(dataDir, "notes.txt")
	require.NoError(t, os.WriteFile(bogus, []byte("not a database"), 0o600))

	_, err := backup.Restore(context.Background(), backup.RestoreOptions{
		From: bogus, DataDir: dataDir, DBPath: filepath.Join(dataDir, "atrium.db"),
	})
	require.Error(t, err)
}
