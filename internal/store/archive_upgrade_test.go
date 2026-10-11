package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestArchiveAuthorizationUpgradePreservesUnboundHistory(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "v9.db"))
	require.NoError(t, err)
	defer db.Close()
	_, err = db.SQL().ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL)`)
	require.NoError(t, err)
	migrations, err := Migrations()
	require.NoError(t, err)
	for _, m := range migrations {
		if m.Version <= 9 {
			require.NoError(t, db.applyMigration(ctx, m))
		}
	}
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO integration_principals VALUES('legacy-agent','Legacy',1,NULL,'2030-01-01T00:00:00Z','{"permissions":["home.read"]}',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
 INSERT INTO archive_tasks VALUES('legacy-task','legacy-home','extract','key','{"original":true}','queued','intake',0,'',1,1,'{}','{}','{"answer":true}','2026-01-01T00:00:00.000000000Z','',NULL,'','2026-01-01T00:00:00.000000000Z','2026-01-01T00:00:00.000000000Z');
 INSERT INTO archive_task_events VALUES('legacy-home','legacy-task',1,'enqueued','{"legacy_event":true}','2026-01-01T00:00:00.000000000Z');
 INSERT INTO archive_task_resolutions VALUES('legacy-home','legacy-task','resolution-key',1,'{"answer":true}',1);`)
	require.NoError(t, err)
	applied, err := db.Migrate(ctx)
	require.NoError(t, err)
	require.Equal(t, 7, applied)
	task, err := archiveTaskIn(ctx, db.sql, "legacy-home", "legacy-task")
	require.NoError(t, err)
	require.Empty(t, task.PrincipalID)
	require.Zero(t, task.PolicyVersion)
	require.JSONEq(t, `{"original":true}`, string(task.InputJSON))
	require.JSONEq(t, `{"answer":true}`, string(task.ClarificationJSON))
	var event, input string
	require.NoError(t, db.SQL().QueryRowContext(ctx, `SELECT event_json FROM archive_task_events WHERE task_id='legacy-task'`).Scan(&event))
	require.JSONEq(t, `{"legacy_event":true}`, event)
	require.NoError(t, db.SQL().QueryRowContext(ctx, `SELECT input_json FROM archive_task_resolutions WHERE task_id='legacy-task'`).Scan(&input))
	require.JSONEq(t, `{"answer":true}`, input)
	p, err := db.Integrations().Get(ctx, "legacy-agent")
	require.NoError(t, err)
	require.Equal(t, []string{"home.read"}, p.Policy.Permissions)
	require.Empty(t, p.Policy.HouseholdIDs)
	claimed, err := db.ArchiveTasks().Claim(ctx, "legacy-home", time.Minute)
	require.NoError(t, err)
	require.Nil(t, claimed)
	rows, err := db.SQL().QueryContext(ctx, `PRAGMA foreign_key_check`)
	require.NoError(t, err)
	defer rows.Close()
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}
