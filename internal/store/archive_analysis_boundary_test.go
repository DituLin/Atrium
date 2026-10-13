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
)

func analysisInvoiceCSVBundle(t *testing.T, text string, identityFields ...string) store.ArchivePublicationBundle {
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
	for row := 2; row <= strings.Count(text, "\n"); row++ {
		id := fmt.Sprintf("f%d", row)
		num := refs[fmt.Sprintf("%d:2", row)]
		cur := refs[fmt.Sprintf("%d:3", row)]
		invoice := refs[fmt.Sprintf("%d:4", row)]
		ext.Fields = append(ext.Fields, archive.ExtractedField{ID: "invoice" + id, Name: func() string {
			if len(identityFields) > 0 {
				return identityFields[0]
			}
			return "invoice_id"
		}(), Raw: invoice.Quote, SubjectRefs: []string{}, Normalizations: []archive.Normalization{}, Evidence: []archive.EvidenceReference{invoice}, Dimensions: []archive.LiteralDimension{}})
		label := refs[fmt.Sprintf("%d:1", row)]
		ext.Fields = append(ext.Fields, archive.ExtractedField{ID: id, Name: "amount", Raw: num.Quote, SubjectRefs: []string{}, Normalizations: []archive.Normalization{{Kind: "decimal", Value: num.Quote}}, Evidence: []archive.EvidenceReference{num}, Dimensions: []archive.LiteralDimension{{Kind: "currency", Raw: cur.Quote, Evidence: []archive.EvidenceReference{cur}}}})
		ext.Events = append(ext.Events, archive.EventProposal{ID: "e" + id, Type: "expense", FieldRefs: []string{id, "invoice" + id}, SubjectRefs: []string{}, SectionEvidence: []archive.EvidenceReference{label, num, cur, invoice}})
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
func TestAnalysisDistinctInvoicesSameTable(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	_, f, x := publicationFixture(t, d, r, "different-invoices", domain.ArchiveInputText)
	b := analysisInvoiceCSVBundle(t, "项目,金额,币种,账单号\n费用,10.00,CNY,INV-A\n合计,10.00,CNY,INV-B\n")
	f = saveBundle(t, r, f, b)
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "UTC"})
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "20.00", result.Groups[0].Value, "different explicit invoices must not become one charge merely by adjacency and equal amounts")
}

func TestAnalysisInvoiceBoundaryUsesCurrentAssertionsAndPrivacy(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	editor := archiveActor(t, d)
	a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	rec, f, x := publicationFixture(t, d, r, "invoice-revisions", domain.ArchiveInputText)
	f = saveBundle(t, r, f, analysisInvoiceCSVBundle(t, "项目,金额,币种,账单号\n费用,10.00,CNY,INV-A\n合计,10.00,CNY,INV-A\n"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	q := archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "UTC"}
	result, e := r.AnalyzeArchives(ctx, a, "home-1", false, q)
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "10.00", result.Groups[0].Value)
	obs, e := r.CurrentArchiveObservations(ctx, editor, "home-1", rec.DocumentID, false)
	require.NoError(t, e)
	invoice := ""
	for _, o := range obs {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(o.Body, &b))
		if b.Field.Name == "invoice_id" && b.Field.Evidence[0].Quote == "INV-A" {
			for _, ev := range b.Field.Evidence {
				if ev.ParentSegmentID != "" {
					invoice = o.ID
					break
				}
			}
			if invoice != "" {
				break
			}
		}
	}
	require.NotEmpty(t, invoice)
	_, e = r.ReviseArchive(ctx, editor, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: invoice, Value: "INV-B", Reason: "correct bill identity"})
	require.NoError(t, e)
	result, e = r.AnalyzeArchives(ctx, a, "home-1", false, q)
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "20.00", result.Groups[0].Value)
	raw, e := json.Marshal(result.Groups[0].Evidence)
	require.NoError(t, e)
	require.Contains(t, string(raw), "INV-A")
	require.Contains(t, string(raw), "INV-B")
	require.Contains(t, string(raw), "assertion_id")
	_, e = r.ReviseArchive(ctx, editor, "home-1", rec.DocumentID, 3, store.ArchiveRevision{Kind: "value", ObservationID: invoice, Value: "INV-A", Reason: "confirmed same bill"})
	require.NoError(t, e)
	result, e = r.AnalyzeArchives(ctx, a, "home-1", false, q)
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "10.00", result.Groups[0].Value)
	_, e = r.ReviseArchive(ctx, editor, "home-1", rec.DocumentID, 4, store.ArchiveRevision{Kind: "private", ObservationID: invoice, Reason: "private bill identity"})
	require.NoError(t, e)
	result, e = r.AnalyzeArchives(ctx, a, "home-1", true, q)
	require.NoError(t, e)
	require.False(t, result.Coverage.Complete)
	raw, e = json.Marshal(result)
	require.NoError(t, e)
	require.NotContains(t, string(raw), "INV-B")
	require.NotContains(t, string(raw), "assertion_id")
}

func TestAnalysisExplicitIdentityAliases(t *testing.T) {
	for _, name := range []string{"transaction_id", "bill_number", "invoice_no", "订单号", "claim_id", "statement_number"} {
		t.Run(name, func(t *testing.T) {
			d := testutil.NewDB(t)
			ctx := context.Background()
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			_ = archiveActor(t, d)
			a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
			_, f, x := publicationFixture(t, d, r, "identity-alias", domain.ArchiveInputText)
			f = saveBundle(t, r, f, analysisInvoiceCSVBundle(t, "项目,金额,币种,标识\n费用,10.00,CNY,ID-A\n合计,10.00,CNY,ID-B\n", name))
			_, e := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "UTC"})
			require.NoError(t, e)
			require.Len(t, result.Groups, 1)
			require.Equal(t, "20.00", result.Groups[0].Value)
		})
	}
}

func TestAnalysisCorrectedInvoiceRetainsOriginalBoundaryEvidence(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	editor := archiveActor(t, d)
	a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	rec, f, x := publicationFixture(t, d, r, "corrected-source-identity", domain.ArchiveInputText)
	f = saveBundle(t, r, f, analysisInvoiceCSVBundle(t, "项目,金额,币种,账单号\n费用,10.00,CNY,INV-A\n合计,10.00,CNY,INV-B\n"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	obs, e := r.CurrentArchiveObservations(ctx, editor, "home-1", rec.DocumentID, false)
	require.NoError(t, e)
	id := ""
	for _, o := range obs {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(o.Body, &b))
		if b.Field.Name == "invoice_id" && b.Field.Raw == "INV-B" {
			id = o.ID
		}
	}
	require.NotEmpty(t, id)
	_, e = r.ReviseArchive(ctx, editor, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: id, Value: "INV-A", Reason: "correct original invoice"})
	require.NoError(t, e)
	result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "UTC"})
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "10.00", result.Groups[0].Value)
	raw, e := json.Marshal(result.Groups[0].Evidence)
	require.NoError(t, e)
	require.Contains(t, string(raw), "INV-B")
	require.Contains(t, string(raw), "INV-A")
	require.Contains(t, string(raw), "assertion_id")
}
