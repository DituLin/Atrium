package store_test

import (
	"context"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestArchiveHomeDaySearchAndCount(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	h, e := d.LocalHousehold(ctx)
	require.NoError(t, e)
	a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{h.ID})
	require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "baby", Name: "Baby"}, archiveNow))
	occurred := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)
	rec, _, e := d.FamilyRecords().Record(ctx, "test", "midnight", domain.FamilyRecordInput{MemberID: "baby", Category: "growth", Title: "milk", OccurredAt: occurred, DisplayOnTV: true, Measurements: []domain.FamilyMeasurement{{Metric: "volume", Value: 70, Unit: "ml"}}}, archiveNow)
	require.NoError(t, e)
	require.NoError(t, d.ProjectLegacyArchive(ctx, rec.ID))
	for _, bound := range []struct{ from, to, want string }{{"2026-10-04", "2026-10-04", "1"}, {"2026-10-03", "2026-10-03", "0"}, {"2026-10-03", "2026-10-04", "1"}, {"2026-10-05", "", "0"}, {"", "2026-10-03", "0"}} {
		q := archive.AnalysisQuery{Operation: "count", Count: "documents", DateFrom: bound.from, DateTo: bound.to, Timezone: "Asia/Singapore"}
		result, e := r.AnalyzeArchives(ctx, a, h.ID, false, q)
		require.NoError(t, e)
		require.Len(t, result.Groups, 1)
		require.Equal(t, bound.want, result.Groups[0].Value, "2026-10-03T18:00Z is Oct4 02:00 in home timezone")
		page, e := r.SearchArchives(ctx, a, h.ID, false, archive.SearchFilter{DateFrom: bound.from, DateTo: bound.to, Timezone: q.Timezone}, []byte("key"))
		require.NoError(t, e)
		if bound.want == "1" {
			require.Len(t, page.Items, 1)
			require.Equal(t, occurred.Format(time.RFC3339Nano), page.Items[0].Dates[0])
		} else {
			require.Empty(t, page.Items)
		}
	}
}
