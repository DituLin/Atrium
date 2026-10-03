package archive

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

func cellAt(t *testing.T, r ParseResult, a1 string) Segment {
	t.Helper()
	for _, s := range r.Segments {
		if s.Locator.XLSX != nil && s.Locator.XLSX.Cell == a1 {
			return s
		}
	}
	t.Fatalf("missing %s in %+v", a1, r)
	return Segment{}
}
func TestDOCXOrderedParagraphAndTableEvidence(t *testing.T) {
	r := parseBytes(t, fixture(t, "e01.docx"), "text/plain", ParseLimits{})
	if r.Status != ParseParsed || r.DetectedFormat != "docx" || len(r.Segments) != 6 {
		t.Fatalf("%+v", r)
	}
	want := []string{"学生李妍 出生2012-11-06 2026春季", "数学", "88/100", "语文", "91/100", "示例学校"}
	for i, s := range r.Segments {
		if s.Text != want[i] {
			t.Errorf("%d: %q", i, s.Text)
		}
	}
	if l := r.Segments[4].Locator.DOCX; l == nil || l.PartURI != "/content/main.xml" || !reflect.DeepEqual(l.TablePath, []int{1}) || l.Row != 2 || l.GridColumn != 2 || l.Paragraph != 1 {
		t.Fatalf("locator %+v", l)
	}
	if l := r.Segments[5].Locator.DOCX; l == nil || l.Paragraph != 2 || len(l.TablePath) != 0 {
		t.Fatalf("locator %+v", l)
	}
	if r.Counts.PartsRead != r.Counts.ZIPEntries || r.Counts.ExpandedBytes == 0 || !r.Coverage.Complete {
		t.Fatalf("coverage %+v", r)
	}
}
func TestDOCXMergesNestedTablesRunsAndSkippedContent(t *testing.T) {
	r := parseBytes(t, fixture(t, "merges.docx"), "docx", ParseLimits{})
	if r.Status != ParseParsed || len(r.Segments) != 8 {
		t.Fatalf("%+v", r)
	}
	anchor := r.Segments[1]
	if anchor.Text != "合并标题" || anchor.DOCXMerge == nil || *anchor.DOCXMerge != (DOCXMerge{1, 1, 2, 2, false}) {
		t.Fatalf("anchor %+v", anchor)
	}
	cont := r.Segments[3]
	if cont.Text != "" || cont.DOCXMerge == nil || *cont.DOCXMerge != (DOCXMerge{1, 1, 2, 2, true}) {
		t.Fatalf("continuation %+v", cont)
	}
	nested := r.Segments[5]
	if nested.Text != "嵌套" || !reflect.DeepEqual(nested.Locator.DOCX.TablePath, []int{1, 2, 3, 1}) {
		t.Fatalf("nested %+v", nested)
	}
	if r.Segments[7].Text != "A\tB\nC" {
		t.Fatalf("runs %+v", r.Segments[7])
	}
	r = parseBytes(t, fixture(t, "partial.docx"), "docx", ParseLimits{})
	if r.Status != ParsePartial || r.Coverage.Complete || !warning(r, "external_relationship") || !warning(r, "docx_revisions") || !warning(r, "docx_headers") || !warning(r, "docx_textboxes") || !warning(r, "docx_images") {
		t.Fatalf("%+v", r)
	}
	for _, s := range r.Segments {
		if strings.Contains(s.Text, "已删除") || strings.Contains(s.Text, "文本框") || strings.Contains(s.Text, "页眉") {
			t.Fatalf("skipped text leaked: %+v", s)
		}
	}
}
func TestXLSXSparseCoordinatesMergesAndRichStrings(t *testing.T) {
	r := parseBytes(t, fixture(t, "e02.xlsx"), "application/octet-stream", ParseLimits{})
	if r.Status != ParseParsed || r.DetectedFormat != "xlsx" || len(r.Segments) != 5 {
		t.Fatalf("%+v", r)
	}
	a := cellAt(t, r, "A1")
	if a.Text != "学生李妍" || a.Cell.RawValue != "0" || a.Cell.Type != "s" || a.Cell.MergeAnchor != "A1" || a.Cell.MergeRange != "A1:B1" {
		t.Fatalf("%+v", a)
	}
	if a.Locator.XLSX.SheetID != "7" || a.Locator.XLSX.SheetName != "成绩" || a.Locator.XLSX.PartURI != "/sheets/data.xml" {
		t.Fatalf("%+v", a.Locator)
	}
	if c := cellAt(t, r, "C3"); c.Text != "132" || c.Cell.RawValue != "132" {
		t.Fatalf("%+v", c)
	}
	if len(r.Sheets) != 1 || r.Sheets[0].State != "hidden" || !reflect.DeepEqual(r.Sheets[0].Merges, []string{"A1:B1"}) {
		t.Fatalf("sheets %+v", r.Sheets)
	}
}
func TestXLSXPreservesMissingBlankEmptyZeroFormulaAndHidden(t *testing.T) {
	r := parseBytes(t, fixture(t, "f01.xlsx"), "xlsx", ParseLimits{})
	if r.Status != ParsePartial || !warning(r, "unknown_number_format") {
		t.Fatalf("%+v", r)
	}
	for _, c := range []struct{ ref, state, text string }{{"A10", "blank", ""}, {"B10", "empty_string", ""}, {"C10", "value", "0"}, {"E10", "formula_missing_cache", ""}, {"F10", "formula_cached", ""}} {
		s := cellAt(t, r, c.ref)
		if s.Cell.State != c.state || s.Text != c.text {
			t.Fatalf("%s: %+v", c.ref, s)
		}
	}
	for _, s := range r.Segments {
		if s.Locator.XLSX.Cell == "D10" {
			t.Fatal("synthesized missing cell")
		}
	}
	sum := cellAt(t, r, "B4")
	if sum.Cell.Formula == nil || sum.Cell.Formula.Text != "SUM(B2:B3)" || !sum.Cell.Formula.CachePresent || sum.Cell.Formula.CachedRaw != "200.00" || sum.Text != "200.00" {
		t.Fatalf("formula %+v", sum)
	}
	missing := cellAt(t, parseBytes(t, fixture(t, "f01-missing-cache.xlsx"), "xlsx", ParseLimits{}), "B4")
	if missing.Cell.Formula.CachePresent || missing.Text != "" {
		t.Fatalf("fabricated formula %+v", missing)
	}
	shared := cellAt(t, r, "F12")
	if shared.Cell.Formula.Text != "" || shared.Cell.Formula.Attributes["si"] != "7" || shared.Cell.Formula.Attributes["t"] != "shared" {
		t.Fatalf("shared %+v", shared)
	}
	c := cellAt(t, r, "B2")
	if !c.Cell.RowHidden || !c.Cell.ColumnHidden || c.Cell.StyleIndex != 1 || c.Cell.NumberFormatID != 2 || c.Cell.NumberFormatCode != "0.00" || c.Cell.DisplayValue == nil || *c.Cell.DisplayValue != "128.10" {
		t.Fatalf("style/hidden %+v", c)
	}
	if cellAt(t, r, "A20").Text != "TRUE" || cellAt(t, r, "B20").Text != "#DIV/0!" || cellAt(t, r, "C20").Text != "2026-10-03" {
		t.Fatal("typed cells not preserved")
	}
	if cellAt(t, r, "D20").Cell.DisplayValue != nil {
		t.Fatal("unknown format invented display")
	}
}
func TestXLSXDateSystems(t *testing.T) {
	for _, c := range []struct {
		name   string
		values []string
	}{{"dates-1900.xlsx", []string{"1899-12-31", "1900-02-28", "1900-02-29", "1900-03-01"}}, {"dates-1904.xlsx", []string{"1904-01-01", "1904-02-29", "1904-03-01", "1904-03-02"}}} {
		r := parseBytes(t, fixture(t, c.name), "xlsx", ParseLimits{})
		for i, a := range []string{"A1", "B1", "C1", "D1"} {
			s := cellAt(t, r, a)
			if s.Cell.DisplayValue == nil || *s.Cell.DisplayValue != c.values[i] {
				t.Fatalf("%s %s: %+v", c.name, a, s)
			}
		}
		if c.name == "dates-1900.xlsx" && !warning(r, "excel_phantom_date") {
			t.Fatal("phantom date not disclosed")
		}
	}
}

// Rewrite packages deterministically for one malicious condition at a time.
func rewritePackage(t *testing.T, original []byte, edit func(map[string][]byte)) []byte {
	t.Helper()
	r, e := zip.NewReader(bytes.NewReader(original), int64(len(original)))
	if e != nil {
		t.Fatal(e)
	}
	parts := map[string][]byte{}
	for _, f := range r.File {
		q, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(q)
		q.Close()
		if e != nil {
			t.Fatal(e)
		}
		parts[f.Name] = b
	}
	edit(parts)
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for name, b := range parts {
		q, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = q.Write(b); e != nil {
			t.Fatal(e)
		}
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}
func TestOfficeRejectsMalformedAndUnsupportedPackages(t *testing.T) {
	for _, c := range []struct {
		name   string
		status ParseStatus
		code   string
	}{{"wrong-content-type.zip", ParseUnsupported, "unsupported_main_content_type"}, {"escaping-relationship.zip", ParseInvalid, "relationship_escape"}, {"path-traversal.zip", ParseInvalid, "invalid_zip_path"}, {"dtd.zip", ParseInvalid, "xml_directive"}, {"compound.bin", ParseUnsupported, "unsupported_compound_container"}, {"corrupt.zip", ParseInvalid, "invalid_zip"}} {
		t.Run(c.name, func(t *testing.T) {
			r := parseBytes(t, fixture(t, "badpackages/"+c.name), "docx", ParseLimits{})
			if r.Status != c.status || !warning(r, c.code) {
				t.Fatalf("%+v", r)
			}
		})
	}
	doc := fixture(t, "e01.docx")
	xls := fixture(t, "e02.xlsx")
	cases := []struct {
		name string
		base []byte
		edit func(map[string][]byte)
		want ParseStatus
		code string
	}{
		{"no proof", doc, func(p map[string][]byte) { delete(p, "[Content_Types].xml") }, ParseInvalid, "missing_content_types"},
		{"bad shared index", xls, func(p map[string][]byte) {
			p["sheets/data.xml"] = bytes.Replace(p["sheets/data.xml"], []byte("<v>0</v>"), []byte("<v>9</v>"), 1)
		}, ParseInvalid, "invalid_shared_string"},
		{"strict", doc, func(p map[string][]byte) {
			p["content/main.xml"] = bytes.ReplaceAll(p["content/main.xml"], []byte("http://schemas.openxmlformats.org/wordprocessingml/2006/main"), []byte("http://purl.oclc.org/ooxml/wordprocessingml/main"))
		}, ParseUnsupported, "unsupported_xml_namespace"},
		{"macro", doc, func(p map[string][]byte) {
			p["[Content_Types].xml"] = bytes.ReplaceAll(p["[Content_Types].xml"], []byte("application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"), []byte("application/vnd.ms-word.document.macroEnabled.main+xml"))
		}, ParseUnsupported, "unsupported_main_content_type"},
		{"encryptedzip", doc, func(p map[string][]byte) {}, ParseEncrypted, "encrypted_zip"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := rewritePackage(t, c.base, c.edit)
			if c.name == "encryptedzip" {
				for i := 0; i+10 < len(b); i++ {
					if bytes.Equal(b[i:i+4], []byte{'P', 'K', 1, 2}) {
						b[i+8] |= 1
					}
				}
			}
			r := parseBytes(t, b, "xlsx", ParseLimits{})
			if r.Status != c.want || !warning(r, c.code) {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestOfficeResourceLimitsAndCRC(t *testing.T) {
	doc := fixture(t, "e01.docx")
	for _, l := range []ParseLimits{{ZIPEntries: 2}, {PartBytes: 20}, {ExpandedBytes: 100}, {XMLDepth: 3}, {XMLNodes: 4}, {Strings: 2}, {Cells: 2}, {Relationships: 0, Segments: 2}, {CompressionRatio: 1}} {
		r := parseBytes(t, doc, "docx", l)
		if r.Status != ParseLimitExceeded {
			t.Fatalf("limit %+v result %+v", l, r)
		}
	}
	xls := fixture(t, "e02.xlsx")
	r := parseBytes(t, xls, "xlsx", ParseLimits{Relationships: 1})
	if r.Status != ParseLimitExceeded {
		t.Fatalf("rels %+v", r)
	}
	// Duplicate names are never silently overwritten.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i := 0; i < 2; i++ {
		q, _ := w.Create("same.xml")
		q.Write([]byte("x"))
	}
	w.Close()
	r = parseBytes(t, buf.Bytes(), "xlsx", ParseLimits{})
	if r.Status != ParseInvalid || !warning(r, "duplicate_zip_part") {
		t.Fatalf("duplicate %+v", r)
	}
	// Corrupt CRC of a used part without breaking XML; must not yield full success.
	b := append([]byte(nil), doc...)
	for i := 0; i+20 < len(b); i++ {
		if bytes.Equal(b[i:i+4], []byte{'P', 'K', 1, 2}) {
			binary.LittleEndian.PutUint32(b[i+16:i+20], 0)
			break
		}
	}
	r = parseBytes(t, b, "docx", ParseLimits{})
	if r.Status != ParseInvalid || !warning(r, "corrupt_zip_part") {
		t.Fatalf("CRC %+v", r)
	}
}
func TestOfficeActualExpansionAndLocalHeaderValidation(t *testing.T) {
	// Lie in both directory and local size fields; expansion must still be counted.
	doc := fixture(t, "e01.docx")
	bomb := rewritePackage(t, doc, func(p map[string][]byte) { p["payload.bin"] = bytes.Repeat([]byte("z"), 10000) })
	for i := 0; i+46 < len(bomb); i++ {
		if bytes.Equal(bomb[i:i+4], []byte{'P', 'K', 1, 2}) {
			length := int(binary.LittleEndian.Uint16(bomb[i+28 : i+30]))
			if string(bomb[i+46:i+46+length]) == "payload.bin" {
				binary.LittleEndian.PutUint32(bomb[i+24:i+28], 0)
				local := int(binary.LittleEndian.Uint32(bomb[i+42 : i+46]))
				offset := local + 30 + int(binary.LittleEndian.Uint16(bomb[local+26:local+28])) + int(binary.LittleEndian.Uint16(bomb[local+28:local+30]))
				descriptor := offset + int(binary.LittleEndian.Uint32(bomb[i+20:i+24]))
				if binary.LittleEndian.Uint32(bomb[descriptor:descriptor+4]) == 0x08074b50 {
					descriptor += 4
				}
				binary.LittleEndian.PutUint32(bomb[descriptor+8:descriptor+12], 0)
			}
		}
	}
	r := parseBytes(t, bomb, "docx", ParseLimits{ExpandedBytes: 5000})
	if r.Status != ParseLimitExceeded || r.Counts.ExpandedBytes <= 5000 || r.Counts.ExpandedBytes > 5001 {
		t.Fatalf("actual expansion %+v", r)
	}
	// Local header encryption/method cannot disagree with the directory.
	for _, offset := range []int{6, 8} {
		b := append([]byte(nil), doc...)
		b[offset] ^= 1
		r := parseBytes(t, b, "docx", ParseLimits{})
		if r.Status != ParseInvalid && r.Status != ParseEncrypted {
			t.Fatalf("local header %d: %+v", offset, r)
		}
	}
}
func TestOfficeExtraAdversarialEvidence(t *testing.T) {
	doc := fixture(t, "e01.docx")
	xls := fixture(t, "e02.xlsx")
	cases := []struct {
		name string
		base []byte
		edit func(map[string][]byte)
	}{
		{"duplicate attribute", doc, func(p map[string][]byte) {
			p["content/main.xml"] = bytes.Replace(p["content/main.xml"], []byte("<w:t "), []byte("<w:t duplicate=\"a\" duplicate=\"b\" "), 1)
		}},
		{"duplicate values", xls, func(p map[string][]byte) {
			p["sheets/data.xml"] = bytes.Replace(p["sheets/data.xml"], []byte("<v>132</v>"), []byte("<v>132</v><v>99</v>"), 1)
		}},
		{"duplicate sheet data", xls, func(p map[string][]byte) {
			p["sheets/data.xml"] = bytes.Replace(p["sheets/data.xml"], []byte("</sheetData>"), []byte("</sheetData><sheetData/>"), 1)
		}},
		{"coordinate leading plus", xls, func(p map[string][]byte) {
			p["sheets/data.xml"] = bytes.Replace(p["sheets/data.xml"], []byte("C3"), []byte("C+3"), 1)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := parseBytes(t, rewritePackage(t, c.base, c.edit), "docx", ParseLimits{})
			if r.Status != ParseInvalid {
				t.Fatalf("%+v", r)
			}
		})
	}
	for _, name := range []string{"../evil.xml", "/absolute.xml", "a/../main.xml", "a\\main.xml", "%2e%2e/evil.xml"} {
		r := parseBytes(t, rewritePackage(t, doc, func(p map[string][]byte) { p[name] = []byte("x") }), "docx", ParseLimits{})
		if r.Status != ParseInvalid {
			t.Fatalf("path %q %+v", name, r)
		}
	}
	collision := rewritePackage(t, doc, func(p map[string][]byte) { p["content/%6dain.xml"] = p["content/main.xml"] })
	r := parseBytes(t, collision, "docx", ParseLimits{})
	if r.Status != ParseInvalid || !warning(r, "duplicate_zip_part") {
		t.Fatalf("normalized duplicate %+v", r)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "symlink"}
	h.SetMode(0o777 | 1<<27)
	q, _ := w.CreateHeader(h)
	q.Write([]byte("target"))
	w.Close()
	r = parseBytes(t, buf.Bytes(), "docx", ParseLimits{})
	if r.Status != ParseInvalid || !warning(r, "nonregular_zip_part") {
		t.Fatalf("symlink %+v", r)
	}
}
func TestOfficeEmptyAndProtectedDocumentStates(t *testing.T) {
	doc := rewritePackage(t, fixture(t, "e01.docx"), func(p map[string][]byte) {
		p["content/main.xml"] = []byte(`<w:document xmlns:w="` + nsWord + `"><w:body/></w:document>`)
	})
	r := parseBytes(t, doc, "docx", ParseLimits{})
	if r.Status != ParseEmpty {
		t.Fatalf("empty %+v", r)
	}
	xls := rewritePackage(t, fixture(t, "e02.xlsx"), func(p map[string][]byte) {
		p["sheets/data.xml"] = bytes.Replace(p["sheets/data.xml"], []byte("</worksheet>"), []byte(`<sheetProtection sheet="1" password="ABCD"/></worksheet>`), 1)
	})
	r = parseBytes(t, xls, "xlsx", ParseLimits{})
	if r.Status != ParseParsed || cellAt(t, r, "C3").Text != "132" {
		t.Fatalf("protection confused with encryption %+v", r)
	}
}
func TestOfficeSkippedPartsAndEncodingCoverage(t *testing.T) {
	doc := fixture(t, "e01.docx")
	r := parseBytes(t, rewritePackage(t, doc, func(p map[string][]byte) {
		p["content/footnotes.xml"] = []byte(`<w:footnotes xmlns:w="` + nsWord + `"><w:footnote><w:p><w:r><w:t>张明检验附注</w:t></w:r></w:p></w:footnote></w:footnotes>`)
	}), "docx", ParseLimits{})
	if r.Status != ParsePartial || !warning(r, "docx_notes") || len(r.Coverage.SkippedParts) != 1 {
		t.Errorf("skipped notes %+v", r)
	}
	nested := fixture(t, "f01.xlsx")
	r = parseBytes(t, rewritePackage(t, doc, func(p map[string][]byte) { p["nested.zip"] = nested }), "docx", ParseLimits{})
	if r.Status != ParsePartial || !warning(r, "unparsed_package_part") || r.Counts.ZIPEntries != 4 || r.Counts.PartsRead != 4 {
		t.Errorf("nested archive %+v", r)
	}
	encoded := rewritePackage(t, doc, func(p map[string][]byte) {
		var b bytes.Buffer
		b.Write([]byte{255, 254})
		for _, u := range utf16.Encode([]rune(string(p["content/main.xml"]))) {
			binary.Write(&b, binary.LittleEndian, u)
		}
		p["content/main.xml"] = b.Bytes()
	})
	r = parseBytes(t, encoded, "docx", ParseLimits{})
	if r.Status != ParseParsed || r.Segments[0].Text != "学生李妍 出生2012-11-06 2026春季" {
		t.Errorf("UTF16 XML %+v", r)
	}
	duplicate := rewritePackage(t, doc, func(p map[string][]byte) {
		p["content/main.xml"] = bytes.Replace(p["content/main.xml"], []byte("</w:body>"), []byte("</w:body><w:body/>"), 1)
	})
	r = parseBytes(t, duplicate, "docx", ParseLimits{})
	if r.Status != ParseInvalid {
		t.Fatalf("duplicate body %+v", r)
	}
}
func TestOfficeXMLDecodedTextLimit(t *testing.T) {
	// Unused XML text is still decoded data; tag/part limits do not substitute for text limits.
	doc := rewritePackage(t, fixture(t, "e01.docx"), func(p map[string][]byte) { p["extra.xml"] = []byte("<root>" + strings.Repeat("x", 2000) + "</root>") })
	r := parseBytes(t, doc, "docx", ParseLimits{TextBytes: 1000})
	if r.Status != ParseLimitExceeded {
		t.Fatalf("decoded XML text %+v", r)
	}
}
func TestXLSXInheritedStyleDoesNotInventDisplay(t *testing.T) {
	xls := rewritePackage(t, fixture(t, "f01.xlsx"), func(p map[string][]byte) {
		p["xl/styles.xml"] = bytes.Replace(p["xl/styles.xml"], []byte(`<xf numFmtId="2"/>`), []byte(`<xf xfId="1" applyNumberFormat="0" numFmtId="2"/>`), 1)
	})
	r := parseBytes(t, xls, "xlsx", ParseLimits{})
	c := cellAt(t, r, "B2")
	if c.Cell.DisplayValue != nil || c.Cell.RawValue != "128.10" || !warning(r, "unsupported_style_inheritance") {
		t.Fatalf("inherited style %+v warnings %+v", c.Cell, r.Warnings)
	}
}
func TestIndependentProducerControls(t *testing.T) {
	r := parseBytes(t, fixture(t, "native-health.docx"), "docx", ParseLimits{})
	if r.Status != ParsePartial || !warning(r, "unparsed_package_part") {
		t.Fatalf("native DOCX %+v", r)
	}
	if len(r.Segments) != 6 || r.Segments[0].Text != "虚构健康报告：患者张明 出生1980-03-12" || r.Segments[4].Text != "周青" {
		t.Fatalf("native DOCX segments %+v", r.Segments)
	}
	l := r.Segments[4].Locator.DOCX
	if l.PartURI != "/word/document.xml" || l.Row != 2 || l.GridColumn != 2 || l.Paragraph != 1 || !reflect.DeepEqual(l.TablePath, []int{1}) {
		t.Fatalf("native DOCX locator %+v", l)
	}
	t.Logf("native DOCX status=%s warnings=%+v", r.Status, r.Warnings)
	r = parseBytes(t, fixture(t, "native-home.xlsx"), "xlsx", ParseLimits{})
	if r.Status != ParseParsed {
		t.Fatalf("native XLSX %+v", r)
	}
	if len(r.Segments) != 8 || cellAt(t, r, "B1").Text != "测试苑1号测试室" || cellAt(t, r, "B2").Text != "DEMO-AC" || cellAt(t, r, "B3").Text != "2028-05-31" {
		t.Fatalf("native XLSX %+v", r.Segments)
	}
	if s := cellAt(t, r, "B3"); s.Locator.XLSX.SheetName != "家庭设备" || s.Locator.XLSX.PartURI != "/xl/worksheets/sheet1.xml" || s.Cell.NumberFormatCode != "yyyy-mm-dd" || s.Cell.RawValue != "46904" {
		t.Fatalf("native date %+v", s)
	}
	if cellAt(t, r, "A5").Cell.MergeRange != "A5:B5" || !cellAt(t, r, "C7").Cell.RowHidden {
		t.Fatal("native merge/hidden evidence missing")
	}
	t.Logf("native XLSX status=%s warnings=%+v", r.Status, r.Warnings)
}
func TestOfficeIdentityDoesNotDependOnPartExtension(t *testing.T) {
	b := rewritePackage(t, fixture(t, "e01.docx"), func(p map[string][]byte) {
		p["document.payload"] = p["content/main.xml"]
		delete(p, "content/main.xml")
		p["[Content_Types].xml"] = bytes.ReplaceAll(p["[Content_Types].xml"], []byte("content/main.xml"), []byte("document.payload"))
		p["_rels/.rels"] = bytes.ReplaceAll(p["_rels/.rels"], []byte("content/main.xml"), []byte("document.payload"))
	})
	r := parseBytes(t, b, "csv", ParseLimits{})
	if r.Status != ParseParsed || r.DetectedFormat != "docx" || r.Segments[0].Locator.DOCX.PartURI != "/document.payload" {
		t.Fatalf("identity %+v", r)
	}
}
func TestXLSXEmptyDefinedNamesDoesNotReduceCoverage(t *testing.T) {
	r := parseBytes(t, fixture(t, "native-home.xlsx"), "xlsx", ParseLimits{})
	if r.Status != ParseParsed {
		t.Fatalf("empty named definitions should not hide content: %+v", r.Warnings)
	}
}
func TestSyntheticCorpusExactTextAndCSV(t *testing.T) {
	for _, name := range []string{"feeding.txt", "home.txt", "school.txt"} {
		b := fixture(t, name)
		r := parseBytes(t, b, "text", ParseLimits{})
		if r.Status != ParseParsed || len(r.Segments) != 1 || r.Segments[0].Text != string(b) {
			t.Fatalf("%s: %+v", name, r)
		}
	}
	r := parseBytes(t, fixture(t, "f02.csv"), "csv", ParseLimits{})
	if r.Status != ParseParsed || len(r.Segments) != 15 || r.Segments[5].Text != "TEST-BILL-001" || r.Segments[6].Text != "128.10" || r.Segments[12].Text != "USD" || *r.Segments[12].Locator.CSV != (CSVLocation{3, 3, 3, 21}) {
		t.Fatalf("CSV corpus %+v", r)
	}
}
func FuzzParseBounded(f *testing.F) {
	for _, name := range []string{"e01.docx", "e02.xlsx"} {
		b, e := os.ReadFile("../../testdata/archive/" + name)
		if e != nil {
			f.Fatal(e)
		}
		f.Add(b)
	}
	for _, b := range [][]byte{[]byte("a,b\r\n1,2\r\n"), []byte{255, 254, 0x2d, 0x4e}, []byte("PK truncated"), []byte(`<!DOCTYPE a><a/>`)} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		r := parseBytes(t, b, "csv", ParseLimits{SourceBytes: 1 << 20, ExpandedBytes: 2 << 20, PartBytes: 1 << 20, TextBytes: 64 << 10, StringBytes: 16 << 10, XMLNodes: 5000, Cells: 2000, Segments: 2000})
		switch r.Status {
		case ParseParsed, ParseEmpty:
			if !r.Coverage.Complete {
				t.Fatal("complete status without coverage")
			}
		case ParsePartial, ParseInvalid, ParseUnsupported, ParseEncrypted, ParseLimitExceeded:
			if r.Coverage.Complete {
				t.Fatal("rejected document claimed complete")
			}
		default:
			t.Fatalf("unknown status %q", r.Status)
		}
	})
}
func TestFiniteNumberDisplayFormats(t *testing.T) {
	for _, c := range []struct {
		raw, format, want     string
		date1904, ok, phantom bool
	}{
		{"128.10", "General", "128.10", false, true, false}, {"128.1", "0.00", "128.10", false, true, false}, {"12.4", "0", "12", false, true, false}, {"0.125", "0%", "12%", false, true, false}, {"0.125", "0.00%", "12.50%", false, true, false}, {"60", "mm-dd-yy", "02-29-00", false, true, true}, {"0", "yyyy-mm-dd", "1904-01-01", true, true, false}, {"61", "yyyy-mm-dd", "1900-03-01", false, true, false}, {"61.5", "yyyy-mm-dd", "", false, false, false}, {"1", "unrecognized", "", false, false, false}, {"10000000000000000", "0.00", "", false, false, false},
	} {
		n, _ := strconv.ParseFloat(c.raw, 64)
		got, ok, phantom := formatNumber(n, c.raw, c.format, c.date1904)
		if got != c.want || ok != c.ok || phantom != c.phantom {
			t.Errorf("%+v -> %q %v %v", c, got, ok, phantom)
		}
	}
}
