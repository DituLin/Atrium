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
