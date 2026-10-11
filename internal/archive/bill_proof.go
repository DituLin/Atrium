package archive

import (
	"strconv"
	"strings"
)

func billLabelKind(label string) string {
	switch strings.ToLower(strings.TrimSpace(strings.TrimRight(label, ":："))) {
	case "账单", "账单编号", "账单号", "付款所引用账单编号", "引用账单编号", "bill id", "bill number", "referenced bill number":
		return "bill_id"
	case "收款方", "收款人", "开票方", "issuer", "payee":
		return "issuer"
	case "日期", "账单日期", "付款日期", "date", "bill date", "payment date":
		return "bill_date"
	}
	return ""
}

// billIdentity is an economic obligation reference, not a payment transaction
// identifier. Both the bill-number role and counterparty role must be anchored
// in literal source labels. A date is required as an additional conflict fence.
func billIdentity(fields []PublicationField) *TransactionProof {
	p := &TransactionProof{Type: "bill"}
	seen := map[string]bool{}
	for _, f := range fields {
		name := f.Name
		if name == "日期" || name == "date" || name == "账单日期" || name == "付款日期" {
			name = "bill_date"
		}
		if name != "bill_id" && name != "issuer" && name != "bill_date" {
			continue
		}
		labelled, raw := false, false
		for _, e := range f.Evidence {
			labelled = labelled || billLabelKind(e.Quote) == name
			raw = raw || e.Quote == f.Raw
		}
		if !labelled || !raw || f.Raw == "" || f.RequiresReview || seen[name] {
			return nil
		}
		seen[name] = true
		switch name {
		case "bill_id":
			p.Identifier = f.Raw
		case "issuer":
			p.Issuer = f.Raw
		case "bill_date":
			if !validDate(f.Raw) {
				return nil
			}
			p.Date = f.Raw
		}
		p.Evidence = append(p.Evidence, f.Evidence...)
	}
	if p.Identifier == "" || p.Issuer == "" || p.Date == "" {
		return nil
	}
	return p
}

// sourceBillLabels resolves exact text prefixes and first-row table headers
// independently of model spelling. It adds provenance, never a new raw fact or
// guessed issuer. Value slots retain their original physical anchors.
func sourceBillLabels(parsed ParseResult, f *PublicationField) {
	name := f.Name
	if name == "日期" || name == "date" || name == "账单日期" || name == "付款日期" {
		name = "bill_date"
	}
	if name != "bill_id" && name != "issuer" && name != "bill_date" {
		name = inferredBillRole(parsed, *f)
		if name == "" {
			return
		}
		f.Name = name
	}
	original := append([]CanonicalEvidence(nil), f.Evidence...)
	verifiedLabels := map[CanonicalEvidence]bool{}
	add := func(s Segment, start, end int) {
		for _, v := range componentViews(s, original[0].ParseDigest, parsed.ParserVersion) {
			runes := []rune(v.Text)
			if v.RequiresReview || v.Start != 0 || start < 0 || end > len(runes) {
				continue
			}
			quote := string(runes[start:end])
			if billLabelKind(quote) != name {
				continue
			}
			ref := canonicalEvidence(v, EvidenceReference{Start: start, End: end, Quote: quote})
			verifiedLabels[ref] = true
			for _, old := range f.Evidence {
				if old == ref {
					return
				}
			}
			f.Evidence = append(f.Evidence, ref)
			anchors, e := physicalEvidence(parsed, []CanonicalEvidence{ref})
			if e == nil {
				f.VisibilityAnchors = append(f.VisibilityAnchors, anchors...)
			}
			return
		}
	}
	for _, ev := range original {
		if ev.Quote != f.Raw {
			continue
		}
		for _, value := range parsed.Segments {
			if value.ID != ev.ParentSegmentID {
				continue
			}
			if value.Locator.CSV != nil || value.Locator.XLSX != nil {
				for _, header := range parsed.Segments {
					if sameHeaderColumn(value.Locator, header.Locator) && continuousHeaderColumn(parsed, value, header) {
						add(header, 0, len([]rune(header.Text)))
					}
				}
			} else {
				runes := []rune(value.Text)
				if ev.Start < 0 || ev.Start > len(runes) {
					continue
				}
				start := ev.Start
				for start > 0 && runes[start-1] != '\n' && runes[start-1] != '\r' {
					start--
				}
				prefix := string(runes[start:ev.Start])
				trim := strings.TrimSpace(prefix)
				trim = strings.TrimRight(trim, ":：")
				trim = strings.TrimSpace(trim)
				// Require an adjacent explicit delimiter, rather than an arbitrary name
				// occurring somewhere earlier on a line.
				if !strings.ContainsAny(prefix, ":：") {
					continue
				}
				offset := strings.Index(prefix, trim)
				if offset < 0 {
					continue
				}
				start += len([]rune(prefix[:offset]))
				add(value, start, start+len([]rune(trim)))
			}
		}
	}
	// Model-supplied labels require the same physical relationship as labels
	// discovered here. Keep the original evidence, but never authorize a
	// transaction merge using an unrelated header elsewhere in the document.
	for _, ev := range original {
		if billLabelKind(ev.Quote) == name && !verifiedLabels[ev] {
			f.RequiresReview = true
		}
	}
}
func sameHeaderColumn(value, header Locator) bool {
	if value.CSV != nil && header.CSV != nil {
		return value.CSV.Record > 1 && header.CSV.Record == 1 && value.CSV.Column == header.CSV.Column
	}
	if value.XLSX != nil && header.XLSX != nil && value.XLSX.PartURI == header.XLSX.PartURI {
		split := func(cell string) (string, string) {
			i := 0
			for i < len(cell) && cell[i] >= 'A' && cell[i] <= 'Z' {
				i++
			}
			return cell[:i], cell[i:]
		}
		vc, vr := split(value.XLSX.Cell)
		hc, hr := split(header.XLSX.Cell)
		return vc != "" && vc == hc && hr == "1" && vr != "1"
	}
	return false
}

// inferredBillRole is limited to a model-extracted literal cell and an explicit
// header in a continuous, visible, unmerged column. A gap or another header
// terminates that region; company names alone never establish counterparty role.
func inferredBillRole(parsed ParseResult, f PublicationField) string {
	if len(f.Normalizations) > 0 || len(f.Dimensions) > 0 || f.Raw == "" || f.RequiresReview {
		return ""
	}
	role := ""
	positions := map[string]bool{}
	for _, ev := range f.Evidence {
		if ev.Quote == f.Raw {
			positions[ev.ParentSegmentID+"/"+ev.ComponentID+"/"+strconv.Itoa(ev.Start)] = true
		}
	}
	if len(positions) != 1 {
		return ""
	}
	for _, ev := range f.Evidence {
		if ev.Quote != f.Raw {
			continue
		}
		for _, value := range parsed.Segments {
			if value.ID != ev.ParentSegmentID || value.Text != f.Raw || ev.Start != 0 || ev.End != len([]rune(f.Raw)) {
				continue
			}
			for _, header := range parsed.Segments {
				if !sameHeaderColumn(value.Locator, header.Locator) || !continuousHeaderColumn(parsed, value, header) {
					continue
				}
				kind := billLabelKind(header.Text)
				if kind != "issuer" {
					continue
				}
				if role != "" && role != kind {
					return ""
				}
				role = kind
			}
		}
	}
	return role
}
func continuousHeaderColumn(parsed ParseResult, value, header Segment) bool {
	safe := func(s Segment) bool {
		return s.Visual == nil && (s.Cell == nil || (!s.Cell.RowHidden && !s.Cell.ColumnHidden && !s.Cell.ColumnHiddenUnresolved && s.Cell.MergeRange == "" && s.Cell.Formula == nil))
	}
	if !safe(value) || !safe(header) {
		return false
	}
	row := func(s Segment) int {
		if s.Locator.CSV != nil {
			return s.Locator.CSV.Record
		}
		if s.Locator.XLSX != nil {
			cell := s.Locator.XLSX.Cell
			i := 0
			for i < len(cell) && cell[i] >= 'A' && cell[i] <= 'Z' {
				i++
			}
			n, _ := strconv.Atoi(cell[i:])
			return n
		}
		return 0
	}
	end := row(value)
	if end < 2 || end > 10000 {
		return false
	}
	seen := map[int]bool{}
	for _, s := range parsed.Segments {
		n := row(s)
		if n < 2 || n > end || !sameHeaderColumn(s.Locator, header.Locator) {
			continue
		}
		if !safe(s) || s.Text == "" || billLabelKind(s.Text) != "" || seen[n] {
			return false
		}
		seen[n] = true
	}
	return len(seen) == end-1
}
