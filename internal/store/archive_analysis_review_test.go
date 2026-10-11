package store_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func analysisReviewBundle(t *testing.T, prefix string, currency ...bool) store.ArchivePublicationBundle {
	t.Helper()
	return analysisReviewTextBundle(t, prefix+"70 ml serum CNY 100 2026-10-04", currency...)
}
func analysisReviewTextBundle(t *testing.T, text string, currency ...bool) store.ArchivePublicationBundle {
	t.Helper()
	parsed, e := archive.Parse(context.Background(), archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "text"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	plan, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	ch := plan.Chunks[0]
	view := ch.Owned[0]
	ref := func(token string) archive.EvidenceReference {
		start := strings.LastIndex(text, token)
		return archive.EvidenceReference{ViewID: view.ViewID, Start: utf8.RuneCountInString(text[:start]), End: utf8.RuneCountInString(text[:start+len(token)]), Quote: token}
	}
	fields := []archive.ExtractedField{}
	ids := []string{}
	for _, pair := range [][2]string{{"volume", "70"}, {"unit", "ml"}, {"sample", "serum"}, {"currency", "CNY"}, {"full_score", "100"}, {"report_date", "2026-10-04"}} {
		f := archive.ExtractedField{ID: pair[0], Name: pair[0], Raw: pair[1], SubjectRefs: []string{}, Normalizations: []archive.Normalization{}, Evidence: []archive.EvidenceReference{ref(pair[1])}, Dimensions: []archive.LiteralDimension{}}
		if pair[0] == "volume" {
			f.Normalizations = []archive.Normalization{{Kind: "decimal", Value: "70"}}
			f.Dimensions = []archive.LiteralDimension{{Kind: "unit", Raw: "ml", Evidence: []archive.EvidenceReference{ref("ml")}}}
		}
		if pair[0] == "full_score" && (strings.Contains(text, "100分") || strings.Contains(text, "100 分")) {
			f.Dimensions = []archive.LiteralDimension{{Kind: "unit", Raw: "分", Evidence: []archive.EvidenceReference{ref("分")}}}
		}
		if pair[0] == "report_date" {
			f.Normalizations = []archive.Normalization{{Kind: "date", Value: pair[1]}}
		}
		if pair[0] == "volume" && len(currency) > 0 && currency[0] {
			f.Dimensions = append(f.Dimensions, archive.LiteralDimension{Kind: "currency", Raw: "CNY", Evidence: []archive.EvidenceReference{ref("CNY")}})
		}
		fields = append(fields, f)
		ids = append(ids, pair[0])
	}
	ext := archive.Extraction{Domain: "health", DocumentType: "report", Labels: []string{}, Subjects: []archive.LocalSubject{}, Fields: fields, Events: []archive.EventProposal{{ID: "e", Type: "report", FieldRefs: ids, SubjectRefs: []string{}, SectionEvidence: []archive.EvidenceReference{{ViewID: view.ViewID, Start: 0, End: utf8.RuneCountInString(text), Quote: text}}}}, Coverage: []archive.UnitCoverage{{ViewID: view.ViewID, Disposition: "extracted"}}, Issues: []archive.ExtractionIssue{}}
	raw, e := json.Marshal(ext)
	require.NoError(t, e)
	chunk, e := archive.ValidateChunk(plan, ch, raw)
	require.NoError(t, e)
	merged, e := archive.MergeChunks(plan, []archive.ChunkUnderstanding{chunk})
	require.NoError(t, e)
	return store.ArchivePublicationBundle{Parsed: parsed, Plan: plan, Understanding: merged}
}
func TestAnalysisReviewSourceQualifiers(t *testing.T) {
	for _, prefix := range []string{">", "<", "<= ", ">= ", "≥", "≤", "约", "大约 ", "approximately ", "about ", "≈", "other >9; volume: ", "other <9\nvolume: "} {
		t.Run(prefix, func(t *testing.T) {
			ctx := context.Background()
			d := testutil.NewDB(t)
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			a := archiveActor(t, d)
			analyst := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
			rec, f, x := publicationFixture(t, d, r, "qualifier", domain.ArchiveInputText)
			f = saveBundle(t, r, f, analysisReviewBundle(t, prefix))
			_, e := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			q := archive.AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "UTC"}
			result, e := r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
			require.NoError(t, e)
			if strings.HasPrefix(prefix, "other") {
				require.Len(t, result.Groups, 1)
				require.Equal(t, "70", result.Groups[0].Value)
				return
			}
			require.Empty(t, result.Groups)
			require.False(t, result.Coverage.Complete)
			obs, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
			require.NoError(t, e)
			id := ""
			for _, o := range obs {
				var b struct{ Field archive.PublicationField }
				require.NoError(t, json.Unmarshal(o.Body, &b))
				if b.Field.Name == "volume" {
					id = o.ID
				}
			}
			_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: id, Value: "80", Reason: "explicit measured correction"})
			require.NoError(t, e)
			result, e = r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
			require.NoError(t, e)
			require.Len(t, result.Groups, 1)
			require.Equal(t, "80", result.Groups[0].Value)
			_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 3, store.ArchiveRevision{Kind: "value", ObservationID: id, Value: ">80", Reason: "correct bound"})
			require.NoError(t, e)
			result, e = r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
			require.NoError(t, e)
			require.Empty(t, result.Groups)
		})
	}
}
func TestAnalysisReviewCurrentDimensionsAndPrivacy(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	analyst := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	rec, f, x := publicationFixture(t, d, r, "dimensions", domain.ArchiveInputText)
	f = saveBundle(t, r, f, analysisReviewBundle(t, "", true))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	obs, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
	require.NoError(t, e)
	ids := map[string]string{}
	for _, o := range obs {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(o.Body, &b))
		ids[b.Field.Name] = o.ID
	}
	rev := int64(2)
	for _, pair := range [][2]string{{"sample", "urine"}, {"unit", "L"}, {"currency", "USD"}, {"full_score", "200"}, {"report_date", "2026-10-05"}} {
		_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, rev, store.ArchiveRevision{Kind: "value", ObservationID: ids[pair[0]], Value: pair[1], Reason: "current fact"})
		require.NoError(t, e)
		rev++
	}
	q := archive.AnalysisQuery{Operation: "series", Metric: "volume", Timezone: "UTC"}
	result, e := r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Contains(t, result.Groups[0].Sample, "urine")
	require.Equal(t, "L", result.Groups[0].Unit)
	require.Equal(t, "USD", result.Groups[0].Currency)
	require.Equal(t, "2026-10-05", result.Groups[0].Date)
	evidence, e := json.Marshal(result.Groups[0].Evidence)
	require.NoError(t, e)
	require.Contains(t, string(evidence), "urine")
	require.Contains(t, string(evidence), "serum")
	require.Contains(t, string(evidence), "assertion_id")
	q.Operation = "ratio"
	result, e = r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "35.00", result.Groups[0].Value)
	require.Equal(t, "200", result.Groups[0].Denominator)
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, rev, store.ArchiveRevision{Kind: "private", ObservationID: ids["sample"], Reason: "private grouping dimension"})
	require.NoError(t, e)
	result, e = r.AnalyzeArchives(ctx, analyst, "home-1", true, q)
	require.NoError(t, e)
	require.Empty(t, result.Groups)
	require.False(t, result.Coverage.Complete)
	raw, e := json.Marshal(result)
	require.NoError(t, e)
	require.NotContains(t, string(raw), "urine")
	require.NotContains(t, string(raw), "serum")
}

func TestAnalysisReviewIndependentTableTotals(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		values     map[string]string
	}{
		{"different currencies", "项目,金额,币种\n费用,10.00,CNY\n合计,20.00,USD\n", map[string]string{"CNY": "10.00", "USD": "20.00"}},
		{"two independent totals", "项目,金额,币种\n费用,10.00,CNY\n合计,20.00,CNY\n", map[string]string{"CNY": "30.00"}},
		{"two billed blocks", "项目,金额,币种\n费用,10.00,CNY\n合计,10.00,CNY\n合计,20.00,CNY\n", map[string]string{"CNY": "30.00"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testutil.NewDB(t)
			ctx := context.Background()
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			_ = archiveActor(t, d)
			a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
			_, f, x := publicationFixture(t, d, r, "tables", domain.ArchiveInputText)
			f = saveBundle(t, r, f, analysisCSVTextBundle(t, tc.text))
			_, e := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "UTC"})
			require.NoError(t, e)
			require.Len(t, result.Groups, len(tc.values))
			for _, g := range result.Groups {
				require.Equal(t, tc.values[g.Currency], g.Value)
			}
			if tc.name == "two independent totals" {
				require.False(t, result.Coverage.Complete)
				require.Contains(t, result.Coverage.Reasons, "unproven_summary_relation")
			}
		})
	}
}
