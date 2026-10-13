package archive

import (
	"bytes"
	"reflect"
	"testing"
)

func qualityWorksheet(t *testing.T, content string, edit func(map[string][]byte)) []byte {
	t.Helper()
	return rewritePackage(t, fixture(t, "e02.xlsx"), func(parts map[string][]byte) {
		parts["sheets/data.xml"] = []byte(`<worksheet xmlns="` + nsSheet + `">` + content + `</worksheet>`)
		if edit != nil {
			edit(parts)
		}
	})
}
func TestXLSXDateChecksExactRawDecimal(t *testing.T) {
	for _, raw := range []string{"60.000000000000001", "61.000000000000001", "-1e-999", "1e-999", "2957003.00000000001", "2957004", "-0.00000000000000001", "1e-1000000", "1e999", "-1e999", "1e99999999999999999999999"} {
		t.Run(raw, func(t *testing.T) {
			b := qualityWorksheet(t, `<sheetData><row r="1"><c r="A1" s="4"><f>HISTORICAL()</f><v>`+raw+`</v></c></row></sheetData>`, nil)
			r := parseBytes(t, b, "xlsx", ParseLimits{})
			if r.Status != ParsePartial || r.Coverage.Complete || !warning(r, "unknown_number_format") || warning(r, "excel_phantom_date") {
				t.Fatalf("fractional/range status %+v", r)
			}
			s := cellAt(t, r, "A1")
			if s.Text != raw || s.Cell.RawValue != raw || s.Cell.DisplayValue != nil || s.Cell.Formula == nil || s.Cell.Formula.Text != "HISTORICAL()" || !s.Cell.Formula.CachePresent || s.Cell.Formula.CachedRaw != raw {
				t.Fatalf("invented date or changed cache: %+v", s)
			}
		})
	}
}
func TestXLSXDateAcceptsExactIntegerSpellings(t *testing.T) {
	for _, raw := range []string{"61", "61.0000", "610e-1", ".61e2", "+0061.000", "6.1000e1"} {
		for _, date1904 := range []bool{false, true} {
			t.Run(raw+map[bool]string{false: "/1900", true: "/1904"}[date1904], func(t *testing.T) {
				b := qualityWorksheet(t, `<sheetData><row r="1"><c r="A1" s="4"><v>`+raw+`</v></c></row></sheetData>`, func(p map[string][]byte) {
					if date1904 {
						p["xl/book.xml"] = bytes.Replace(p["xl/book.xml"], []byte(`date1904="0"`), []byte(`date1904="1"`), 1)
					}
				})
				r := parseBytes(t, b, "xlsx", ParseLimits{})
				want := "1900-03-01"
				if date1904 {
					want = "1904-03-02"
				}
				s := cellAt(t, r, "A1")
				if r.Status != ParseParsed || s.Text != want || s.Cell.RawValue != raw || s.Cell.DisplayValue == nil || *s.Cell.DisplayValue != want || warning(r, "excel_phantom_date") {
					t.Fatalf("integer spelling %+v", r)
				}
			})
		}
	}
	for _, c := range []struct {
		raw, want         string
		date1904, phantom bool
	}{{"60.00000", "1900-02-29", false, true}, {"6e1", "1904-03-01", true, false}, {"000e-999", "1899-12-31", false, false}, {"2957003.000", "9999-12-31", true, false}} {
		got, ok, phantom := formatNumber(0, c.raw, "yyyy-mm-dd", c.date1904)
		if !ok || got != c.want || phantom != c.phantom {
			t.Fatalf("exact date %+v => %q %v %v", c, got, ok, phantom)
		}
	}
	for _, raw := range []string{"1e-99999999999999999999999", "-1e-99999999999999999999999", "1e99999999999999999999999"} {
		if got, ok, _ := formatNumber(0, raw, "yyyy-mm-dd", false); ok {
			t.Fatalf("unbounded exponent accepted %q -> %s", raw, got)
		}
	}
}
func TestXLSXAllColumnGroupsRetainDocumentOrder(t *testing.T) {
	for _, c := range []struct {
		name, cols string
		want       []ColumnEvidence
		hidden     bool
		partial    bool
	}{
		{"multiple disjoint", `<cols><col min="2" max="2" hidden="0"/></cols><cols><col min="3" max="3" hidden="1"/></cols>`, []ColumnEvidence{{2, 2, false}, {3, 3, true}}, true, false},
		{"conflicting overlap", `<cols><col min="2" max="4" hidden="1"/></cols><cols><col min="3" max="3" hidden="0"/></cols>`, []ColumnEvidence{{2, 4, true}, {3, 3, false}}, false, true},
		{"reverse conflicting overlap", `<cols><col min="3" max="3" hidden="0"/></cols><cols><col min="2" max="4" hidden="1"/></cols>`, []ColumnEvidence{{3, 3, false}, {2, 4, true}}, false, true},
		{"agreeing overlap", `<cols><col min="2" max="4" hidden="1"/></cols><cols><col min="3" max="3" hidden="1"/></cols>`, []ColumnEvidence{{2, 4, true}, {3, 3, true}}, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := parseBytes(t, qualityWorksheet(t, c.cols+`<sheetData><row r="3"><c r="C3"><v>132</v></c></row></sheetData>`, nil), "xlsx", ParseLimits{})
			wantStatus := ParseParsed
			if c.partial {
				wantStatus = ParsePartial
			}
			if r.Status != wantStatus || r.Coverage.Complete == c.partial || warning(r, "conflicting_column_visibility") != c.partial || len(r.Sheets) != 1 || !reflect.DeepEqual(r.Sheets[0].Columns, c.want) {
				t.Fatalf("columns %+v", r)
			}
			s := cellAt(t, r, "C3")
			if s.Cell.ColumnHidden != c.hidden || s.Cell.ColumnHiddenUnresolved != c.partial || s.Cell.RawValue != "132" || s.Locator.XLSX.Cell != "C3" {
				t.Fatalf("column evidence %+v", s)
			}
		})
	}
	emptyConflict := qualityWorksheet(t, `<cols><col min="1" max="16384" hidden="1"/></cols><cols><col min="16384" max="16384" hidden="0"/></cols><sheetData/>`, nil)
	conflict := parseBytes(t, emptyConflict, "xlsx", ParseLimits{})
	if conflict.Status != ParsePartial || !warning(conflict, "conflicting_column_visibility") {
		t.Fatalf("empty conflicting metadata %+v", conflict)
	}
	b := qualityWorksheet(t, `<cols><col min="2" max="2"/></cols><cols><col min="3" max="16385"/></cols><sheetData/>`, nil)
	r := parseBytes(t, b, "xlsx", ParseLimits{})
	if r.Status != ParseInvalid || len(r.Segments) != 0 {
		t.Fatalf("second group escaped bounds %+v", r)
	}
}
func TestXLSXRejectsDuplicateSingletonContainersBeforeEvidence(t *testing.T) {
	groups := []struct {
		part, close string
		names       []string
	}{
		{"xl/book.xml", "</workbook>", []string{"workbookPr", "sheets", "bookViews", "calcPr", "fileVersion", "workbookProtection", "definedNames"}},
		{"xl/styles.xml", "</styleSheet>", []string{"numFmts", "cellXfs", "cellStyleXfs"}},
		{"sheets/data.xml", "</worksheet>", []string{"sheetData", "mergeCells", "dimension", "sheetViews", "sheetFormatPr", "pageMargins", "pageSetup", "printOptions", "sheetPr", "sheetProtection", "autoFilter"}},
	}
	for _, group := range groups {
		for _, name := range group.names {
			t.Run(group.part+"/"+name, func(t *testing.T) {
				b := rewritePackage(t, fixture(t, "e02.xlsx"), func(p map[string][]byte) { // Always append two, including when original has none.
					extra := []byte("<" + name + "/><" + name + "/>" + group.close)
					p[group.part] = bytes.Replace(p[group.part], []byte(group.close), extra, 1)
				})
				r := parseBytes(t, b, "xlsx", ParseLimits{})
				if r.Status != ParseInvalid || r.Coverage.Complete || len(r.Segments) != 0 || len(r.Sheets) != 0 {
					t.Fatalf("ambiguous singleton yielded evidence: %+v", r)
				}
			})
		}
	}
}
func TestXLSXRejectsInlineContentForOtherCellTypes(t *testing.T) {
	for _, typ := range []string{"", "n", "s", "str", "b", "e", "d", "unknown"} {
		for _, formula := range []string{"", `<f>2+2</f>`} {
			t.Run(typ+formula, func(t *testing.T) {
				attr := ""
				if typ != "" {
					attr = ` t="` + typ + `"`
				}
				b := qualityWorksheet(t, `<sheetData><row r="1"><c r="A1"`+attr+`>`+formula+`<v>0</v><is><t>Unexpected second value</t></is></c></row></sheetData>`, nil)
				r := parseBytes(t, b, "xlsx", ParseLimits{})
				if r.Status != ParseInvalid || r.Coverage.Complete || len(r.Segments) != 0 {
					t.Fatalf("incompatible cell emitted %+v", r)
				}
			})
		}
	}
	for _, value := range []string{`<is><t>only inline text</t></is>`, `<v>4</v><is><t>text</t></is>`, `<f>2+2</f><is><t>text</t></is>`} {
		b := qualityWorksheet(t, `<sheetData><row r="1"><c r="A1" t="inlineStr">`+value+`</c></row></sheetData>`, nil)
		r := parseBytes(t, b, "xlsx", ParseLimits{})
		if value == `<is><t>only inline text</t></is>` {
			if r.Status != ParseParsed || cellAt(t, r, "A1").Text != "only inline text" {
				t.Fatalf("valid inline rejected %+v", r)
			}
		} else if r.Status != ParseInvalid {
			t.Fatalf("inline conflict accepted %+v", r)
		}
	}
}
