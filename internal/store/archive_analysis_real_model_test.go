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
	editor := archiveActor(t, d)
	a := newArchiveActor(t, d, "analysis", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	rec, f, x := publicationFixture(t, d, r, "real-scores", domain.ArchiveInputFile)
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

	observations, e := r.CurrentArchiveObservations(ctx, editor, "home-1", rec.DocumentID, false)
	require.NoError(t, e)
	var mathID, fullID string
	for _, o := range observations {
		var body struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(o.Body, &body))
		if body.Field.Name == "数学" {
			mathID = o.ID
		}
	}
	require.NotEmpty(t, mathID)
	revision, e := r.ReviseArchive(ctx, editor, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: mathID, Value: "90", Reason: "synthetic human correction"})
	require.NoError(t, e)
	result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "ratio", Metric: "数学", Timezone: "Asia/Singapore"})
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "90.00", result.Groups[0].Value)
	for _, o := range observations {
		var body struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(o.Body, &body))
		if body.Field.Name != "full_score" && body.Field.Name != "满分" {
			continue
		}
		for _, ev := range result.Groups[0].Evidence {
			if ev.ObservationID == o.ID {
				fullID = o.ID
			}
		}
	}
	require.NotEmpty(t, fullID)
	_, e = r.ReviseArchive(ctx, editor, "home-1", rec.DocumentID, revision, store.ArchiveRevision{Kind: "value", ObservationID: fullID, Value: "120", Reason: "synthetic denominator correction"})
	require.NoError(t, e)
	result, e = r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "ratio", Metric: "数学", Timezone: "Asia/Singapore"})
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "75.00", result.Groups[0].Value)
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

func TestRealModelEvidencePaginationFitsCompleteMCPEnvelope(t *testing.T) {
	raw, e := os.ReadFile("../archive/testdata/t15-e01-retained.json")
	require.NoError(t, e)
	var b store.ArchivePublicationBundle
	require.NoError(t, json.Unmarshal(raw, &b))
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "real-page", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, b)
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	cursor := ""
	seen := map[string]bool{}
	pages := 0
	for {
		p, e := r.ArchiveEvidence(ctx, a, "home-1", false, rec.DocumentID, archive.SearchFilter{Limit: 25, Cursor: cursor}, []byte("test-secret-at-least-32-bytes-long"))
		require.NoError(t, e)
		data, _ := json.Marshal(p)
		envelope := map[string]any{"schema_version": "1", "observed_at": "2026-10-11T00:00:00Z", "availability": "available", "data": json.RawMessage(data)}
		encoded, _ := json.Marshal(envelope)
		complete, _ := json.Marshal(map[string]any{"structuredContent": envelope, "content": []any{map[string]any{"type": "text", "text": string(encoded)}}})
		require.LessOrEqual(t, len(complete), 32*1024-512, "complete structured+text MCP representation must fit without truncation")
		for _, item := range p.Items {
			for _, ev := range item.Evidence {
				key, _ := json.Marshal(ev)
				require.False(t, seen[string(key)], "duplicate evidence across pages")
				seen[string(key)] = true
			}
		}
		pages++
		require.Less(t, pages, 20)
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	require.Greater(t, pages, 1)
	require.Greater(t, len(seen), 20)
}

func TestRealBillAndPaymentDecimalScalesShareSupportedAmount(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var ids []string
	for _, name := range []string{"f01", "f02"} {
		raw, e := os.ReadFile("../archive/testdata/t15-run05-" + name + "-retained.json")
		require.NoError(t, e)
		var b store.ArchivePublicationBundle
		require.NoError(t, json.Unmarshal(raw, &b))
		rec, f, x := publicationFixture(t, d, r, "scale-"+name, domain.ArchiveInputText)
		f = saveBundle(t, r, f, b)
		task, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		require.Equal(t, domain.ArchiveTaskSucceeded, task.Status)
		obs, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
		require.NoError(t, e)
		for _, o := range obs {
			var body struct{ Field archive.PublicationField }
			require.NoError(t, json.Unmarshal(o.Body, &body))
			if body.Field.Name == "amount" && (body.Field.Raw == "128.1" || body.Field.Raw == "128.10") {
				ids = append(ids, o.ID)
			}
		}
	}
	require.Len(t, ids, 2)
	require.Equal(t, ids[0], ids[1])
}
