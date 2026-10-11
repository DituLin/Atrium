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

func labeledBillBundle(t *testing.T, label, issuer, date string) store.ArchivePublicationBundle {
	text := label + "：TEST-BILL-001\n收款方：" + issuer + "\n日期：" + date + "\n费用：128.10 CNY"
	p, e := archive.Parse(context.Background(), archive.ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "text"}, archive.DefaultParseLimits())
	require.NoError(t, e)
	plan, e := archive.PlanChunks(p)
	require.NoError(t, e)
	c := plan.Chunks[0]
	ref := func(raw string) []archive.EvidenceReference {
		i := strings.Index(text, raw)
		start := len([]rune(text[:i]))
		return []archive.EvidenceReference{{ViewID: c.Owned[0].ViewID, Start: start, End: start + len([]rune(raw)), Quote: raw}}
	}
	x := archive.Extraction{Labels: []string{}, Domain: "finance", DocumentType: "bill", Subjects: []archive.LocalSubject{}, Fields: []archive.ExtractedField{}, Events: []archive.EventProposal{}, Coverage: []archive.UnitCoverage{{ViewID: c.Owned[0].ViewID, Disposition: "extracted"}}, Issues: []archive.ExtractionIssue{}}
	ids := []string{}
	for i, row := range [][2]string{{"bill_id", "TEST-BILL-001"}, {"issuer", issuer}, {"bill_date", date}, {"amount", "128.10"}} {
		id := fmt.Sprint(i)
		ids = append(ids, id)
		f := archive.ExtractedField{ID: id, Name: row[0], Raw: row[1], Evidence: ref(row[1]), SubjectRefs: []string{}, Dimensions: []archive.LiteralDimension{}, Normalizations: []archive.Normalization{}}
		if row[0] == "amount" {
			f.Dimensions = []archive.LiteralDimension{{Kind: "currency", Raw: "CNY", Evidence: ref("CNY")}}
			f.Normalizations = []archive.Normalization{{Kind: "decimal", Value: row[1]}}
		}
		x.Fields = append(x.Fields, f)
	}
	x.Events = []archive.EventProposal{{ID: "event", Type: "bill", FieldRefs: ids, SubjectRefs: []string{}, SectionEvidence: ref(text)}}
	raw, _ := json.Marshal(x)
	chunk, e := archive.ValidateChunk(plan, c, raw)
	require.NoError(t, e)
	u, e := archive.MergeChunks(plan, []archive.ChunkUnderstanding{chunk})
	require.NoError(t, e)
	return store.ArchivePublicationBundle{Parsed: p, Plan: plan, Understanding: u}
}
func TestBillReferenceSharingHasDateAndIssuerFences(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	for i, row := range [][3]string{{"账单编号", "示例物业", "2026-10-01"}, {"付款所引用账单编号", "示例物业", "2026-10-01"}, {"账单编号", "别的物业", "2026-10-01"}, {"账单编号", "示例物业", "2026-10-02"}} {
		_, f, x := publicationFixture(t, d, r, fmt.Sprint(i), domain.ArchiveInputText)
		f = saveBundle(t, r, f, labeledBillBundle(t, row[0], row[1], row[2]))
		task, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		if i == 3 {
			require.Equal(t, domain.ArchiveTaskWaitingInput, task.Status)
		} else {
			require.Equal(t, domain.ArchiveTaskSucceeded, task.Status)
		}
	}
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations WHERE slot_key='supported-transaction-amount/1'`).Scan(&n))
	require.Equal(t, 2, n)
}
