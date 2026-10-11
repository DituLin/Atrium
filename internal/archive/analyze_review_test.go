package archive

import "testing"

func TestAnalysisReviewUnrelatedTotalInSameDocument(t *testing.T) {
	a, b := af("item", "10.00", ""), af("total", "20.00", "")
	a.DocumentID = "compound"
	b.DocumentID = "compound"
	a.Domain = "finance"
	b.Domain = "finance"
	a.Currency = "CNY"
	b.Currency = "USD"
	a.Role = "line_item"
	b.Role = "total"
	a.RoleEvidence = a.Evidence
	b.RoleEvidence = b.Evidence
	r, e := Analyze(AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "UTC"}, AnalysisInput{Facts: []AnalysisFact{a, b}, Complete: true})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Groups) != 2 {
		t.Fatalf("independent USD bill total discarded merely because same file has CNY line item: %+v", r)
	}
}

func TestAnalysisReviewQualifiedDenominator(t *testing.T) {
	f := af("score", "88", "points")
	f.Denominator = "100"
	f.DenominatorEvidence = f.Evidence
	f.DenominatorNonExact = true
	r, e := Analyze(AnalysisQuery{Operation: "ratio", Metric: "volume", Timezone: "UTC"}, AnalysisInput{Facts: []AnalysisFact{f}, Complete: true})
	if e != nil || len(r.Groups) != 0 || r.Coverage.Complete {
		t.Fatalf("qualified denominator became exact: %+v %v", r, e)
	}
}

func TestAnalysisExplicitBoundaryRequiredForAggregate(t *testing.T) {
	for _, axis := range []string{"invoice", "transaction", "owners", "event_date", "unknown", "missing"} {
		t.Run(axis, func(t *testing.T) {
			a, b := af("a", "10.00", ""), af("b", "10.00", "")
			a.Currency = "CNY"
			b.Currency = "CNY"
			a.Role = "line_item"
			b.Role = "total"
			a.RoleEvidence = a.Evidence
			b.RoleEvidence = b.Evidence
			a.AggregateBoundary = map[string]string{axis: "A"}
			b.AggregateBoundary = map[string]string{axis: "B"}
			if axis == "unknown" {
				a.AggregateBoundaryUnknown = true
			}
			if axis == "missing" {
				b.AggregateBoundary = nil
			}
			b.AggregateMembers = []string{"a"}
			b.AggregateEvidence = b.Evidence
			r, e := Analyze(AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "UTC"}, AnalysisInput{Facts: []AnalysisFact{a, b}, Complete: true})
			if e != nil || len(r.Groups) != 1 || r.Groups[0].Value != "20.00" {
				t.Fatalf("boundary %s overwritten by aggregation: %+v %v", axis, r, e)
			}
		})
	}
}
