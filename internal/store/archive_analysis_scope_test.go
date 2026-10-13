package store

import (
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAnalysisAggregatePhysicalScope(t *testing.T) {
	for _, axis := range []string{"currency", "unit", "sample", "table", "column", "amount", "contiguous"} {
		t.Run(axis, func(t *testing.T) {
			line := archive.AnalysisFact{DocumentID: "d", ObservationID: "a", EventID: "a", Metric: "amount", Currency: "CNY", Unit: "yuan", Sample: "project:A", Value: "10.00", Role: "line_item", Evidence: []archive.SearchEvidence{{Confirmed: true, Locator: archive.Locator{XLSX: &archive.XLSXLocation{PartURI: "sheet1", Cell: "B2"}}}}}
			line.RoleEvidence = line.Evidence
			total := line
			total.ObservationID = "b"
			total.EventID = "b"
			total.Role = "total"
			total.Evidence = []archive.SearchEvidence{{Confirmed: true, Locator: archive.Locator{XLSX: &archive.XLSXLocation{PartURI: "sheet1", Cell: "B3"}}}}
			total.RoleEvidence = total.Evidence
			switch axis {
			case "currency":
				total.Currency = "USD"
			case "unit":
				total.Unit = "dollars"
			case "sample":
				total.Sample = "project:B"
			case "table":
				total.Evidence[0].Locator.XLSX.PartURI = "sheet2"
			case "column":
				total.Evidence[0].Locator.XLSX.Cell = "C3"
			case "amount":
				total.Value = "20.00"
			case "contiguous":
				total.Evidence[0].Locator.XLSX.Cell = "B4"
			}
			facts := []archive.AnalysisFact{line, total}
			analysisTableAggregates(facts)
			result, e := archive.Analyze(archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "UTC"}, archive.AnalysisInput{Facts: facts, Complete: true})
			require.NoError(t, e)
			require.Equal(t, 2, result.Coverage.Included)
		})
	}
}
