package store

import (
	"context"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBackfillSchemaEightPreservesHistoryAndResumes(t *testing.T) {
	ctx := context.Background()
	d, e := OpenMemory(ctx)
	require.NoError(t, e)
	defer d.Close()
	_, e = d.sql.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL)`)
	require.NoError(t, e)
	ms, e := Migrations()
	require.NoError(t, e)
	for _, m := range ms {
		if m.Version > 8 {
			break
		}
		require.NoError(t, d.applyMigration(ctx, m))
	}
	now := time.Now()
	require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "old", Name: "Synthetic"}, now))
	in := domain.FamilyRecordInput{MemberID: "old", Category: "growth", Title: "milk", OccurredAt: now, DisplayOnTV: true, Measurements: []domain.FamilyMeasurement{{Metric: "volume", Value: 70, Unit: "ml"}}}
	first, _, e := d.FamilyRecords().Record(ctx, "p", "one", in, now)
	require.NoError(t, e)
	in.SupersedesID = first.ID
	in.RevisionReason = "correct"
	in.Measurements[0].Value = 80
	last, _, e := d.FamilyRecords().Record(ctx, "p", "two", in, now)
	require.NoError(t, e)
	var before string
	require.NoError(t, d.sql.QueryRow(`SELECT group_concat(id||payload_json,'') FROM (SELECT * FROM family_records ORDER BY id)`).Scan(&before))
	_, e = d.Migrate(ctx)
	require.NoError(t, e)
	require.NoError(t, d.ProjectLegacyArchive(ctx, first.ID))
	dead, cancel := context.WithCancel(ctx)
	cancel()
	_, e = d.BackfillArchives(dead)
	require.Error(t, e)
	s, e := d.BackfillArchives(ctx)
	require.NoError(t, e)
	require.Equal(t, 2, s.Scanned)
	var event, current, after string
	require.NoError(t, d.sql.QueryRow(`SELECT event_id,current_record_id FROM archive_legacy_projections`).Scan(&event, &current))
	require.Equal(t, last.ID, current)
	_, e = d.BackfillArchives(ctx)
	require.NoError(t, e)
	require.NoError(t, d.ProjectLegacyArchive(ctx, first.ID))
	var again string
	require.NoError(t, d.sql.QueryRow(`SELECT event_id FROM archive_legacy_projections`).Scan(&again))
	require.Equal(t, event, again)
	require.NoError(t, d.sql.QueryRow(`SELECT group_concat(id||payload_json,'') FROM (SELECT * FROM family_records ORDER BY id)`).Scan(&after))
	require.Equal(t, before, after)
	in.SupersedesID = last.ID
	in.RevisionReason = "withdraw"
	in.Retracted = true
	in.DisplayOnTV = false
	withdraw, _, e := d.FamilyRecords().Record(ctx, "p", "three", in, now)
	require.NoError(t, e)
	_, e = d.BackfillArchives(ctx)
	require.NoError(t, e)
	var active, visible bool
	require.NoError(t, d.sql.QueryRow(`SELECT event_id,current_record_id,active,tv_visible FROM archive_legacy_projections`).Scan(&again, &current, &active, &visible))
	require.Equal(t, event, again)
	require.Equal(t, withdraw.ID, current)
	require.False(t, active)
	require.False(t, visible)
}
