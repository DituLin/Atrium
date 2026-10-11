package store_test

import (
	"context"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestRealModelChineseRecordDateSupportsDistinctFeedings(t *testing.T) {
	raw, e := os.ReadFile("../archive/testdata/t15-h04-retained.json")
	require.NoError(t, e)
	var b store.ArchivePublicationBundle
	require.NoError(t, json.Unmarshal(raw, &b))
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	_ = archiveActor(t, d)
	a := newArchiveActor(t, d, "analysis", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	for _, id := range []string{"real-feed-a", "real-feed-b"} {
		_, f, x := publicationFixture(t, d, r, id, domain.ArchiveInputText)
		f = saveBundle(t, r, f, b)
		out, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		require.Equal(t, domain.ArchiveTaskSucceeded, out.Status)
	}
	result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "volume", DateFrom: "2026-10-03", DateTo: "2026-10-03", Timezone: "Asia/Singapore"})
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "140", result.Groups[0].Value)
	birthday, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "volume", DateFrom: "2026-06-01", DateTo: "2026-06-01", Timezone: "Asia/Singapore"})
	require.NoError(t, e)
	require.Empty(t, birthday.Groups)
}

func TestRealModelScoreDenominatorsFollowSourceTableRows(t *testing.T) {
	raw, e := os.ReadFile("../archive/testdata/t15-e01-retained.json")
	require.NoError(t, e)
	var b store.ArchivePublicationBundle
	require.NoError(t, json.Unmarshal(raw, &b))
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	_ = archiveActor(t, d)
	a := newArchiveActor(t, d, "analysis", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	_, f, x := publicationFixture(t, d, r, "real-scores", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, b)
	out, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	require.Equal(t, domain.ArchiveTaskSucceeded, out.Status)
	for metric, value := range map[string]string{"数学": "88.00", "语文": "91.00"} {
		result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "ratio", Metric: metric, Timezone: "Asia/Singapore"})
		require.NoError(t, e)
		require.Len(t, result.Groups, 1)
		require.Equal(t, value, result.Groups[0].Value)
		require.NotEmpty(t, result.Groups[0].Evidence)
	}
}

func TestRealModelPaymentRetainsSharedAmountAndBothSourceSupports(t *testing.T) {
	raw, e := os.ReadFile("../archive/testdata/t15-f02-retained.json")
	require.NoError(t, e)
	var payment store.ArchivePublicationBundle
	require.NoError(t, json.Unmarshal(raw, &payment))
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var amountIDs []string
	for i, b := range []store.ArchivePublicationBundle{labeledBillBundle(t, "账单编号", "示例物业", "2026-10-01"), payment} {
		id := "bill"
		if i == 1 {
			id = "payment"
		}
		rec, f, x := publicationFixture(t, d, r, id, domain.ArchiveInputText)
		f = saveBundle(t, r, f, b)
		out, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		require.Equal(t, domain.ArchiveTaskSucceeded, out.Status)
		obs, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
		require.NoError(t, e)
		found := 0
		for _, o := range obs {
			var body struct{ Field archive.PublicationField }
			require.NoError(t, json.Unmarshal(o.Body, &body))
			if body.Field.Name == "amount" {
				found++
				amountIDs = append(amountIDs, o.ID)
			}
		}
		require.Equal(t, 1, found, "payment must expose its actual amount; a missing payment must not mimic successful deduplication")
	}
	require.Len(t, amountIDs, 2)
	require.Equal(t, amountIDs[0], amountIDs[1], "both original documents support one economic amount")
}
