package archive

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func reviewDOCX(t *testing.T, body string) ParseResult {
	t.Helper()
	b := rewritePackage(t, fixture(t, "e01.docx"), func(p map[string][]byte) {
		p["content/main.xml"] = []byte(`<w:document xmlns:w="` + nsWord + `"><w:body>` + body + `</w:body></w:document>`)
	})
	return parseBytes(t, b, "docx", ParseLimits{})
}
func reviewParagraph(text string) string { return `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>` }
func reviewCell(props, text string) string {
	return `<w:tc><w:tcPr>` + props + `</w:tcPr>` + reviewParagraph(text) + `</w:tc>`
}

func TestDOCXPropertyRevisionsRetainCurrentEvidence(t *testing.T) {
	for _, c := range []struct {
		name, body string
		row, col   int
	}{
		{"deleted row", `<w:tbl><w:tr><w:trPr><w:del w:id="1"/></w:trPr>` + reviewCell("", "Deleted fee 999 CNY") + `</w:tr><w:tr>` + reviewCell("", "Current fee 200 CNY") + `</w:tr></w:tbl>`, 2, 1},
		{"deleted cell", `<w:tbl><w:tr>` + reviewCell(`<w:gridSpan w:val="2"/><w:cellDel w:id="1"/>`, "Deleted fee 999 CNY") + reviewCell("", "Current fee 200 CNY") + `</w:tr></w:tbl>`, 1, 3},
		{"paragraph property change", `<w:p><w:pPr><w:pPrChange w:id="1"><w:pPr/></w:pPrChange></w:pPr><w:r><w:t>Current fee 200 CNY</w:t></w:r></w:p>`, 0, 0},
		{"run property change", `<w:p><w:r><w:rPr><w:rPrChange w:id="1"><w:rPr/></w:rPrChange></w:rPr><w:t>Current fee 200 CNY</w:t></w:r></w:p>`, 0, 0},
		{"row property change", `<w:tbl><w:tr><w:trPr><w:trPrChange w:id="1"><w:trPr/></w:trPrChange></w:trPr>` + reviewCell("", "Current fee 200 CNY") + `</w:tr></w:tbl>`, 1, 1},
		{"cell property change", `<w:tbl><w:tr>` + reviewCell(`<w:tcPrChange w:id="1"><w:tcPr/></w:tcPrChange>`, "Current fee 200 CNY") + `</w:tr></w:tbl>`, 1, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := reviewDOCX(t, c.body)
			if r.Status != ParsePartial || r.Coverage.Complete || !warning(r, "docx_revisions") || len(r.Segments) != 1 {
				t.Fatalf("%+v", r)
			}
			s := r.Segments[0]
			if s.Text != "Current fee 200 CNY" || s.Locator.DOCX.Row != c.row || s.Locator.DOCX.GridColumn != c.col || s.Locator.DOCX.Paragraph != 1 {
				t.Fatalf("evidence %+v locator %+v", s, s.Locator.DOCX)
			}
		})
	}
}
func TestDOCXDeletedGridNestedTablesAndMerges(t *testing.T) {
	nested := `<w:tbl><w:tr>` + reviewCell(`<w:vMerge w:val="restart"/>`, "Nested current anchor") + `</w:tr><w:tr>` + reviewCell(`<w:vMerge/>`, "") + `</w:tr></w:tbl>`
	body := `<w:tbl><w:tr><w:trPr><w:del w:id="1"/></w:trPr><w:tc>` + reviewParagraph("Deleted row 999 CNY") + nested + `</w:tc></w:tr><w:tr>` +
		reviewCell(`<w:gridSpan w:val="2"/><w:cellDel w:id="2"/><w:vMerge w:val="restart"/>`, "Deleted cell 999 CNY") +
		`<w:tc><w:tcPr><w:gridSpan w:val="2"/><w:vMerge w:val="restart"/></w:tcPr>` + reviewParagraph("Current fee 200 CNY") + nested + `</w:tc></w:tr><w:tr>` +
		reviewCell(`<w:gridSpan w:val="2"/>`, "Current left") + reviewCell(`<w:gridSpan w:val="2"/><w:vMerge/>`, "Current continuation") + `</w:tr></w:tbl>`
	r := reviewDOCX(t, body)
	if r.Status != ParsePartial || !warning(r, "docx_revisions") || !warning(r, "docx_revision_merge") || len(r.Segments) != 5 {
		t.Fatalf("%+v", r)
	}
	for _, s := range r.Segments {
		if strings.Contains(s.Text, "Deleted") {
			t.Fatalf("deleted content leaked %+v", s)
		}
	}
	s := r.Segments[0]
	if s.Text != "Current fee 200 CNY" || s.Locator.DOCX.Row != 2 || s.Locator.DOCX.GridColumn != 3 || (s.DOCXMerge == nil || !s.DOCXMerge.VerticalUnresolved || s.DOCXMerge.ColumnSpan != 2 || s.DOCXMerge.AnchorRow != 0 || s.DOCXMerge.AnchorColumn != 0 || s.DOCXMerge.RowSpan != 0) {
		t.Fatalf("physical/revised merge %+v locator %+v", s, s.Locator.DOCX)
	}
	nestedAnchor := r.Segments[1]
	if nestedAnchor.Text != "Nested current anchor" || !reflect.DeepEqual(nestedAnchor.Locator.DOCX.TablePath, []int{1, 2, 3, 1}) || nestedAnchor.DOCXMerge == nil || nestedAnchor.DOCXMerge.RowSpan != 2 || nestedAnchor.DOCXMerge.Continuation {
		t.Fatalf("nested anchor %+v locator %+v", nestedAnchor, nestedAnchor.Locator.DOCX)
	}
	if s := r.Segments[2]; s.Text != "" || s.DOCXMerge == nil || !s.DOCXMerge.Continuation || s.DOCXMerge.AnchorRow != 1 {
		t.Fatalf("nested continuation %+v", s)
	}
	if s := r.Segments[4]; s.Text != "Current continuation" || s.Locator.DOCX.Row != 3 || s.Locator.DOCX.GridColumn != 3 || (s.DOCXMerge == nil || !s.DOCXMerge.VerticalUnresolved || s.DOCXMerge.ColumnSpan != 2 || s.DOCXMerge.AnchorRow != 0 || s.DOCXMerge.RowSpan != 0) {
		t.Fatalf("outer continuation %+v", s)
	}
	// A current continuation cannot borrow a deleted anchor as a current fact.
	r = reviewDOCX(t, `<w:tbl><w:tr>`+reviewCell(`<w:cellDel w:id="1"/><w:vMerge w:val="restart"/>`, "Deleted anchor")+`</w:tr><w:tr>`+reviewCell(`<w:vMerge/>`, "Current continuation")+`</w:tr></w:tbl>`)
	if r.Status != ParsePartial || len(r.Segments) != 1 || r.Segments[0].Text != "Current continuation" || (r.Segments[0].DOCXMerge == nil || !r.Segments[0].DOCXMerge.VerticalUnresolved || r.Segments[0].DOCXMerge.ColumnSpan != 1 || r.Segments[0].DOCXMerge.AnchorRow != 0 || r.Segments[0].DOCXMerge.RowSpan != 0) || r.Segments[0].Locator.DOCX.Row != 2 {
		t.Fatalf("deleted anchor %+v", r)
	}
}
func TestOfficeUTF8BOMPartIdentityAndEvidence(t *testing.T) {
	for _, c := range []struct {
		fixture, hint, part string
		segments            int
	}{
		{"e01.docx", "docx", "content/main.xml", 6}, {"e01.docx", "docx", "[Content_Types].xml", 6}, {"e01.docx", "docx", "_rels/.rels", 6},
		{"e02.xlsx", "xlsx", "xl/book.xml", 5}, {"e02.xlsx", "xlsx", "xl/_rels/book.xml.rels", 5}, {"e02.xlsx", "xlsx", "sheets/data.xml", 5},
	} {
		t.Run(c.part, func(t *testing.T) {
			base := parseBytes(t, fixture(t, c.fixture), c.hint, ParseLimits{})
			b := rewritePackage(t, fixture(t, c.fixture), func(p map[string][]byte) { p[c.part] = append([]byte{239, 187, 191}, p[c.part]...) })
			r := parseBytes(t, b, c.hint, ParseLimits{})
			if r.Status != ParseParsed || len(r.Segments) != c.segments || r.Counts.ExpandedBytes != base.Counts.ExpandedBytes+3 || r.Counts.DecodedTextBytes != base.Counts.DecodedTextBytes {
				t.Fatalf("BOM result %+v baseline %+v", r, base.Counts)
			}
			if r.Segments[0].Text != base.Segments[0].Text || !reflect.DeepEqual(r.Segments[0].Locator, base.Segments[0].Locator) {
				t.Fatalf("BOM altered evidence %+v", r.Segments[0])
			}
		})
	}
	// Only the initial BOM is stripped; invalid bytes and repeated BOMs still reject.
	for _, prefix := range [][]byte{{239, 187, 191, 255}, {239, 187, 191, 239, 187, 191}} {
		b := rewritePackage(t, fixture(t, "e01.docx"), func(p map[string][]byte) { p["content/main.xml"] = append(prefix, p["content/main.xml"]...) })
		r := parseBytes(t, b, "docx", ParseLimits{})
		if r.Status != ParseInvalid {
			t.Fatalf("invalid prefix accepted %+v", r)
		}
	}
}
func TestPKProseAndCorruptZIPDispatch(t *testing.T) {
	for _, hint := range []string{"", "text", "text/plain", "csv", "text/csv"} {
		r := parseBytes(t, []byte("PK activity,12.5\n"), hint, ParseLimits{})
		if r.Status != ParseParsed {
			t.Fatalf("hint %q: %+v", hint, r)
		}
		if strings.Contains(hint, "csv") {
			if len(r.Segments) != 2 || r.Segments[0].Text != "PK activity" {
				t.Fatalf("CSV %+v", r)
			}
		} else if len(r.Segments) != 1 || r.Segments[0].Text != "PK activity,12.5\n" {
			t.Fatalf("text %+v", r)
		}
	}
	for _, b := range [][]byte{{'P', 'K', 3, 4}, {'P', 'K', 5, 6}, {'P', 'K', 7, 8}} {
		r := parseBytes(t, b, "text/plain", ParseLimits{})
		if r.Status != ParseInvalid || r.DetectedFormat != "zip" {
			t.Fatalf("corrupt signature %+v", r)
		}
	}
	for _, hint := range []string{"docx", "xlsx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"} {
		r := parseBytes(t, []byte("PK corrupt Office"), hint, ParseLimits{})
		if r.Status != ParseInvalid || len(r.Segments) != 0 {
			t.Fatalf("typed Office bypass %q %+v", hint, r)
		}
	}
}
func TestSpreadsheetDecimalDisplayRoundingPreservesRawAndCache(t *testing.T) {
	for _, c := range []struct {
		raw, format, want string
		style             int
	}{
		{"2.5", "0", "3", 1}, {"-2.5", "0", "-3", 1}, {"0.125", "0%", "13%", 9}, {"-0.125", "0%", "-13%", 9},
		{"1.005", "0.00", "1.01", 2}, {"-1.005", "0.00", "-1.01", 2}, {"2.675", "0.00", "2.68", 2},
		{"0.01005", "0.00%", "1.01%", 10}, {"-0.01005", "0.00%", "-1.01%", 10}, {"1.005e-2", "0.00%", "1.01%", 10},
		{"9.995", "0.00", "10.00", 2}, {"0.12499999999999999", "0%", "12%", 9}, {"2.49999999999999999", "0", "2", 1},
	} {
		t.Run(c.raw+"_"+c.format, func(t *testing.T) {
			b := rewritePackage(t, fixture(t, "e02.xlsx"), func(p map[string][]byte) {
				p["xl/styles.xml"] = []byte(`<styleSheet xmlns="` + nsSheet + `"><cellXfs><xf numFmtId="` + strconv.Itoa(c.style) + `"/></cellXfs></styleSheet>`)
				p["sheets/data.xml"] = []byte(`<worksheet xmlns="` + nsSheet + `"><sheetData><row r="1"><c r="A1"><f>2.5</f><v>` + c.raw + `</v></c></row></sheetData></worksheet>`)
			})
			r := parseBytes(t, b, "xlsx", ParseLimits{})
			s := cellAt(t, r, "A1")
			if r.Status != ParseParsed || s.Cell.DisplayValue == nil || *s.Cell.DisplayValue != c.want || s.Text != c.want || s.Cell.RawValue != c.raw || s.Cell.Formula.Text != "2.5" || !s.Cell.Formula.CachePresent || s.Cell.Formula.CachedRaw != c.raw {
				t.Fatalf("result status %s cell %+v formula %+v", r.Status, s.Cell, s.Cell.Formula)
			}
		})
	}
}
