package archive

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPublicationChineseBirthFieldRetainsIdentityProof(t *testing.T) {
	parsed := understandingParse("林明 王华 1988-03-12 70")
	parsed.Segments[0].Locator = Locator{Kind: "text", Text: &TextLocation{Start: 0, End: 19, Unit: "unicode_scalar"}}
	p, e := PlanChunks(parsed)
	require.NoError(t, e)
	c := p.Chunks[0]
	v := c.Owned[0]
	ref := func(start, end int) []EvidenceReference {
		return []EvidenceReference{{ViewID: v.ViewID, Start: start, End: end, Quote: string([]rune(parsed.Segments[0].Text)[start:end])}}
	}
	x := validExtraction(c)
	x.Subjects = []LocalSubject{{ID: "patient", Name: "林明", Role: "patient", Evidence: ref(0, 2)}, {ID: "doctor", Name: "王华", Role: "doctor", Evidence: ref(3, 5)}}
	x.Fields = []ExtractedField{{ID: "n", Name: "doctor_name", Raw: "王华", Evidence: ref(3, 5), SubjectRefs: []string{"doctor"}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{}}, {ID: "dob", Name: "出生日期", Raw: "1988-03-12", Evidence: ref(6, 16), SubjectRefs: []string{"patient"}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{{Kind: "date", Value: "1988-03-12"}}}, {ID: "v", Name: "volume", Raw: "70", Evidence: ref(17, 19), SubjectRefs: []string{"patient"}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{{Kind: "decimal", Value: "70"}}}}
	x.Coverage[0].Disposition = "extracted"
	ch, e := ValidateChunk(p, c, extractionBytes(x))
	require.NoError(t, e)
	u, e := MergeChunks(p, []ChunkUnderstanding{ch})
	require.NoError(t, e)
	out, e := CompilePublication("h", "d", parsed, p, u)
	require.NoError(t, e)
	require.True(t, out.Resolution.Mentions[0].Subject)
	require.Equal(t, "1988-03-12", out.Resolution.Mentions[0].Birth.Value)
	require.False(t, out.Resolution.Mentions[1].Subject)
	require.NotEqual(t, "0:patient", out.Resolution.Mentions[0].Ref)
}

func TestChinesePartialDateNormalizationIsSourceBound(t *testing.T) {
	for _, tc := range []struct {
		raw, value string
		valid      bool
	}{{"2026年10月", "2026-10", true}, {"2026年1月", "2026-01", true}, {"2026年", "2026", true}, {"10月", "2026-10", false}, {"2026年10月", "2026-10-01", false}, {"2026年13月", "2026-13", false}, {"2026年10月", "2027-10", false}} {
		t.Run(tc.raw+tc.value, func(t *testing.T) {
			p, e := PlanChunks(understandingParse(tc.raw))
			require.NoError(t, e)
			c := p.Chunks[0]
			x := validExtraction(c)
			x.Fields = []ExtractedField{{ID: "date", Name: "时间", Raw: tc.raw, SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: c.Owned[0].ViewID, Start: 0, End: len([]rune(tc.raw)), Quote: tc.raw}}, Normalizations: []Normalization{{Kind: "date_partial", Value: tc.value}}}}
			x.Coverage[0].Disposition = "extracted"
			_, e = ValidateChunk(p, c, extractionBytes(x))
			if tc.valid {
				require.NoError(t, e)
			} else {
				require.Error(t, e)
			}
		})
	}
}

func TestPublicationSemanticNamesPreserveModelLabels(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{"金额", "amount"}, {"费用", "amount"}, {"amount", "amount"}, {"奶量", "volume"}, {"volume", "volume"}, {"出生日期", "birth_date"}, {"账单编号", "bill_id"}, {"付款所引用账单编号", "bill_id"}, {"收款方", "issuer"}, {"未知开放字段", "未知开放字段"}} {
		require.Equal(t, tc.want, CanonicalFieldName(tc.raw))
	}
}

func TestCounterpartyRoleDoesNotCreateHouseholdSubject(t *testing.T) {
	for _, role := range []string{"payee", "issuer", "收款方", "收款人", "counterparty", "supplier"} {
		require.False(t, archiveSubjectField(role, "收款方"), role)
		require.False(t, archiveSubjectField(role, "amount"), role)
	}
	require.True(t, archiveSubjectField("account_holder", "amount"))
	require.True(t, archiveSubjectField("patient", "血红蛋白"))
}

func TestBillReferenceProofRequiresExplicitRoles(t *testing.T) {
	field := func(name, raw, label string) PublicationField {
		return PublicationField{Name: name, Raw: raw, Evidence: []CanonicalEvidence{{Quote: raw}, {Quote: label}}}
	}
	f := []PublicationField{field("bill_id", "TEST-BILL-001", "账单编号"), field("issuer", "示例物业", "收款方"), field("bill_date", "2026-10-01", "日期")}
	p := TransactionIdentity(f)
	require.NotNil(t, p)
	require.Equal(t, "bill", p.Type)
	require.Equal(t, "2026-10-01", p.Date)
	f[0].Evidence[1].Quote = "付款所引用账单编号"
	q := TransactionIdentity(f)
	require.NotNil(t, q)
	require.Equal(t, p.Identifier, q.Identifier)
	require.Equal(t, p.Issuer, q.Issuer)
	require.Equal(t, p.Type, q.Type)
	f[0].Evidence[1].Quote = "付款流水号"
	require.Nil(t, TransactionIdentity(f))
	f[0].Evidence[1].Quote = "账单编号"
	f[1].Evidence[1].Quote = "房屋"
	require.Nil(t, TransactionIdentity(f))
}
