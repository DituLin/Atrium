package archive

import "strings"

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
		return
	}
	original := append([]CanonicalEvidence(nil), f.Evidence...)
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
					if sameHeaderColumn(value.Locator, header.Locator) {
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
