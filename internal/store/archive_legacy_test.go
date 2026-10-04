package store_test

import (
	"context"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestArchiveLegacyOutboxProjectsCurrentChainDespiteOldDelivery(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "legacy", Name: "Legacy"}, now))
	in := domain.FamilyRecordInput{MemberID: "legacy", Category: "growth", Title: "milk", OccurredAt: now, DisplayOnTV: true, Measurements: []domain.FamilyMeasurement{{Metric: "volume", Value: 70, Unit: "ml"}}}
	first, _, e := d.FamilyRecords().Record(ctx, "p", "r1", in, now)
	require.NoError(t, e)
	in.SupersedesID = first.ID
	in.RevisionReason = "correct and private"
	in.DisplayOnTV = false
	in.Measurements[0].Value = 80
	middle, _, e := d.FamilyRecords().Record(ctx, "p", "middle", in, now)
	require.NoError(t, e)
	require.NoError(t, d.ProjectLegacyArchive(ctx, first.ID))
	var visible bool
	var intermediate string
	require.NoError(t, d.SQL().QueryRow(`SELECT tv_visible,current_record_id FROM archive_legacy_projections`).Scan(&visible, &intermediate))
	require.False(t, visible)
	require.Equal(t, middle.ID, intermediate)
	in.SupersedesID = middle.ID
	in.RevisionReason = "withdraw"
	in.Retracted = true
	in.DisplayOnTV = false
	last, _, e := d.FamilyRecords().Record(ctx, "p", "r2", in, now)
	require.NoError(t, e)
	for _, id := range []string{last.ID, first.ID, middle.ID, last.ID} {
		require.NoError(t, d.ProjectLegacyArchive(ctx, id))
	}
	var current, event string
	var active bool
	require.NoError(t, d.SQL().QueryRow(`SELECT current_record_id,event_id,active FROM archive_legacy_projections`).Scan(&current, &event, &active))
	require.Equal(t, last.ID, current)
	require.False(t, active)
	require.NotEmpty(t, event)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_legacy_record_events`).Scan(&n))
	require.Equal(t, 3, n)
}
