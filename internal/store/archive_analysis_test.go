package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestArchiveAnalysisCurrentWithoutIndexerAndInvalidation(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	analyst := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	rec, f, x := publicationFixture(t, d, r, "analysis", domain.ArchiveInputText)
	f = saveBundle(t, r, f, analysisVolumeBundle(t, "70"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	q := archive.AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "Asia/Singapore"}
	_, e = r.AnalyzeArchives(ctx, a, "home-1", false, q)
	require.Error(t, e)
	result, e := r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "70", result.Groups[0].Value)
	require.NotEmpty(t, result.SnapshotID)
	saved, e := r.ArchiveAnalysisSnapshot(ctx, analyst, "home-1", false, result.SnapshotID)
	require.NoError(t, e)
	require.Equal(t, result.InputDigest, saved.InputDigest)
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "private", Reason: "private"})
	require.NoError(t, e)
	_, e = r.ArchiveAnalysisSnapshot(ctx, analyst, "home-1", false, result.SnapshotID)
	require.Error(t, e)
	pub, e := r.AnalyzeArchives(ctx, analyst, "home-1", true, q)
	require.NoError(t, e)
	require.Empty(t, pub.Groups)
	auth, e := r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
	require.NoError(t, e)
	require.Equal(t, "70", auth.Groups[0].Value)
}

func TestArchiveAnalysisLabeledFinanceSource(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	ctx := context.Background()
	a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	_, f, x := publicationFixture(t, d, r, "expense", domain.ArchiveInputText)
	b := analysisVolumeBundle(t, "128.10")
	// Rebuild a validated extraction whose raw label explicitly establishes the charge role.
	text := "费用 128.10 CNY"
	parsed, e := archive.Parse(ctx, archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "text"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	plan, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	ch := plan.Chunks[0]
	v := ch.Owned[0]
	extraction := b.Understanding.Chunks[0].Result
	extraction.Domain = "finance"
	extraction.Fields[0].Name = "charge"
	extraction.Fields[0].Evidence = []archive.EvidenceReference{{ViewID: v.ViewID, Start: 3, End: 9, Quote: "128.10"}}
	extraction.Fields[0].Dimensions = []archive.LiteralDimension{{Kind: "currency", Raw: "CNY", Evidence: []archive.EvidenceReference{{ViewID: v.ViewID, Start: 10, End: 13, Quote: "CNY"}}}}
	extraction.Events[0].SectionEvidence = []archive.EvidenceReference{{ViewID: v.ViewID, Start: 0, End: 13, Quote: text}}
	extraction.Coverage = []archive.UnitCoverage{{ViewID: v.ViewID, Disposition: "extracted"}}
	raw, e := json.Marshal(extraction)
	require.NoError(t, e)
	chunk, e := archive.ValidateChunk(plan, ch, raw)
	require.NoError(t, e)
	merged, e := archive.MergeChunks(plan, []archive.ChunkUnderstanding{chunk})
	require.NoError(t, e)
	f = saveBundle(t, r, f, store.ArchivePublicationBundle{Parsed: parsed, Plan: plan, Understanding: merged})
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "charge", Timezone: "Asia/Singapore"})
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "128.10", result.Groups[0].Value)
	require.Equal(t, "CNY", result.Groups[0].Currency)
}
func TestArchiveAnalysisLegacy450AndLatest70(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	h, e := d.LocalHousehold(ctx)
	require.NoError(t, e)
	a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{h.ID})
	require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "baby", Name: "Baby"}, archiveNow))
	for i, value := range []float64{260, 120, 70} {
		rec, _, e := d.FamilyRecords().Record(ctx, "test", fmt.Sprint(i), domain.FamilyRecordInput{MemberID: "baby", Category: "growth", Title: "milk", OccurredAt: archiveNow.Add(time.Duration(i) * time.Hour), DisplayOnTV: true, Measurements: []domain.FamilyMeasurement{{Metric: "volume", Value: value, Unit: "ml"}}}, archiveNow)
		require.NoError(t, e)
		require.NoError(t, d.ProjectLegacyArchive(ctx, rec.ID))
	}
	q := archive.AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "Asia/Singapore"}
	result, e := r.AnalyzeArchives(ctx, a, h.ID, false, q)
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "450", result.Groups[0].Value)
	q.Operation = "latest"
	result, e = r.AnalyzeArchives(ctx, a, h.ID, false, q)
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "70", result.Groups[0].Value)
}
func analysisCSVBundle(t *testing.T) store.ArchivePublicationBundle {
	text := "项目,金额,币种\n费用,128.10,CNY\n费用,71.90,CNY\n合计,200.00,CNY\n"
	parsed, e := archive.Parse(context.Background(), archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "csv"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	plan, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	require.Len(t, plan.Chunks, 1)
	ch := plan.Chunks[0]
	ext := archive.Extraction{Domain: "finance", DocumentType: "statement", Labels: []string{}, Subjects: []archive.LocalSubject{}, Fields: []archive.ExtractedField{}, Events: []archive.EventProposal{}, Coverage: []archive.UnitCoverage{}, Issues: []archive.ExtractionIssue{}}
	refs := map[string]archive.EvidenceReference{}
	for _, v := range ch.Owned {
		for _, seg := range parsed.Segments {
			if seg.ID == v.ParentSegmentID && seg.Locator.CSV != nil {
				refs[fmt.Sprintf("%d:%d", seg.Locator.CSV.Record, seg.Locator.CSV.Column)] = archive.EvidenceReference{ViewID: v.ViewID, Start: 0, End: len([]rune(seg.Text)), Quote: seg.Text}
			}
		}
		ext.Coverage = append(ext.Coverage, archive.UnitCoverage{ViewID: v.ViewID, Disposition: "extracted"})
	}
	for row := 2; row <= 4; row++ {
		id := fmt.Sprintf("f%d", row)
		num := refs[fmt.Sprintf("%d:2", row)]
		cur := refs[fmt.Sprintf("%d:3", row)]
		label := refs[fmt.Sprintf("%d:1", row)]
		ext.Fields = append(ext.Fields, archive.ExtractedField{ID: id, Name: "amount", Raw: num.Quote, SubjectRefs: []string{}, Normalizations: []archive.Normalization{{Kind: "decimal", Value: num.Quote}}, Evidence: []archive.EvidenceReference{num}, Dimensions: []archive.LiteralDimension{{Kind: "currency", Raw: "CNY", Evidence: []archive.EvidenceReference{cur}}}})
		ext.Events = append(ext.Events, archive.EventProposal{ID: "e" + id, Type: "expense", FieldRefs: []string{id}, SubjectRefs: []string{}, SectionEvidence: []archive.EvidenceReference{label, num, cur}})
	}
	for i, cov := range ext.Coverage {
		for _, seg := range parsed.Segments {
			if ref, ok := refs[fmt.Sprintf("1:%d", seg.Locator.CSV.Column)]; ok && ref.ViewID == cov.ViewID {
				ext.Coverage[i].Disposition = "irrelevant"
			}
		}
	}
	raw, e := json.Marshal(ext)
	require.NoError(t, e)
	chunk, e := archive.ValidateChunk(plan, ch, raw)
	require.NoError(t, e)
	merged, e := archive.MergeChunks(plan, []archive.ChunkUnderstanding{chunk})
	require.NoError(t, e)
	return store.ArchivePublicationBundle{Parsed: parsed, Plan: plan, Understanding: merged}
}
func TestArchiveAnalysisCSVLineItemsExcludeTotal(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	ctx := context.Background()
	a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	_, f, x := publicationFixture(t, d, r, "csv-analysis", domain.ArchiveInputText)
	f = saveBundle(t, r, f, analysisCSVBundle(t))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "Asia/Singapore"})
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "200.00", result.Groups[0].Value)
}
func TestArchiveAnalysisSnapshotFencesAndRecompute(t *testing.T) {
	for _, change := range []string{"new_fact", "withdraw", "rebuild", "revoke"} {
		t.Run(change, func(t *testing.T) {
			d := testutil.NewDB(t)
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			a := archiveActor(t, d)
			ctx := context.Background()
			analyst := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
			rec, f, x := publicationFixture(t, d, r, "first", domain.ArchiveInputText)
			f = saveBundle(t, r, f, analysisVolumeBundle(t, "70"))
			_, e := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			q := archive.AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "Asia/Singapore"}
			before, e := r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
			require.NoError(t, e)
			switch change {
			case "new_fact":
				_, f, x = publicationFixture(t, d, r, "second", domain.ArchiveInputText)
				f = saveBundle(t, r, f, analysisVolumeBundle(t, "80"))
				_, e = r.PublishExtraction(ctx, f, x.ID)
			case "withdraw":
				_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "withdraw", Reason: "withdraw"})
			case "rebuild":
				e = r.RebuildArchiveSearch(ctx, "home-1")
			case "revoke":
				e = d.Integrations().Revoke(ctx, analyst.PrincipalID, archiveNow)
			}
			require.NoError(t, e)
			_, e = r.ArchiveAnalysisSnapshot(ctx, analyst, "home-1", false, before.SnapshotID)
			require.Error(t, e)
			after, e := r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
			if change == "revoke" {
				require.Error(t, e)
				return
			}
			require.NoError(t, e)
			switch change {
			case "new_fact":
				require.Equal(t, "150", after.Groups[0].Value)
			case "withdraw":
				require.Empty(t, after.Groups)
			case "rebuild":
				require.Equal(t, "70", after.Groups[0].Value)
			}
		})
	}
}
func analysisLabeledBundle(t *testing.T, label, value, currency string) store.ArchivePublicationBundle {
	text := label + " " + value + " " + currency
	ctx := context.Background()
	parsed, e := archive.Parse(ctx, archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "text"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	plan, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	ch := plan.Chunks[0]
	v := ch.Owned[0]
	start := len([]rune(label)) + 1
	end := start + len(value)
	ext := archive.Extraction{Domain: "finance", DocumentType: "receipt", Labels: []string{}, Subjects: []archive.LocalSubject{}, Fields: []archive.ExtractedField{{ID: "f", Name: "amount", Raw: value, SubjectRefs: []string{}, Normalizations: []archive.Normalization{{Kind: "decimal", Value: value}}, Evidence: []archive.EvidenceReference{{ViewID: v.ViewID, Start: start, End: end, Quote: value}}, Dimensions: []archive.LiteralDimension{{Kind: "currency", Raw: currency, Evidence: []archive.EvidenceReference{{ViewID: v.ViewID, Start: end + 1, End: end + 1 + len(currency), Quote: currency}}}}}}, Events: []archive.EventProposal{{ID: "e", Type: "expense", FieldRefs: []string{"f"}, SubjectRefs: []string{}, SectionEvidence: []archive.EvidenceReference{{ViewID: v.ViewID, Start: 0, End: len([]rune(text)), Quote: text}}}}, Coverage: []archive.UnitCoverage{{ViewID: v.ViewID, Disposition: "extracted"}}, Issues: []archive.ExtractionIssue{}}
	raw, e := json.Marshal(ext)
	require.NoError(t, e)
	chunk, e := archive.ValidateChunk(plan, ch, raw)
	require.NoError(t, e)
	merged, e := archive.MergeChunks(plan, []archive.ChunkUnderstanding{chunk})
	require.NoError(t, e)
	return store.ArchivePublicationBundle{Parsed: parsed, Plan: plan, Understanding: merged}
}
func TestArchiveAnalysisPrivateFinance250Versus200(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	analyst := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	for i, b := range []store.ArchivePublicationBundle{analysisCSVBundle(t), analysisLabeledBundle(t, "费用", "50.00", "CNY"), analysisLabeledBundle(t, "费用", "20.00", "USD")} {
		rec, f, x := publicationFixture(t, d, r, fmt.Sprint("financial-", i), domain.ArchiveInputText)
		f = saveBundle(t, r, f, b)
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		if i == 1 {
			_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "private", Reason: "private expense"})
			require.NoError(t, e)
		}
	}
	for _, public := range []bool{false, true} {
		result, e := r.AnalyzeArchives(ctx, analyst, "home-1", public, archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "Asia/Singapore"})
		require.NoError(t, e)
		require.Len(t, result.Groups, 2)
		expected := "250.00"
		if public {
			expected = "200.00"
		}
		require.Equal(t, "CNY", result.Groups[0].Currency)
		require.Equal(t, expected, result.Groups[0].Value)
		require.Equal(t, "USD", result.Groups[1].Currency)
		require.Equal(t, "20.00", result.Groups[1].Value)
		raw, _ := json.Marshal(result)
		if public {
			require.NotContains(t, string(raw), "50.00")
			require.NotContains(t, string(raw), "250.00")
		}
	}
}
func TestArchiveAnalysisManualValueAndReassignment(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	analyst := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	ids := map[string]string{}
	docs := map[string]string{}
	for _, name := range []string{"Alice", "Bob"} {
		rec, f, x := publicationFixture(t, d, r, name, domain.ArchiveInputText)
		f = saveBundle(t, r, f, analysisNamedVolumeBundle(t, name))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		docs[name] = rec.DocumentID
		var id string
		require.NoError(t, d.SQL().QueryRow(`SELECT entity_id FROM archive_entity_attributes WHERE value=? LIMIT 1`, name).Scan(&id))
		ids[name] = id
	}
	q := archive.AnalysisQuery{Operation: "sum", Metric: "volume", EntityID: ids["Alice"], Timezone: "UTC"}
	before, e := r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
	require.NoError(t, e)
	require.Equal(t, "70", before.Groups[0].Value)
	current, e := r.CurrentArchiveObservations(ctx, a, "home-1", docs["Alice"], false)
	require.NoError(t, e)
	require.Len(t, current, 1)
	_, e = r.ReviseArchive(ctx, a, "home-1", docs["Alice"], 2, store.ArchiveRevision{Kind: "value", ObservationID: current[0].ID, Value: "80", Reason: "correct amount"})
	require.NoError(t, e)
	_, e = r.ReviseArchive(ctx, a, "home-1", docs["Alice"], 3, store.ArchiveRevision{Kind: "reassign", ObservationID: current[0].ID, EntityID: ids["Bob"], Reason: "correct owner"})
	require.NoError(t, e)
	_, e = r.ArchiveAnalysisSnapshot(ctx, analyst, "home-1", false, before.SnapshotID)
	require.Error(t, e)
	after, e := r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
	require.NoError(t, e)
	require.Empty(t, after.Groups)
	q.EntityID = ids["Bob"]
	after, e = r.AnalyzeArchives(ctx, analyst, "home-1", false, q)
	require.NoError(t, e)
	require.Len(t, after.Groups, 1)
	require.Equal(t, "150", after.Groups[0].Value)
	raw, _ := json.Marshal(after)
	require.Contains(t, string(raw), `"asserted_value":"80"`)
	require.Contains(t, string(raw), `"excerpt":"70"`)
}
func TestArchiveAnalysisPendingLegacyIsNotCompleteZero(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	h, e := d.LocalHousehold(ctx)
	require.NoError(t, e)
	a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{h.ID})
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	require.NoError(t, d.FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "baby", Name: "Baby"}, archiveNow))
	_, _, e = d.FamilyRecords().Record(ctx, "test", "pending", domain.FamilyRecordInput{MemberID: "baby", Category: "growth", Title: "milk", OccurredAt: archiveNow, DisplayOnTV: true, Measurements: []domain.FamilyMeasurement{{Metric: "volume", Value: 70, Unit: "ml"}}}, archiveNow)
	require.NoError(t, e)
	result, e := r.AnalyzeArchives(ctx, a, h.ID, false, archive.AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "UTC"})
	require.NoError(t, e)
	require.False(t, result.Coverage.Complete)
	require.Contains(t, result.Coverage.Reasons, "legacy_projection_pending")
}

func analysisWithVolumeUnit(t *testing.T, b store.ArchivePublicationBundle) store.ArchivePublicationBundle {
	ext := b.Understanding.Chunks[0].Result
	view := b.Plan.Chunks[0].Owned[0]
	text := b.Parsed.Segments[0].Text
	n := len([]rune(text))
	ext.Fields[0].Dimensions = []archive.LiteralDimension{{Kind: "unit", Raw: "ml", Evidence: []archive.EvidenceReference{{ViewID: view.ViewID, Start: n - 2, End: n, Quote: "ml"}}}}
	return revalidatePublication(t, b, ext)
}
func analysisVolumeBundle(t *testing.T, value string) store.ArchivePublicationBundle {
	return analysisWithVolumeUnit(t, publicationBundle(t, value))
}
func analysisNamedVolumeBundle(t *testing.T, name string) store.ArchivePublicationBundle {
	return analysisWithVolumeUnit(t, namedPublicationBundle(t, name))
}
