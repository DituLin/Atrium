package store

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DituLin/Atrium/internal/archive"
)

// This is a reference recognizer, not a spreadsheet evaluator. Only a literal,
// bounded SUM of same-sheet cells/ranges establishes summary membership.
var analysisSumFormula = regexp.MustCompile(`(?i)^=?SUM\(([A-Z$0-9:, ]+)\)$`)
var analysisFormulaCell = regexp.MustCompile(`^\$?([A-Z]{1,3})\$?([1-9][0-9]{0,6})$`)

func analysisFormulaCells(formula string) ([]string, bool) {
	if len(formula) > 2048 {
		return nil, false
	}
	match := analysisSumFormula.FindStringSubmatch(strings.TrimSpace(formula))
	if match == nil {
		return nil, false
	}
	result := []string{}
	seen := map[string]bool{}
	cell := func(raw string) (int, int, bool) {
		m := analysisFormulaCell.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil {
			return 0, 0, false
		}
		col := 0
		for _, c := range m[1] {
			col = col*26 + int(c-'A'+1)
		}
		row, _ := strconv.Atoi(m[2])
		return col, row, col <= 16384 && row <= 1048576
	}
	column := func(col int) string {
		s := ""
		for col > 0 {
			col--
			s = string(rune('A'+col%26)) + s
			col /= 26
		}
		return s
	}
	for _, arg := range strings.Split(strings.ToUpper(match[1]), ",") {
		ends := strings.Split(arg, ":")
		if len(ends) > 2 {
			return nil, false
		}
		c1, r1, ok := cell(ends[0])
		if !ok {
			return nil, false
		}
		c2, r2 := c1, r1
		if len(ends) == 2 {
			c2, r2, ok = cell(ends[1])
			if !ok {
				return nil, false
			}
		}
		if c2 < c1 || r2 < r1 || (c2-c1+1)*(r2-r1+1) > 256 {
			return nil, false
		}
		for r := r1; r <= r2; r++ {
			for c := c1; c <= c2; c++ {
				ref := column(c) + strconv.Itoa(r)
				if seen[ref] || len(result) >= 256 {
					return nil, false
				}
				seen[ref] = true
				result = append(result, ref)
			}
		}
	}
	return result, len(result) > 0
}

func (r *ArchiveTasks) analysisFormulaAggregatesIn(ctx context.Context, tx *sql.Tx, h, doc string, public bool, facts []archive.AnalysisFact) error {
	byCell := map[string][]int{}
	for i, f := range facts {
		if len(f.Evidence) > 0 {
			ev := f.Evidence[0]
			if loc := ev.Locator.XLSX; loc != nil {
				key := ev.SourceExtractionID + "\x00" + loc.PartURI + "\x00" + loc.Cell
				byCell[key] = append(byCell[key], i)
			}
		}
	}
	privateByDocument := map[string][]archive.PhysicalAnchor{}
	for i := range facts {
		total := &facts[i]
		if len(total.Evidence) == 0 {
			continue
		}
		source := total.Evidence[0]
		if source.ComponentKind != "historical_cache" || source.Locator.XLSX == nil {
			continue
		}
		private := privateByDocument[source.DocumentID]
		if public {
			if _, loaded := privateByDocument[source.DocumentID]; !loaded {
				var err error
				private, err = archivePrivateAnchorsIn(ctx, tx, h, source.DocumentID)
				if err != nil {
					return err
				}
				privateByDocument[source.DocumentID] = private
			}
		}
		total.HistoricalCache = true
		// An unsupported formula must not inherit the weaker adjacency proof.
		total.AggregateMembers = nil
		total.AggregateEvidence = nil
		total.FormulaAggregate = false
		bundle, err := searchBundleIn(ctx, tx, h, source.SourceExtractionID)
		if err != nil {
			return err
		}
		for _, seg := range bundle.Parsed.Segments {
			if seg.ID != source.SegmentID || seg.Locator.XLSX == nil || seg.Cell == nil || seg.Cell.Formula == nil {
				continue
			}
			formula := seg.Cell.Formula
			if len(formula.Attributes) > 0 {
				continue
			} // shared/array/external semantics are not interpreted.
			cells, ok := analysisFormulaCells(formula.Text)
			if !ok {
				continue
			}
			anchor := archive.PhysicalAnchor{Locator: seg.Locator, Component: "formula_text", Start: 0, End: utf8.RuneCountInString(formula.Text)}
			hidden := false
			for _, p := range private {
				if !archiveAnchorsDisjoint(p, anchor) {
					hidden = true
				}
			}
			if hidden {
				continue
			}
			formulaEvidence := source
			formulaEvidence.ComponentKind = "formula_text"
			formulaEvidence.ComponentID = seg.ID + "/formula_text"
			formulaEvidence.SourceKind = "analysis_formula_membership"
			formulaEvidence.Start = 0
			formulaEvidence.End = anchor.End
			formulaEvidence.Excerpt = formula.Text
			formulaEvidence.AssertedValue = ""
			formulaEvidence.AssertionID = ""
			formulaEvidence.SourceNormalizations = nil
			members := []string{}
			proof := []archive.SearchEvidence{source, formulaEvidence}
			complete := true
			for _, cell := range cells {
				if cell == seg.Locator.XLSX.Cell {
					complete = false
					break
				}
				matches := byCell[source.SourceExtractionID+"\x00"+seg.Locator.XLSX.PartURI+"\x00"+cell]
				if len(matches) != 1 {
					complete = false
					break
				}
				found := matches[0]
				f := facts[found]
				if f.Evidence[0].ComponentKind != "numeric_raw" || f.Unavailable || f.NonExact || f.HistoricalCache || f.Role != "line_item" || len(f.RoleEvidence) == 0 || f.Metric != total.Metric || f.Unit != total.Unit || f.Currency != total.Currency || f.Sample != total.Sample {
					complete = false
					break
				}
				members = append(members, f.ObservationID)
				proof = append(proof, f.Evidence...)
				proof = append(proof, f.BoundaryEvidence...)
				proof = append(proof, f.RoleEvidence...)
			}
			if complete {
				total.AggregateMembers = members
				total.AggregateEvidence = proof
				total.FormulaAggregate = true
			}
		}
	}
	return nil
}
