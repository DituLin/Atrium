package archive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestPublicationUsesPhysicalParentAnchors(t *testing.T) {
	text := "体积70 ml"
	parsed, e := Parse(context.Background(), ParseSource{Reader: strings.NewReader(text), Size: int64(len(text)), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(text))), FormatHint: "text"}, DefaultParseLimits())
	require.NoError(t, e)
	plan, e := PlanChunks(parsed)
	require.NoError(t, e)
	c := plan.Chunks[0]
	v := c.Owned[0]
	x := validExtraction(c)
	x.Fields = []ExtractedField{{ID: "f", Name: "volume", Raw: "70", SubjectRefs: []string{}, Dimensions: []LiteralDimension{{Kind: "unit", Raw: "ml", Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 5, End: 7, Quote: "ml"}}}}, Normalizations: []Normalization{{Kind: "decimal", Value: "70"}}, Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 2, End: 4, Quote: "70"}}}}
	x.Events = []EventProposal{{ID: "e", Type: "measurement", FieldRefs: []string{"f"}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{{ViewID: v.ViewID, Start: 0, End: 7, Quote: text}}}}
	x.Coverage[0].Disposition = "extracted"
	ch, e := ValidateChunk(plan, c, extractionBytes(x))
	require.NoError(t, e)
	merged, e := MergeChunks(plan, []ChunkUnderstanding{ch})
	require.NoError(t, e)
	out, e := CompilePublication("h", "d", parsed, plan, merged)
	require.NoError(t, e)
	require.Len(t, out.Events, 1)
	require.Len(t, out.Events[0].Fields, 1)
	require.Equal(t, parsed.Segments[0].Locator, out.Events[0].Anchors[0].Locator)
	require.Equal(t, v.ParentSegmentID, out.Events[0].Fields[0].Evidence[0].ParentSegmentID)
	require.Equal(t, v.ParentSegmentID, out.Events[0].Fields[0].Dimensions[0].Evidence[0].ParentSegmentID)
	require.NotContains(t, out.Events[0].AnchorKey, "chunk")
	merged.Digest = "tampered"
	_, e = CompilePublication("h", "d", parsed, plan, merged)
	require.Error(t, e)
}
func TestPublicationRoleBridgeKeepsDoctorLocalAndBirthEvidence(t *testing.T) {
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
	x.Fields = []ExtractedField{{ID: "n", Name: "doctor_name", Raw: "王华", Evidence: ref(3, 5), SubjectRefs: []string{"doctor"}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{}}, {ID: "dob", Name: "birth_date", Raw: "1988-03-12", Evidence: ref(6, 16), SubjectRefs: []string{"patient"}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{{Kind: "date", Value: "1988-03-12"}}}, {ID: "v", Name: "volume", Raw: "70", Evidence: ref(17, 19), SubjectRefs: []string{"patient"}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{{Kind: "decimal", Value: "70"}}}}
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
func TestPublicationPDFAnchorRequiresUniquePhysicalCoordinateMatch(t *testing.T) {
	crop := Rect{0, 0, 600, 800}
	g := &PageGeometry{Page: 1, Rotation: 0, EffectiveCrop: &crop, Width: 600, Height: 800}
	a := []PhysicalAnchor{{Locator: Locator{Kind: "page", Page: &PageLocation{Page: 1, Rect: Rect{.1, .2, .5, .04}}}, Component: "body", Start: 0, End: 10, Geometry: g}}
	b := []PhysicalAnchor{{Locator: Locator{Kind: "page", Page: &PageLocation{Page: 1, Rect: Rect{.101, .2, .5, .04}}}, Component: "body", Start: 0, End: 11, Geometry: g}}
	require.True(t, PhysicalAnchorsMatch(a, b))
	rotated := *g
	rotated.Rotation = 90
	b[0].Geometry = &rotated
	require.False(t, PhysicalAnchorsMatch(a, b))
	b[0].Geometry = g
	b[0].Locator.Page.Page = 2
	require.False(t, PhysicalAnchorsMatch(a, b))
}
func TestPublicationSubjectSemanticsAreNotNumericGate(t *testing.T) {
	require.True(t, archiveSubjectField("patient", "diagnosis"))
	require.True(t, archiveSubjectField("student", "comment"))
	require.False(t, archiveSubjectField("doctor", "license_number"))
	require.False(t, archiveSubjectField("engineer", "fee"))
	require.False(t, archiveSubjectField("doctor", "doctor_name"))
	require.True(t, archiveSubjectField("owner", "full_address"))
}
func TestTransactionProofRequiresCompleteSupportedIssuerIdentity(t *testing.T) {
	fields := []PublicationField{{Name: "transaction_id", Raw: "00012", Evidence: []CanonicalEvidence{{Quote: "00012"}}}, {Name: "transaction_type", Raw: "payment", Evidence: []CanonicalEvidence{{Quote: "payment"}}}, {Name: "issuer", Raw: "Bank A", Evidence: []CanonicalEvidence{{Quote: "Bank A"}}}}
	p := TransactionIdentity(fields)
	require.NotNil(t, p)
	require.Equal(t, "00012", p.Identifier)
	require.Nil(t, TransactionIdentity(fields[:2]))
	fields = append(fields, PublicationField{Name: "transaction_id", Raw: "00013"})
	require.Nil(t, TransactionIdentity(fields))
}
func TestProtectedIdentityBridgeRequiresIssuerAndScopedOwner(t *testing.T) {
	fields := []PublicationField{{Name: "national_id", Raw: "00012", Subjects: []string{"person"}, Evidence: []CanonicalEvidence{{Quote: "00012"}}}, {Name: "identity_issuer", Raw: "SG", Subjects: []string{"person"}, Evidence: []CanonicalEvidence{{Quote: "SG"}}}}
	p := ProtectedIdentityValues(fields)
	require.Len(t, p, 1)
	require.Equal(t, "00012", p[0].Value)
	require.Equal(t, "SG", p[0].Issuer)
	require.Empty(t, ProtectedIdentityValues(fields[:1]))
	fields[1].Subjects = []string{"other"}
	require.Empty(t, ProtectedIdentityValues(fields))
}

func TestPublicationDerivedEvidenceRetainsTypedDependencies(t *testing.T) {
	parsed := understandingParse("151.234")
	display := "151.23"
	parsed.Segments[0].Locator = Locator{Kind: "xlsx", XLSX: &XLSXLocation{PartURI: "xl/worksheets/sheet1.xml", Cell: "A1"}}
	parsed.Segments[0].Cell = &CellEvidence{State: "value", Type: "n", ValuePresent: true, RawValue: "151.234", DisplayValue: &display, NumberFormatCode: "0.00"}
	p, e := PlanChunks(parsed)
	require.NoError(t, e)
	c := p.Chunks[0]
	x := validExtraction(c)
	var ref EvidenceReference
	for _, v := range c.Owned {
		if v.Derived != nil {
			ref = EvidenceReference{ViewID: v.ViewID, DerivedValueRef: v.Derived.Ref}
			for i := range x.Coverage {
				if x.Coverage[i].ViewID == v.ViewID {
					x.Coverage[i].Disposition = "extracted"
				}
			}
		}
	}
	x.Fields = []ExtractedField{{ID: "f", Name: "reading", Raw: display, SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{}, Evidence: []EvidenceReference{ref}}}
	x.Events = []EventProposal{{ID: "e", Type: "measurement", FieldRefs: []string{"f"}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{ref}}}
	ch, e := ValidateChunk(p, c, extractionBytes(x))
	require.NoError(t, e)
	u, e := MergeChunks(p, []ChunkUnderstanding{ch})
	require.NoError(t, e)
	out, e := CompilePublication("h", "d", parsed, p, u)
	require.NoError(t, e)
	require.Len(t, out.Events[0].Fields[0].DerivedDependencies, 1)
	require.Contains(t, out.Events[0].Fields[0].DerivedDependencies[0].OriginalComponentID, "numeric_raw")
	require.Contains(t, out.Events[0].Fields[0].DerivedDependencies[0].FormatComponentID, "format_code")
}
