package archive

import "testing"

func af(id, value, unit string) AnalysisFact {
	return AnalysisFact{DocumentID: "d" + id, EventID: "e" + id, ObservationID: id, Revision: 1, Metric: "volume", Value: value, Unit: unit, Date: "2026-10-04", Evidence: []SearchEvidence{{DocumentID: "d" + id, ObservationID: id, ObservationRevision: 1, SegmentID: "s", Excerpt: value, Confirmed: true}}}
}
func TestAnalyzeExactAndDeduplicatesCanonicalSlot(t *testing.T) {
	a, b := af("a", "0.10", "ml"), af("b", "0.20", "ml")
	r, e := Analyze(AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "Asia/Singapore"}, AnalysisInput{Facts: []AnalysisFact{a, b, a}, Complete: true})
	if e != nil || len(r.Groups) != 1 || r.Groups[0].Value != "0.30" || len(r.Groups[0].Evidence) != 2 {
		t.Fatalf("exact unique sum: %+v %v", r, e)
	}
}
func TestAnalyzeCurrenciesRolesAndIncompleteCoverage(t *testing.T) {
	a, b, c, d := af("a", "128.10", ""), af("b", "71.90", ""), af("c", "200.00", ""), af("d", "20.00", "")
	for _, f := range []*AnalysisFact{&a, &b, &c, &d} {
		f.Metric = "charge"
		f.Domain = "finance"
		f.Currency = "CNY"
		f.Role = "line_item"
		f.RoleEvidence = f.Evidence
	}
	c.Role = "total"
	c.AggregateMembers = []string{a.ObservationID, b.ObservationID}
	c.AggregateEvidence = c.RoleEvidence
	c.EventID = a.EventID
	b.EventID = a.EventID
	d.Currency = "USD"
	unknown := af("u", "99.00", "")
	unknown.Domain = "finance"
	unknown.Metric = "charge"
	unknown.Currency = "CNY"
	r, e := Analyze(AnalysisQuery{Operation: "sum", Metric: "charge", Timezone: "Asia/Singapore"}, AnalysisInput{Facts: []AnalysisFact{a, b, c, d, unknown}, Complete: true})
	if e != nil || len(r.Groups) != 2 || r.Groups[0].Value != "200.00" || r.Groups[1].Value != "20.00" || r.Coverage.Complete {
		t.Fatalf("roles/currencies %+v %v", r, e)
	}
}
func TestAnalyzeRatioAndCoarseLatest(t *testing.T) {
	a, b := af("a", "88", "points"), af("b", "132", "points")
	a.Denominator = "100"
	b.Denominator = "150"
	a.DenominatorEvidence = a.Evidence
	b.DenominatorEvidence = b.Evidence
	r, e := Analyze(AnalysisQuery{Operation: "ratio", Metric: "volume", Timezone: "Asia/Singapore"}, AnalysisInput{Facts: []AnalysisFact{a, b}, Complete: true})
	if e != nil || len(r.Groups) != 2 || r.Groups[0].Value != "88.00" || r.Groups[1].Value != "88.00" {
		t.Fatalf("ratio %+v %v", r, e)
	}
	a.Date = "2026-10"
	b.Date = "2026-10-04"
	r, e = Analyze(AnalysisQuery{Operation: "latest", Metric: "volume", Timezone: "Asia/Singapore"}, AnalysisInput{Facts: []AnalysisFact{a, b}, Complete: true})
	if e != nil || len(r.Groups) != 2 || r.Coverage.Complete {
		t.Fatalf("ambiguous latest %+v %v", r, e)
	}
}
func TestAnalyzeDateBoundsExactnessAndCount(t *testing.T) {
	cases := []struct {
		name     string
		q        AnalysisQuery
		facts    []AnalysisFact
		groups   int
		complete bool
	}{
		{"precision overlap", AnalysisQuery{Operation: "sum", Metric: "volume", DateFrom: "2026-10-04", DateTo: "2026-10-04"}, []AnalysisFact{func() AnalysisFact { f := af("a", "70", "ml"); f.Date = "2026-10"; return f }()}, 0, false},
		{"non exact", AnalysisQuery{Operation: "sum", Metric: "volume"}, []AnalysisFact{af("a", ">70", "ml")}, 0, false},
		{"zero denominator", AnalysisQuery{Operation: "ratio", Metric: "volume"}, []AnalysisFact{func() AnalysisFact {
			f := af("a", "70", "ml")
			f.Denominator = "0"
			f.DenominatorEvidence = f.Evidence
			return f
		}()}, 0, false},
		{"event count", AnalysisQuery{Operation: "count", Count: "events"}, []AnalysisFact{af("a", "70", "ml"), af("a", "70", "ml")}, 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.q.Timezone = "Asia/Singapore"
			r, e := Analyze(c.q, AnalysisInput{Facts: c.facts, Complete: true})
			if e != nil || len(r.Groups) != c.groups || r.Coverage.Complete != c.complete {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
}
func TestAnalyzeDifferentValuesInSameSlotFail(t *testing.T) {
	a, b := af("a", "70", "ml"), af("a", "80", "ml")
	if _, e := Analyze(AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "UTC"}, AnalysisInput{Facts: []AnalysisFact{a, b}, Complete: true}); e == nil {
		t.Fatal("conflicting slot counted")
	}
}
func TestAnalyzeTotalDifferentEventFromLineItems(t *testing.T) {
	a, b, total := af("a", "128.10", ""), af("b", "71.90", ""), af("c", "200.00", "")
	for _, f := range []*AnalysisFact{&a, &b, &total} {
		f.DocumentID = "one-statement"
		f.Currency = "CNY"
		f.Domain = "finance"
		f.Role = "line_item"
		f.RoleEvidence = f.Evidence
	}
	total.Role = "total"
	total.AggregateMembers = []string{a.ObservationID, b.ObservationID}
	total.AggregateEvidence = total.RoleEvidence
	r, e := Analyze(AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "UTC"}, AnalysisInput{Facts: []AnalysisFact{a, b, total}, Complete: true})
	if e != nil || len(r.Groups) != 1 || r.Groups[0].Value != "200.00" {
		t.Fatalf("double counted total %+v %v", r, e)
	}
}
func TestAnalyzeSharedPaymentProofDoesNotSuppressCharge(t *testing.T) {
	payment, charge := af("shared", "200.00", ""), af("shared", "200.00", "")
	payment.Domain = "finance"
	charge.Domain = "finance"
	payment.Currency = "CNY"
	charge.Currency = "CNY"
	payment.Role = "payment"
	charge.Role = "total"
	payment.RoleEvidence = payment.Evidence
	charge.RoleEvidence = charge.Evidence
	for _, facts := range [][]AnalysisFact{{payment, charge}, {charge, payment}} {
		r, e := Analyze(AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "UTC"}, AnalysisInput{Facts: facts, Complete: true})
		if e != nil || len(r.Groups) != 1 || r.Groups[0].Value != "200.00" {
			t.Fatalf("proof order changed charge %+v %v", r, e)
		}
	}
}
func TestAnalyzeRatioChangeUsesExactFractions(t *testing.T) {
	a, b := af("a", "88", "points"), af("b", "132", "points")
	a.Date = "2026-04-01"
	b.Date = "2026-10-01"
	a.Denominator = "100"
	b.Denominator = "150"
	a.DenominatorEvidence = a.Evidence
	b.DenominatorEvidence = b.Evidence
	r, e := Analyze(AnalysisQuery{Operation: "ratio", Metric: "volume", Timezone: "Asia/Singapore"}, AnalysisInput{Facts: []AnalysisFact{a, b}, Complete: true})
	if e != nil || len(r.Groups) != 2 || r.Groups[1].ChangePercentPoints != "0.00" {
		t.Fatalf("ratio change %+v %v", r, e)
	}
}
func TestAnalyzeUnknownUnitsNeverBecomeOneSum(t *testing.T) {
	a, b := af("a", "70", ""), af("b", "1", "")
	r, e := Analyze(AnalysisQuery{Operation: "sum", Metric: "volume", Timezone: "UTC"}, AnalysisInput{Facts: []AnalysisFact{a, b}, Complete: true})
	if e != nil || r.Coverage.Complete || len(r.Groups) != 2 {
		t.Fatalf("unknown dimensions combined: %+v %v", r, e)
	}
}
