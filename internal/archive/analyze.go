package archive

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

const AnalysisVersion = "archive-analysis/1"
const AnalysisAlgorithmVersion = "archive-analysis/2"

// AnalysisQuery is a bounded business query. Projection and authority are host supplied.
type AnalysisQuery struct {
	Operation    string `json:"operation"`
	Count        string `json:"count,omitempty"`
	Metric       string `json:"metric,omitempty"`
	EntityID     string `json:"entity_id,omitempty"`
	Domain       string `json:"domain,omitempty"`
	DocumentType string `json:"document_type,omitempty"`
	Query        string `json:"query,omitempty"`
	DateFrom     string `json:"date_from,omitempty"`
	DateTo       string `json:"date_to,omitempty"`
	Timezone     string `json:"timezone"`
}

func (q AnalysisQuery) Normalize() (AnalysisQuery, error) {
	q.Metric = strings.TrimSpace(q.Metric)
	q.Query = strings.TrimSpace(q.Query)
	if _, e := (SearchFilter{EntityID: q.EntityID, Domain: q.Domain, DocumentType: q.DocumentType, Query: q.Query, DateFrom: q.DateFrom, DateTo: q.DateTo}).Normalize(); e != nil {
		return q, e
	}
	if len(q.Metric) > 128 || strings.ContainsRune(q.Metric, 0) {
		return q, fmt.Errorf("invalid metric")
	}
	if _, e := time.LoadLocation(q.Timezone); e != nil || q.Timezone == "" {
		return q, fmt.Errorf("invalid timezone")
	}
	switch q.Operation {
	case "count":
		if q.Count != "documents" && q.Count != "events" && q.Count != "observations" {
			return q, fmt.Errorf("count target required")
		}
	case "sum", "latest", "series", "ratio":
		if q.Metric == "" || q.Count != "" {
			return q, fmt.Errorf("metric required")
		}
	default:
		return q, fmt.Errorf("invalid operation")
	}
	return q, nil
}

type AnalysisFact struct {
	DenominatorNonExact                                                    bool
	Unavailable                                                            bool
	NonExact                                                               bool
	AggregateMembers                                                       []string
	AggregateEvidence                                                      []SearchEvidence
	DocumentID, EventID, ObservationID                                     string
	Revision                                                               int64
	Metric, Value, Unit, Currency, Domain, Sample, Date, Role, Denominator string
	Evidence, RoleEvidence, DenominatorEvidence                            []SearchEvidence
}
type AnalysisInput struct {
	Facts     []AnalysisFact
	Documents []string
	Complete  bool
	Reasons   []string
}
type AnalysisCoverage struct {
	Scope    string   `json:"scope"`
	Complete bool     `json:"complete"`
	Included int      `json:"included"`
	Excluded int      `json:"excluded"`
	Reasons  []string `json:"reasons"`
}
type AnalysisGroup struct {
	ChangePercentPoints string           `json:"change_percent_points,omitempty"`
	BaselineDate        string           `json:"baseline_date,omitempty"`
	Metric              string           `json:"metric"`
	Unit                string           `json:"unit,omitempty"`
	Currency            string           `json:"currency,omitempty"`
	Sample              string           `json:"sample,omitempty"`
	Value               string           `json:"value"`
	Date                string           `json:"date,omitempty"`
	DatePrecision       string           `json:"date_precision,omitempty"`
	Numerator           string           `json:"numerator,omitempty"`
	Denominator         string           `json:"denominator,omitempty"`
	Rounding            string           `json:"rounding,omitempty"`
	Evidence            []SearchEvidence `json:"evidence"`
}
type AnalysisResult struct {
	SchemaVersion    string           `json:"schema_version"`
	SnapshotID       string           `json:"snapshot_id,omitempty"`
	AlgorithmVersion string           `json:"algorithm_version"`
	Query            AnalysisQuery    `json:"query"`
	Projection       string           `json:"projection"`
	DataEpoch        int64            `json:"data_epoch"`
	Generation       int64            `json:"generation"`
	InputDigest      string           `json:"input_digest"`
	Groups           []AnalysisGroup  `json:"groups"`
	Coverage         AnalysisCoverage `json:"coverage"`
}

var analysisDecimal = regexp.MustCompile(`^[+-]?[0-9]{1,72}(?:\.[0-9]{1,18})?$`)

func exactDecimal(s string) (*big.Rat, int, bool) {
	if !analysisDecimal.MatchString(s) {
		return nil, 0, false
	}
	r, ok := new(big.Rat).SetString(s)
	scale := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		scale = len(s) - i - 1
	}
	return r, scale, ok
}

// DateInterval represents source precision in the family's timezone, never a guessed instant.
func DateInterval(s, zone string) (time.Time, time.Time, string, bool) {
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return time.Time{}, time.Time{}, "", false
	}
	if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
		return t, t, "datetime", true
	}
	formats := []struct{ layout, precision string }{{"2006-01-02", "date"}, {"2006-01", "month"}, {"2006", "year"}}
	for _, f := range formats {
		t, e := time.ParseInLocation(f.layout, s, loc)
		if e != nil {
			continue
		}
		end := t
		switch f.precision {
		case "date":
			end = t.AddDate(0, 0, 1)
		case "month":
			end = t.AddDate(0, 1, 0)
		case "year":
			end = t.AddDate(1, 0, 0)
		}
		return t, end.Add(-time.Nanosecond), f.precision, true
	}
	return time.Time{}, time.Time{}, "unknown", false
}
func Analyze(q AnalysisQuery, in AnalysisInput) (AnalysisResult, error) {
	q, e := q.Normalize()
	r := AnalysisResult{SchemaVersion: AnalysisVersion, AlgorithmVersion: AnalysisAlgorithmVersion, Query: q, Groups: []AnalysisGroup{}, Coverage: AnalysisCoverage{Scope: "archived_only", Complete: in.Complete, Reasons: append([]string{}, in.Reasons...)}}
	if e != nil {
		return r, e
	}
	if len(in.Facts) > 4096 || len(in.Documents) > 4096 {
		return r, fmt.Errorf("analysis input bound")
	}
	reason := func(s string) {
		r.Coverage.Complete = false
		for _, v := range r.Coverage.Reasons {
			if v == s {
				return
			}
		}
		r.Coverage.Reasons = append(r.Coverage.Reasons, s)
	}
	facts := []AnalysisFact{}
	ordered := append([]AnalysisFact{}, in.Facts...)
	rolePriority := func(f AnalysisFact) int {
		if len(f.RoleEvidence) == 0 {
			return 3
		}
		switch f.Role {
		case "line_item":
			return 0
		case "total":
			return 1
		case "payment":
			return 2
		}
		return 3
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if rolePriority(ordered[i]) != rolePriority(ordered[j]) {
			return rolePriority(ordered[i]) < rolePriority(ordered[j])
		}
		return ordered[i].DocumentID < ordered[j].DocumentID
	})
	seen := map[string]AnalysisFact{}
	for _, f := range ordered {
		if q.Metric != "" && f.Metric != q.Metric {
			continue
		}
		if f.ObservationID == "" || f.EventID == "" || len(f.Evidence) == 0 {
			reason("missing_evidence")
			r.Coverage.Excluded++
			continue
		}
		slotKey := f.ObservationID
		if q.Operation == "count" && q.Count == "documents" {
			slotKey = f.DocumentID + ":" + slotKey
		}
		if prev, ok := seen[slotKey]; ok {
			if prev.Value != f.Value || prev.Revision != f.Revision || prev.Unit != f.Unit || prev.Currency != f.Currency || prev.Sample != f.Sample {
				return r, fmt.Errorf("inconsistent canonical slot")
			}
			continue
		}
		seen[slotKey] = f
		if q.DateFrom != "" || q.DateTo != "" {
			lo, hi, _, ok := DateInterval(f.Date, q.Timezone)
			from, _, _, _ := DateInterval(q.DateFrom, q.Timezone)
			_, to, _, _ := DateInterval(q.DateTo, q.Timezone)
			if !ok {
				reason("unknown_date")
				r.Coverage.Excluded++
				continue
			}
			if q.DateFrom != "" && hi.Before(from) || q.DateTo != "" && lo.After(to) {
				continue
			}
			if q.DateFrom != "" && lo.Before(from) || q.DateTo != "" && hi.After(to) {
				reason("overlapping_date_precision")
				r.Coverage.Excluded++
				continue
			}
		}
		facts = append(facts, f)
	}
	if q.Operation == "count" {
		ids := map[string]bool{}
		ev := []SearchEvidence{}
		cited := map[string]bool{}
		if q.Count == "documents" && q.Metric == "" && q.DateFrom == "" && q.DateTo == "" {
			for _, id := range in.Documents {
				ids[id] = true
			}
		}
		for _, f := range facts {
			id := f.ObservationID
			if q.Count == "documents" {
				id = f.DocumentID
			}
			if q.Count == "events" {
				id = f.EventID
			}
			ids[id] = true
			if !cited[id] {
				cited[id] = true
				ev = append(ev, f.Evidence...)
			}
		}
		if q.Count == "documents" {
			docIDs := []string{}
			for id := range ids {
				docIDs = append(docIDs, id)
			}
			sort.Strings(docIDs)
			for _, id := range docIDs {
				if !cited[id] {
					ev = append(ev, SearchEvidence{DocumentID: id, SourceKind: "saved_document", SegmentID: "metadata", Confirmed: true})
				}
			}
		}
		r.Groups = append(r.Groups, AnalysisGroup{Metric: q.Count, Value: fmt.Sprint(len(ids)), Evidence: ev})
		r.Coverage.Included = len(ids)
		return r, nil
	}
	type accum struct {
		group AnalysisGroup
		sum   *big.Rat
		scale int
		facts []AnalysisFact
	}
	groups := map[string]*accum{}
	byID := map[string]AnalysisFact{}
	for _, f := range facts {
		byID[f.ObservationID] = f
	}
	aggregateCovered := func(f AnalysisFact) bool {
		if f.NonExact || len(f.AggregateMembers) == 0 || len(f.AggregateEvidence) == 0 {
			return false
		}
		sum := new(big.Rat)
		members := map[string]bool{}
		for _, id := range f.AggregateMembers {
			m, ok := byID[id]
			if !ok || m.Unavailable || members[id] || m.Role != "line_item" || len(m.RoleEvidence) == 0 || m.NonExact || m.Metric != f.Metric || m.Currency != f.Currency || m.Unit != f.Unit || m.Sample != f.Sample {
				return false
			}
			n, _, ok := exactDecimal(m.Value)
			if !ok {
				return false
			}
			sum.Add(sum, n)
			members[id] = true
		}
		total, _, ok := exactDecimal(f.Value)
		return ok && total.Cmp(sum) == 0
	}
	financeEvents := map[string]bool{}
	for _, f := range facts {
		if f.Unavailable {
			reason("unavailable_dimension_evidence")
			r.Coverage.Excluded++
			continue
		}
		if (f.Domain == "finance" || f.Currency != "") && q.Operation == "sum" {
			if len(f.RoleEvidence) == 0 || (f.Role != "line_item" && f.Role != "total" && f.Role != "payment") {
				reason("unknown_amount_role")
				r.Coverage.Excluded++
				continue
			}
			if f.Role == "payment" || f.Role == "total" && aggregateCovered(f) {
				r.Coverage.Excluded++
				continue
			}
			if f.Role == "total" && !aggregateCovered(f) {
				for _, m := range facts {
					if m.Role == "line_item" && m.Metric == f.Metric && m.Currency == f.Currency && m.Unit == f.Unit && m.Sample == f.Sample {
						reason("unproven_summary_relation")
						break
					}
				}
			}
			financeEvents[f.EventID] = true
		}
		n, scale, ok := exactDecimal(f.Value)
		if !ok || f.NonExact {
			reason("non_exact_value")
			r.Coverage.Excluded++
			continue
		}
		key := f.Metric + "\x00" + f.Currency + "\x00" + f.Unit + "\x00" + f.Sample
		if q.Operation == "sum" && f.Unit == "" && f.Currency == "" {
			key += "\x00" + f.ObservationID
			reason("unknown_dimensions")
		}
		if q.Operation == "sum" && f.Domain == "education" {
			key += "\x00" + f.Denominator
		}
		if q.Operation == "series" || q.Operation == "ratio" {
			key += "\x00" + f.ObservationID
		}
		a := groups[key]
		if a == nil {
			a = &accum{group: AnalysisGroup{Metric: f.Metric, Currency: f.Currency, Unit: f.Unit, Sample: f.Sample, Evidence: []SearchEvidence{}}, sum: new(big.Rat)}
			groups[key] = a
		}
		a.sum.Add(a.sum, n)
		if scale > a.scale {
			a.scale = scale
		}
		a.facts = append(a.facts, f)
		a.group.Evidence = append(a.group.Evidence, f.Evidence...)
		a.group.Evidence = append(a.group.Evidence, f.RoleEvidence...)
		r.Coverage.Included++
	}
	if len(financeEvents) > 1 {
		reason("unproven_duplicate_candidates")
	}
	keys := []string{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := groups[k]
		g := a.group
		if q.Operation == "sum" {
			g.Value = a.sum.FloatString(a.scale)
			r.Groups = append(r.Groups, g)
			continue
		}
		candidates := a.facts
		if q.Operation == "latest" {
			candidates = nil
			for _, f := range a.facts {
				_, end, _, ok := DateInterval(f.Date, q.Timezone)
				if !ok {
					reason("unknown_date")
					continue
				}
				latest := true
				for _, other := range a.facts {
					start, _, _, known := DateInterval(other.Date, q.Timezone)
					if known && start.After(end) {
						latest = false
						break
					}
				}
				if latest {
					candidates = append(candidates, f)
				}
			}
			if len(candidates) > 1 {
				reason("ambiguous_latest")
			}
		}
		for _, f := range candidates {
			g := a.group
			g.Evidence = append([]SearchEvidence{}, f.Evidence...)
			g.Value = f.Value
			g.Date = f.Date
			_, _, g.DatePrecision, _ = DateInterval(f.Date, q.Timezone)
			if q.Operation == "ratio" {
				den, _, ok := exactDecimal(f.Denominator)
				if !ok || f.DenominatorNonExact || den.Sign() <= 0 || len(f.DenominatorEvidence) == 0 {
					reason("missing_or_invalid_denominator")
					r.Coverage.Excluded++
					r.Coverage.Included--
					continue
				}
				num, _, _ := exactDecimal(f.Value)
				g.Numerator = f.Value
				g.Denominator = f.Denominator
				g.Value = new(big.Rat).Mul(new(big.Rat).Quo(num, den), big.NewRat(100, 1)).FloatString(2)
				g.Rounding = "half_away_from_zero/2"
				g.Unit = "percent"
				g.Evidence = append(g.Evidence, f.DenominatorEvidence...)
			}
			r.Groups = append(r.Groups, g)
		}
	}
	if len(r.Groups) > 256 {
		return r, fmt.Errorf("analysis result bound")
	}
	if q.Operation == "series" || q.Operation == "ratio" {
		sort.SliceStable(r.Groups, func(i, j int) bool { return r.Groups[i].Date < r.Groups[j].Date })
	}
	if q.Operation == "ratio" {
		originalGroups := append([]AnalysisGroup{}, r.Groups...)
		for i := range r.Groups {
			g := &r.Groups[i]
			start, _, _, known := DateInterval(g.Date, q.Timezone)
			if !known {
				continue
			}
			baseline := -1
			ambiguous := false
			for j := range r.Groups {
				if j == i {
					continue
				}
				p := &r.Groups[j]
				if p.Metric != g.Metric || p.Currency != g.Currency || p.Sample != g.Sample {
					continue
				}
				pstart, pend, _, ok := DateInterval(p.Date, q.Timezone)
				if !ok || !pend.Before(start) {
					continue
				}
				if baseline < 0 {
					baseline = j
					continue
				}
				bstart, bend, _, _ := DateInterval(r.Groups[baseline].Date, q.Timezone)
				if pstart.After(bend) {
					baseline = j
					ambiguous = false
				} else if !pend.Before(bstart) {
					ambiguous = true
				}
			}
			if baseline >= 0 && !ambiguous {
				p := originalGroups[baseline]
				num, _, _ := exactDecimal(g.Numerator)
				den, _, _ := exactDecimal(g.Denominator)
				pn, _, _ := exactDecimal(p.Numerator)
				pd, _, _ := exactDecimal(p.Denominator)
				delta := new(big.Rat).Sub(new(big.Rat).Quo(num, den), new(big.Rat).Quo(pn, pd))
				g.ChangePercentPoints = delta.Mul(delta, big.NewRat(100, 1)).FloatString(2)
				g.BaselineDate = p.Date
				g.Evidence = append(g.Evidence, p.Evidence...)
			}
		}
	}
	if len(r.Groups) > 256 {
		return r, fmt.Errorf("analysis result bound")
	}
	sort.Strings(r.Coverage.Reasons)
	return r, nil
}
