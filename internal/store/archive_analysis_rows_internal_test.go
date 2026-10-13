package store

import (
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestScoreRowBindingIgnoresHeadersAndRejectsAmbiguousValueLocations(t *testing.T) {
	p := archive.ParseResult{Segments: []archive.Segment{
		{ID: "header", Locator: archive.Locator{Kind: "xlsx", XLSX: &archive.XLSXLocation{PartURI: "/sheet1.xml", Cell: "B1"}}},
		{ID: "math", Locator: archive.Locator{Kind: "xlsx", XLSX: &archive.XLSXLocation{PartURI: "/sheet1.xml", Cell: "B2"}}},
		{ID: "mathmax", Locator: archive.Locator{Kind: "xlsx", XLSX: &archive.XLSXLocation{PartURI: "/sheet1.xml", Cell: "C2"}}},
		{ID: "langmax", Locator: archive.Locator{Kind: "xlsx", XLSX: &archive.XLSXLocation{PartURI: "/sheet1.xml", Cell: "C3"}}},
	}}
	math := archive.PublicationField{Raw: "88", Evidence: []archive.CanonicalEvidence{{ParentSegmentID: "math", Quote: "88"}, {ParentSegmentID: "header", Quote: "得分"}}}
	max := archive.PublicationField{Raw: "100", Evidence: []archive.CanonicalEvidence{{ParentSegmentID: "mathmax", Quote: "100"}}}
	require.False(t, analysisDifferentValueRows(p, math, p, max))
	max.Evidence[0].ParentSegmentID = "langmax"
	require.True(t, analysisDifferentValueRows(p, math, p, max))
	max.Evidence = append(max.Evidence, archive.CanonicalEvidence{ParentSegmentID: "mathmax", Quote: "100"})
	require.False(t, analysisDifferentValueRows(p, math, p, max), "ambiguous repeated values cannot be selectively assigned")
	math.Evidence[0].ParentSegmentID = "unavailable"
	require.False(t, analysisDifferentValueRows(p, math, p, max), "unknown value location cannot inherit header row")
}
func TestAnalysisDateRolesExcludeBirthAndWarranty(t *testing.T) {
	for _, s := range []string{"记录日期", "record_date", "bill_date", "账单日期", "付款日期"} {
		require.True(t, analysisEventDateField(s), s)
	}
	for _, s := range []string{"birth_date", "出生日期", "warranty_end", "保修截止日期", "due_date"} {
		require.False(t, analysisEventDateField(s), s)
	}
}
