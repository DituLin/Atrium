package store_test

import (
	"context"
	"fmt"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
)

func TestFamilyRecordsReplayAndVisibility(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	repo := db.FamilyRecords()
	now := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	require.NoError(t, repo.PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, now))
	require.NoError(t, repo.PutMember(ctx, domain.FamilyMember{ID: "parent", Name: "家长"}, now))
	in := domain.FamilyRecordInput{MemberID: "abu", Category: "growth", OccurredAt: now, Title: "身高记录", Measurements: []domain.FamilyMeasurement{{Metric: "height", Value: 70, Unit: "cm"}}, DisplayOnTV: true}
	first, replayed, err := repo.Record(ctx, "im-principal", "event-1", in, now)
	require.NoError(t, err)
	require.False(t, replayed)
	require.NotEmpty(t, first.ID)
	second, replayed, err := repo.Record(ctx, "im-principal", "event-1", in, now.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, first.ID, second.ID)
	in.Title = "different"
	_, _, err = repo.Record(ctx, "im-principal", "event-1", in, now)
	require.Error(t, err)
	in.Title = "体检"
	in.DisplayOnTV = false
	in.Category = "checkup"
	_, _, err = repo.Record(ctx, "im-principal", "event-2", in, now)
	require.NoError(t, err)
	all, err := repo.List(ctx, "abu", false, 50)
	require.NoError(t, err)
	require.Len(t, all, 2)
	tv, err := repo.List(ctx, "abu", true, 50)
	require.NoError(t, err)
	require.Len(t, tv, 1)
	other, err := repo.List(ctx, "parent", false, 50)
	require.NoError(t, err)
	require.Empty(t, other)
	pending, err := repo.PendingExports(ctx, 50)
	require.NoError(t, err)
	require.Len(t, pending, 2)
	require.NoError(t, repo.MarkExported(ctx, first.ID, now))
	pending, err = repo.PendingExports(ctx, 50)
	require.NoError(t, err)
	require.Len(t, pending, 1)
}

func TestFamilyRevisionHistoryAndTVProjection(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	r := db.FamilyRecords()
	now := time.Now().UTC()
	require.NoError(t, r.PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, now))
	in := domain.FamilyRecordInput{MemberID: "abu", Category: "growth", OccurredAt: now, Title: "身高", DisplayOnTV: true, Measurements: []domain.FamilyMeasurement{{Metric: "height", Value: 70, Unit: "cm"}}}
	original, _, err := r.Record(ctx, "im", "original", in, now)
	require.NoError(t, err)
	in.SupersedesID = original.ID
	in.RevisionReason = "更正录入错误"
	in.Measurements = []domain.FamilyMeasurement{{Metric: "height", Value: 71, Unit: "cm"}}
	corrected, _, err := r.Record(ctx, "im", "correction", in, now.Add(time.Second))
	require.NoError(t, err)
	replayed, replay, err := r.Record(ctx, "im", "correction", in, now.Add(2*time.Second))
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, corrected.ID, replayed.ID)
	_, _, err = r.Record(ctx, "im", "stale-correction", in, now)
	require.Error(t, err, "an already superseded record cannot fork")
	tv, err := r.ListPage(ctx, "abu", true, 50, "")
	require.NoError(t, err)
	require.Len(t, tv.Records, 1)
	require.Equal(t, corrected.ID, tv.Records[0].ID)
	history, err := r.List(ctx, "abu", false, 50)
	require.NoError(t, err)
	require.Len(t, history, 2)
	for _, record := range history {
		if record.ID == original.ID {
			require.Equal(t, corrected.ID, record.SupersededBy)
			require.Equal(t, float64(70), record.Measurements[0].Value)
		}
	}
	in.SupersedesID = corrected.ID
	in.RevisionReason = "撤回误记"
	in.Retracted = true
	in.DisplayOnTV = false
	withdrawn, _, err := r.Record(ctx, "im", "retraction", in, now.Add(3*time.Second))
	require.NoError(t, err)
	tv, err = r.ListPage(ctx, "abu", true, 50, "")
	require.NoError(t, err)
	require.Empty(t, tv.Records)
	in.SupersedesID = withdrawn.ID
	in.Retracted = false
	_, _, err = r.Record(ctx, "im", "revive", in, now)
	require.Error(t, err, "retraction is terminal; a new observation needs a new record")
	pending, err := r.PendingExports(ctx, 50)
	require.NoError(t, err)
	require.Len(t, pending, 3)
	for _, record := range pending {
		require.Empty(t, record.SupersededBy, "immutable archive excludes dynamic successor projection")
	}
}

func TestFamilyRevisionCannotCrossMemberOrOmitReason(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	r := db.FamilyRecords()
	now := time.Now().UTC()
	for _, id := range []string{"abu", "parent"} {
		require.NoError(t, r.PutMember(ctx, domain.FamilyMember{ID: id, Name: id}, now))
	}
	in := domain.FamilyRecordInput{MemberID: "abu", Category: "note", OccurredAt: now, Title: "记录"}
	original, _, err := r.Record(ctx, "im", "original", in, now)
	require.NoError(t, err)
	in.SupersedesID = original.ID
	_, _, err = r.Record(ctx, "im", "no-reason", in, now)
	require.Error(t, err)
	in.RevisionReason = "更正"
	in.MemberID = "parent"
	_, _, err = r.Record(ctx, "im", "other-member", in, now)
	require.Error(t, err)
	in.MemberID = "abu"
	in.SupersedesID = domain.NewID()
	_, _, err = r.Record(ctx, "im", "missing", in, now)
	require.Error(t, err)
	pending, err := r.PendingExports(ctx, 50)
	require.NoError(t, err)
	require.Len(t, pending, 1)
}

func TestConcurrentFamilyCorrectionsHaveOneWinnerAndPrivateRevisionHidesOriginal(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	repo := db.FamilyRecords()
	now := time.Now().UTC()
	require.NoError(t, repo.PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, now))
	in := domain.FamilyRecordInput{MemberID: "abu", Category: "note", OccurredAt: now, Title: "记录", DisplayOnTV: true}
	original, _, err := repo.Record(ctx, "im", "original", in, now)
	require.NoError(t, err)
	in.SupersedesID = original.ID
	in.RevisionReason = "改为不在客厅显示"
	in.DisplayOnTV = false
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			_, _, err := repo.Record(ctx, "im", fmt.Sprintf("revision-%d", i), in, now)
			results <- err
		}(i)
	}
	success := 0
	for i := 0; i < 8; i++ {
		if <-results == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
	tv, err := repo.List(ctx, "abu", true, 50)
	require.NoError(t, err)
	require.Empty(t, tv)
	history, err := repo.List(ctx, "abu", false, 50)
	require.NoError(t, err)
	require.Len(t, history, 2)
	original.FamilyRecordInput.Measurements = nil
	back, replay, err := repo.Record(ctx, "im", "original", original.FamilyRecordInput, now)
	require.NoError(t, err)
	require.True(t, replay)
	require.NotEmpty(t, back.SupersededBy, "old request replay must indicate a newer revision exists")
}

func TestFamilyRecordsRejectUnknownMemberAndInvalidMeasurements(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	r := db.FamilyRecords()
	now := time.Now().UTC()
	in := domain.FamilyRecordInput{MemberID: "missing", Category: "growth", OccurredAt: now, Title: "记录"}
	_, _, err := r.Record(ctx, "im", "event", in, now)
	require.Error(t, err)
	require.NoError(t, r.PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, now))
	in.MemberID = "abu"
	in.Measurements = []domain.FamilyMeasurement{{Metric: "weight", Value: 3, Unit: ""}}
	_, _, err = r.Record(ctx, "im", "event", in, now)
	require.Error(t, err)
	pending, err := r.PendingExports(ctx, 50)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestFamilyRecordsSurviveReopenAndSortByEffectiveTime(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "family.db")
	db, err := store.Open(ctx, path)
	require.NoError(t, err)
	_, err = db.Migrate(ctx)
	require.NoError(t, err)
	now := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	require.NoError(t, db.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, now))
	in := domain.FamilyRecordInput{MemberID: "abu", Category: "growth", OccurredAt: now, Title: "今日测量"}
	a, _, err := db.FamilyRecords().Record(ctx, "im", "today", in, now)
	require.NoError(t, err)
	in.OccurredAt = now.Add(-24 * time.Hour)
	in.Title = "补录昨天"
	_, _, err = db.FamilyRecords().Record(ctx, "im", "yesterday", in, now.Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	db, err = store.Open(ctx, path)
	require.NoError(t, err)
	defer db.Close()
	records, err := db.FamilyRecords().List(ctx, "abu", false, 10)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, a.ID, records[0].ID)
	pending, err := db.FamilyRecords().PendingExports(ctx, 10)
	require.NoError(t, err)
	require.Len(t, pending, 2)
	members, err := db.FamilyRecords().Members(ctx)
	require.NoError(t, err)
	require.Equal(t, []domain.FamilyMember{{ID: "abu", Name: "阿布"}}, members)
}

func TestFamilyRecordsConcurrentReplay(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	repo := db.FamilyRecords()
	require.NoError(t, repo.PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, now))
	in := domain.FamilyRecordInput{MemberID: "abu", Category: "note", OccurredAt: now, Title: "学会挥手"}
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { _, _, err := repo.Record(ctx, "im", "same-event", in, now); results <- err }()
	}
	for i := 0; i < 8; i++ {
		require.NoError(t, <-results)
	}
	got, err := repo.List(ctx, "abu", false, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	pending, err := repo.PendingExports(ctx, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
}

func TestFamilyRecordPaginationKeepsMemberAndVisibility(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	r := db.FamilyRecords()
	now := time.Now().UTC()
	require.NoError(t, r.PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, now))
	for i := 0; i < 3; i++ {
		_, _, err := r.Record(ctx, "im", fmt.Sprint(i), domain.FamilyRecordInput{MemberID: "abu", Category: "note", OccurredAt: now, Title: fmt.Sprint(i), DisplayOnTV: true}, now)
		require.NoError(t, err)
	}
	page, err := r.ListPage(ctx, "abu", true, 2, "")
	require.NoError(t, err)
	require.Len(t, page.Records, 2)
	require.NotEmpty(t, page.NextCursor)
	next, err := r.ListPage(ctx, "abu", true, 2, page.NextCursor)
	require.NoError(t, err)
	require.Len(t, next.Records, 1)
	require.Empty(t, next.NextCursor)
	require.NotEqual(t, page.Records[0].ID, next.Records[0].ID)
	_, err = r.ListPage(ctx, "parent", true, 2, page.NextCursor)
	require.Error(t, err)
	_, err = r.ListPage(ctx, "abu", false, 2, page.NextCursor)
	require.Error(t, err)
	_, err = r.ListPage(ctx, "abu", true, 2, "garbage")
	require.Error(t, err)
}
