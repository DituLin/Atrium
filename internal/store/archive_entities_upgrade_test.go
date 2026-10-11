package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntityUpgradeFromTwelvePreservesLegacyAndOriginals(t *testing.T) {
	ctx := context.Background()
	d, e := Open(ctx, filepath.Join(t.TempDir(), "upgrade.db"))
	require.NoError(t, e)
	defer d.Close()
	_, e = d.sql.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL)`)
	require.NoError(t, e)
	ms, e := Migrations()
	require.NoError(t, e)
	for _, m := range ms {
		if m.Version <= 12 {
			require.NoError(t, d.applyMigration(ctx, m))
		}
	}
	var home string
	require.NoError(t, d.sql.QueryRow(`SELECT household_id FROM local_household`).Scan(&home))
	_, e = d.sql.Exec(`INSERT INTO family_members VALUES('abu','阿布','now')`)
	require.NoError(t, e)
	_, e = d.sql.Exec(`INSERT INTO archive_blobs VALUES(?,?,1,'text/plain',1,'now')`, home, string(makeHex()))
	require.NoError(t, e)
	_, e = d.sql.Exec(`INSERT INTO archive_documents(id,household_id,blob_sha256,input_kind,tv_visible,created_at) VALUES('original',?,?,'text',0,'now')`, home, string(makeHex()))
	require.NoError(t, e)
	n, e := d.Migrate(ctx)
	require.NoError(t, e)
	require.Equal(t, 4, n)
	n, e = d.Migrate(ctx)
	require.NoError(t, e)
	require.Zero(t, n)
	var name string
	require.NoError(t, d.sql.QueryRow(`SELECT name FROM family_members WHERE id='abu'`).Scan(&name))
	require.Equal(t, "阿布", name)
	var private bool
	require.NoError(t, d.sql.QueryRow(`SELECT tv_visible FROM archive_documents WHERE id='original'`).Scan(&private))
	require.False(t, private)
	var count int
	require.NoError(t, d.sql.QueryRow(`SELECT count(*) FROM archive_entities`).Scan(&count))
	require.Zero(t, count, "migration must not turn account/member list into entities")
	rows, e := d.sql.Query(`PRAGMA foreign_key_check`)
	require.NoError(t, e)
	defer rows.Close()
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}
