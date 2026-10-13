package archive

import (
	"context"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestSpecCanonicalBillRoleCannotCrossNewHeader(t *testing.T) {
	text := "收款方\n第一物业\n日期\n示例物业\n"
	p, e := Parse(context.Background(), ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "csv"}, DefaultParseLimits())
	require.NoError(t, e)
	plan, e := PlanChunks(p)
	require.NoError(t, e)
	var field PublicationField
	for _, c := range plan.Chunks {
		for _, v := range c.Owned {
			if v.Text == "示例物业" {
				field = PublicationField{Name: "issuer", Raw: v.Text, Evidence: []CanonicalEvidence{canonicalEvidence(v, EvidenceReference{Start: 0, End: len([]rune(v.Text)), Quote: v.Text})}}
			}
		}
	}
	require.NotEmpty(t, field.Raw)
	sourceBillLabels(p, &field)
	for _, ev := range field.Evidence {
		require.NotEqual(t, "收款方", ev.Quote, "model spelling cannot turn unrelated old header into bill proof")
	}
}

func TestModelCitedBillRoleCannotCrossNewHeader(t *testing.T) {
	text := "收款方\n第一物业\n日期\n示例物业\n"
	p, e := Parse(context.Background(), ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: strings.Repeat("a", 64), FormatHint: "csv"}, DefaultParseLimits())
	require.NoError(t, e)
	plan, e := PlanChunks(p)
	require.NoError(t, e)
	f := PublicationField{Name: "issuer", Raw: "示例物业"}
	for _, c := range plan.Chunks {
		for _, v := range c.Owned {
			if v.Text == "示例物业" || v.Text == "收款方" {
				f.Evidence = append(f.Evidence, canonicalEvidence(v, EvidenceReference{Start: 0, End: len([]rune(v.Text)), Quote: v.Text}))
			}
		}
	}
	before := append([]CanonicalEvidence(nil), f.Evidence...)
	sourceBillLabels(p, &f)
	require.True(t, f.RequiresReview)
	require.Equal(t, before, f.Evidence, "source quotes remain immutable; unrelated label cannot authorize role")
	require.Nil(t, billIdentity([]PublicationField{f}))
}
