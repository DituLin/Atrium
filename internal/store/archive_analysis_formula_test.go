package store_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"strings"
	"testing"
)

func analysisFormulaBundle(t *testing.T, data []byte) store.ArchivePublicationBundle {
	ctx := context.Background()
	parsed, e := archive.Parse(ctx, archive.ParseSource{Reader: bytes.NewReader(data), Size: int64(len(data)), SHA256: strings.Repeat("a", 64), FormatHint: "xlsx"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	plan, e := archive.PlanChunks(parsed)
	require.NoError(t, e)
	ch := archive.UnderstandingChunk{}
	for _, pc := range plan.Chunks {
		ch.Owned = append(ch.Owned, pc.Owned...)
	}
	refs := map[string]archive.EvidenceReference{}
	used := map[string]bool{}
	for _, v := range ch.Owned {
		if v.Structure.Locator.XLSX != nil {
			cell := v.Structure.Locator.XLSX.Cell
			refs[cell+":"+v.ComponentKind] = archive.EvidenceReference{ViewID: v.ViewID, Start: v.Start, End: v.End, Quote: v.Text}
		}
	}
	get := func(cell string) archive.EvidenceReference {
		for _, kind := range []string{"numeric_raw", "historical_cache", "cell_text"} {
			if v, ok := refs[cell+":"+kind]; ok {
				used[v.ViewID] = true
				return v
			}
		}
		t.Fatalf("no ref %s in %+v", cell, refs)
		return archive.EvidenceReference{}
	}
	ext := archive.Extraction{Domain: "finance", DocumentType: "statement", Labels: []string{}, Subjects: []archive.LocalSubject{}, Fields: []archive.ExtractedField{}, Events: []archive.EventProposal{}, Coverage: []archive.UnitCoverage{}, Issues: []archive.ExtractionIssue{}}
	for row := 2; row <= 4; row++ {
		num := get(fmt.Sprintf("B%d", row))
		cur := get(fmt.Sprintf("C%d", row))
		label := get(fmt.Sprintf("A%d", row))
		id := fmt.Sprintf("amount%d", row)
		ext.Fields = append(ext.Fields, archive.ExtractedField{ID: id, Name: "amount", Raw: num.Quote, SubjectRefs: []string{}, Normalizations: []archive.Normalization{{Kind: "decimal", Value: num.Quote}}, Evidence: []archive.EvidenceReference{num}, Dimensions: []archive.LiteralDimension{{Kind: "currency", Raw: cur.Quote, Evidence: []archive.EvidenceReference{cur}}}})
		ids := []string{id}
		section := []archive.EvidenceReference{label, num, cur}
		if row < 4 {
			for _, axis := range []struct{ col, name string }{{"D", "invoice_id"}, {"E", "payee"}, {"F", "transaction_date"}} {
				ref := get(fmt.Sprintf("%s%d", axis.col, row))
				fid := axis.name + fmt.Sprint(row)
				norm := []archive.Normalization{}
				if axis.name == "transaction_date" {
					norm = append(norm, archive.Normalization{Kind: "date", Value: ref.Quote})
				}
				ext.Fields = append(ext.Fields, archive.ExtractedField{ID: fid, Name: axis.name, Raw: ref.Quote, SubjectRefs: []string{}, Normalizations: norm, Evidence: []archive.EvidenceReference{ref}, Dimensions: []archive.LiteralDimension{}})
				ids = append(ids, fid)
				section = append(section, ref)
			}
		} else {
			ref, ok := refs["B4:formula_text"]
			require.True(t, ok)
			used[ref.ViewID] = true
			section = append(section, ref)
		}
		ext.Events = append(ext.Events, archive.EventProposal{ID: "e" + id, Type: "expense", FieldRefs: ids, SubjectRefs: []string{}, SectionEvidence: section})
	}
	for _, v := range ch.Owned {
		disp := "irrelevant"
		if used[v.ViewID] {
			disp = "extracted"
		}
		ext.Coverage = append(ext.Coverage, archive.UnitCoverage{ViewID: v.ViewID, Disposition: disp})
	}
	chunks := []archive.ChunkUnderstanding{}
	for _, pc := range plan.Chunks {
		part := ext
		part.Fields = []archive.ExtractedField{}
		part.Events = []archive.EventProposal{}
		part.Coverage = []archive.UnitCoverage{}
		owned := map[string]bool{}
		ids := map[string]bool{}
		for _, v := range pc.Owned {
			owned[v.ViewID] = true
			disp := "irrelevant"
			if used[v.ViewID] {
				disp = "extracted"
			}
			part.Coverage = append(part.Coverage, archive.UnitCoverage{ViewID: v.ViewID, Disposition: disp})
		}
		for _, field := range ext.Fields {
			if owned[field.Evidence[0].ViewID] {
				part.Fields = append(part.Fields, field)
				ids[field.ID] = true
			}
		}
		for _, event := range ext.Events {
			if ids[event.FieldRefs[0]] {
				part.Events = append(part.Events, event)
			}
		}
		raw, err := json.Marshal(part)
		require.NoError(t, err)
		chunk, err := archive.ValidateChunk(plan, pc, raw)
		require.NoError(t, err, "chunk %d", pc.Index)
		chunks = append(chunks, chunk)
	}
	merged, e := archive.MergeChunks(plan, chunks)
	require.NoError(t, e)
	return store.ArchivePublicationBundle{Parsed: parsed, Plan: plan, Understanding: merged}
}
func TestAnalysisF01XLSXExplicitFormula(t *testing.T) {
	ctx := context.Background()
	data, e := os.ReadFile("testdata/analysis-f01.xlsx")
	require.NoError(t, e)
	bundle := analysisFormulaBundle(t, data)
	require.Len(t, bundle.Plan.Chunks, 4)

	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_ = archiveActor(t, d)
	a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
	_, f, x := publicationFixture(t, d, r, "f01-formula", domain.ArchiveInputText)
	f = saveBundle(t, r, f, bundle)
	_, e = r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	result, e := r.AnalyzeArchives(ctx, a, "home-1", false, archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "UTC"})
	require.NoError(t, e)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "200.00", result.Groups[0].Value, "explicit formula SUM(B2:B3) cache is not another independent charge")
	proof, e := json.Marshal(result.Groups[0].Evidence)
	require.NoError(t, e)
	for _, text := range []string{"SUM(B2:B3)", "historical_cache", "formula_text", "TEST-BILL-001", "TEST-BILL-002"} {
		require.Contains(t, string(proof), text)
	}
}

func analysisRewriteSheet(t *testing.T, data []byte, old, new string) []byte {
	t.Helper()
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, e)
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, f := range z.File {
		r, e := f.Open()
		require.NoError(t, e)
		body, e := io.ReadAll(r)
		require.NoError(t, e)
		require.NoError(t, r.Close())
		if f.Name == "sheets/data.xml" {
			require.Contains(t, string(body), old)
			body = []byte(strings.ReplaceAll(string(body), old, new))
		}
		entry, e := w.Create(f.Name)
		require.NoError(t, e)
		_, e = entry.Write(body)
		require.NoError(t, e)
	}
	require.NoError(t, w.Close())
	return out.Bytes()
}
func TestAnalysisF01FormulaCurrentAndUntrustedInputs(t *testing.T) {
	for _, mode := range []string{"manual_member", "private_member", "withdraw", "stale_cache", "cross_sheet", "external", "unknown_formula", "missing_member", "self_reference", "currency"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			data, e := os.ReadFile("testdata/analysis-f01.xlsx")
			require.NoError(t, e)
			switch mode {
			case "stale_cache":
				data = analysisRewriteSheet(t, data, "<v>200.00</v>", "<v>199.99</v>")
			case "cross_sheet":
				data = analysisRewriteSheet(t, data, "SUM(B2:B3)", "SUM(Sheet2!B2:B3)")
			case "external":
				data = analysisRewriteSheet(t, data, "SUM(B2:B3)", "SUM([other.xlsx]Sheet1!B2:B3)")
			case "unknown_formula":
				data = analysisRewriteSheet(t, data, "SUM(B2:B3)", "AVERAGE(B2:B3)")
			case "missing_member":
				data = analysisRewriteSheet(t, data, "SUM(B2:B3)", "SUM(B2:B3,B5)")
			case "self_reference":
				data = analysisRewriteSheet(t, data, "SUM(B2:B3)", "SUM(B2:B4)")
			case "currency":
				data = analysisRewriteSheet(t, data, `<c r="C3" t="inlineStr"><is><t>CNY</t>`, `<c r="C3" t="inlineStr"><is><t>USD</t>`)
			}
			d := testutil.NewDB(t)
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			editor := archiveActor(t, d)
			a := newArchiveActor(t, d, "analyst", []string{"archive.read", "archive.analyze"}, []string{"home-1"})
			rec, f, x := publicationFixture(t, d, r, "formula-"+mode, domain.ArchiveInputText)
			f = saveBundle(t, r, f, analysisFormulaBundle(t, data))
			_, e = r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			q := archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "UTC"}
			prior, e := r.AnalyzeArchives(ctx, a, "home-1", false, q)
			require.NoError(t, e)
			if mode == "manual_member" || mode == "private_member" || mode == "withdraw" {
				obs, e := r.CurrentArchiveObservations(ctx, editor, "home-1", rec.DocumentID, false)
				require.NoError(t, e)
				id := ""
				for _, o := range obs {
					var b struct{ Field archive.PublicationField }
					require.NoError(t, json.Unmarshal(o.Body, &b))
					if b.Field.Name == "amount" && b.Field.Raw == "128.10" {
						id = o.ID
					}
				}
				require.NotEmpty(t, id)
				change := store.ArchiveRevision{Kind: "value", ObservationID: id, Value: "178.10", Reason: "current member"}
				if mode == "private_member" {
					change.Kind = "private"
					change.Value = ""
				}
				if mode == "withdraw" {
					change.Kind = "withdraw"
					change.ObservationID = ""
					change.Value = ""
				}
				_, e = r.ReviseArchive(ctx, editor, "home-1", rec.DocumentID, 2, change)
				require.NoError(t, e)
				_, e = r.ArchiveAnalysisSnapshot(ctx, a, "home-1", false, prior.SnapshotID)
				require.Error(t, e)
			}
			result, e := r.AnalyzeArchives(ctx, a, "home-1", mode == "private_member", q)
			require.NoError(t, e)
			if mode == "withdraw" {
				require.Empty(t, result.Groups)
				return
			}
			if mode == "currency" {
				require.Len(t, result.Groups, 2)
				require.Equal(t, "128.10", result.Groups[0].Value)
				require.Equal(t, "71.90", result.Groups[1].Value)
				require.Contains(t, result.Coverage.Reasons, "unverified_formula_cache")
				return
			}
			require.Len(t, result.Groups, 1)
			expected := "200.00"
			if mode == "manual_member" {
				expected = "250.00"
			}
			if mode == "private_member" {
				expected = "71.90"
			}
			require.Equal(t, expected, result.Groups[0].Value)
			raw, e := json.Marshal(result)
			require.NoError(t, e)
			if mode == "private_member" {
				require.NotContains(t, string(raw), "200.00")
				require.NotContains(t, string(raw), "128.10")
				return
			}
			if mode == "manual_member" || mode == "stale_cache" {
				require.NotContains(t, result.Coverage.Reasons, "unverified_formula_cache")
				require.Contains(t, string(raw), "formula_text")
				require.Contains(t, string(raw), "historical_cache")
				require.Contains(t, string(raw), "SUM(B2:B3)")
			} else {
				require.Contains(t, result.Coverage.Reasons, "unverified_formula_cache")
			}
		})
	}
}
