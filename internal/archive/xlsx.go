package archive

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type cellStyle struct {
	id                     int
	code                   string
	attributes             map[string]string
	unsupportedInheritance bool
}

func (pkg *officePackage) parseXLSX(main string) error {
	root, err := pkg.root(main, nsSheet, "workbook")
	if err != nil {
		return err
	}
	if err := xlsxSingletons(root, "workbookPr", "sheets", "bookViews", "calcPr", "fileVersion", "workbookProtection", "definedNames"); err != nil {
		return err
	}
	date1904, err := xmlBool(root.child(nsSheet, "workbookPr").attr("", "date1904"))
	if err != nil {
		return err
	}
	var shared []string
	styles := []cellStyle{{id: 0, code: "General"}}
	rels := map[string]relationship{}
	seenKinds := map[string]bool{}
	for _, r := range pkg.rels[main] {
		rels[r.id] = r
		switch r.kind {
		case nsRelationships + "/sharedStrings", nsRelationships + "/styles":
			if r.external {
				continue
			}
			if seenKinds[r.kind] {
				return invalid("duplicate_workbook_part")
			}
			seenKinds[r.kind] = true
			if r.kind == nsRelationships+"/sharedStrings" {
				sst, e := pkg.root(r.target, nsSheet, "sst")
				if e != nil {
					return e
				}
				for _, si := range sst.all(nsSheet, "si") {
					s, e := pkg.richString(si, r.target)
					if e != nil {
						return e
					}
					shared = append(shared, s)
				}
			} else {
				styles, err = pkg.readStyles(r.target)
				if err != nil {
					return err
				}
			}
		}
	}
	sheets := root.child(nsSheet, "sheets")
	if sheets == nil {
		return invalid("missing_workbook_sheets")
	}
	ids, names, parts := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, n := range sheets.children {
		if n.name.Space != nsSheet || n.name.Local != "sheet" {
			return invalid("invalid_workbook_sheet")
		}
		id, name, rid := n.attr("", "sheetId"), n.attr("", "name"), n.attr(nsRelationships, "id")
		if _, e := positiveInt(id, 2147483647); e != nil || name == "" || ids[id] || names[strings.ToLower(name)] {
			return invalid("invalid_workbook_sheet")
		}
		ids[id] = true
		names[strings.ToLower(name)] = true
		r, ok := rels[rid]
		if !ok || r.external {
			return invalid("invalid_sheet_relationship")
		}
		if r.kind != nsRelationships+"/worksheet" {
			pkg.skip("unsupported_sheet_type", r.target)
			continue
		}
		if parts[r.target] {
			return invalid("duplicate_sheet_part")
		}
		parts[r.target] = true
		state := n.attr("", "state")
		if state == "" {
			state = "visible"
		}
		if state != "visible" && state != "hidden" && state != "veryHidden" {
			return invalid("invalid_sheet_state")
		}
		sheet := SheetEvidence{ID: id, Name: name, PartURI: "/" + r.target, State: state}
		if err := pkg.readSheet(r.target, &sheet, shared, styles, date1904); err != nil {
			return err
		}
		pkg.p.out.Sheets = append(pkg.p.out.Sheets, sheet)
	}
	for _, n := range root.children {
		switch n.name.Local {
		case "workbookPr", "sheets", "bookViews", "calcPr", "fileVersion", "workbookProtection":
		case "definedNames":
			if len(n.children) > 0 {
				pkg.skip("xlsx_workbook_features", main)
			}
		default:
			pkg.skip("xlsx_workbook_features", main)
		}
	}
	return nil
}

// Validate recognized singleton containers before selecting a child. This is
// intentionally not a complete schema validator; repeated worksheet cols are legal.
func xlsxSingletons(root *xmlNode, names ...string) error {
	for _, name := range names {
		if len(root.all(nsSheet, name)) > 1 {
			return invalid("duplicate_xlsx_container")
		}
	}
	return nil
}

func xmlBool(s string) (bool, error) {
	switch s {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	}
	return false, invalid("invalid_boolean")
}
func positiveInt(s string, max int) (int, error) {
	if s == "" {
		return 0, invalid("invalid_integer")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, invalid("invalid_integer")
		}
	}
	n, e := strconv.Atoi(s)
	if e != nil || n < 1 || n > max {
		return 0, invalid("invalid_integer")
	}
	return n, nil
}
func (pkg *officePackage) richString(n *xmlNode, part string) (string, error) {
	var b strings.Builder
	for _, c := range n.children {
		if c.name.Space != nsSheet {
			pkg.skip("xlsx_unknown_content", part)
			continue
		}
		switch c.name.Local {
		case "t":
			b.WriteString(c.text)
		case "r":
			for _, t := range c.all(nsSheet, "t") {
				b.WriteString(t.text)
			}
		case "phoneticPr", "rPh":
			pkg.skip("xlsx_phonetic_text", part)
		default:
			pkg.skip("xlsx_unknown_content", part)
		}
		if b.Len() > pkg.p.limits.StringBytes {
			return "", limit("string_bytes")
		}
	}
	return b.String(), nil
}

var builtinFormats = map[int]string{0: "General", 1: "0", 2: "0.00", 9: "0%", 10: "0.00%", 14: "mm-dd-yy", 49: "@"}

func (pkg *officePackage) readStyles(part string) ([]cellStyle, error) {
	root, err := pkg.root(part, nsSheet, "styleSheet")
	if err != nil {
		return nil, err
	}
	if err := xlsxSingletons(root, "numFmts", "cellXfs", "cellStyleXfs"); err != nil {
		return nil, err
	}
	formats := map[int]string{}
	for _, n := range root.child(nsSheet, "numFmts").all(nsSheet, "numFmt") {
		id, e := strconv.Atoi(n.attr("", "numFmtId"))
		if e != nil || id < 164 || formats[id] != "" {
			return nil, invalid("invalid_number_format")
		}
		formats[id] = n.attr("", "formatCode")
	}
	var styles []cellStyle
	for _, n := range root.child(nsSheet, "cellXfs").all(nsSheet, "xf") {
		id := 0
		if raw := n.attr("", "numFmtId"); raw != "" {
			id, err = strconv.Atoi(raw)
			if err != nil || id < 0 {
				return nil, invalid("invalid_number_format")
			}
		}
		code := builtinFormats[id]
		if id >= 164 {
			code = formats[id]
		}
		attributes := map[string]string{}
		for _, a := range n.attrs {
			key := a.Name.Local
			if a.Name.Space != "" {
				key = "{" + a.Name.Space + "}" + key
			}
			attributes[key] = a.Value
		}
		inherit := n.attr("", "applyNumberFormat") == "0" || n.attr("", "applyNumberFormat") == "false" || (n.attr("", "xfId") != "" && n.attr("", "xfId") != "0")
		styles = append(styles, cellStyle{id, code, attributes, inherit})
	}
	if len(styles) == 0 {
		styles = []cellStyle{{id: 0, code: "General"}}
	}
	return styles, nil
}

type cellPoint struct{ row, col int }
type mergeArea struct {
	ref, anchor string
	first, last cellPoint
}

func parseA1(s string) (cellPoint, error) {
	pos := 0
	col := 0
	for pos < len(s) && s[pos] >= 'A' && s[pos] <= 'Z' {
		col = col*26 + int(s[pos]-'A') + 1
		pos++
		if col > 16384 {
			return cellPoint{}, invalid("invalid_cell_coordinate")
		}
	}
	if pos == 0 || pos == len(s) || s[pos] == '0' {
		return cellPoint{}, invalid("invalid_cell_coordinate")
	}
	row, e := positiveInt(s[pos:], 1048576)
	if e != nil {
		return cellPoint{}, invalid("invalid_cell_coordinate")
	}
	return cellPoint{row, col}, nil
}
func coordinate(row, col int) string {
	var b [3]byte
	i := 3
	for col > 0 {
		col--
		i--
		b[i] = byte('A' + col%26)
		col /= 26
	}
	return string(b[i:]) + strconv.Itoa(row)
}
func parseMerge(s string) (mergeArea, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return mergeArea{}, invalid("invalid_merge_range")
	}
	a, e := parseA1(parts[0])
	if e != nil {
		return mergeArea{}, e
	}
	b, e := parseA1(parts[1])
	if e != nil || a.row > b.row || a.col > b.col {
		return mergeArea{}, invalid("invalid_merge_range")
	}
	return mergeArea{s, parts[0], a, b}, nil
}
func (pkg *officePackage) readSheet(part string, sheet *SheetEvidence, shared []string, styles []cellStyle, date1904 bool) error {
	root, err := pkg.root(part, nsSheet, "worksheet")
	if err != nil {
		return err
	}
	if err := xlsxSingletons(root, "sheetData", "mergeCells", "dimension", "sheetViews", "sheetFormatPr", "pageMargins", "pageSetup", "printOptions", "sheetPr", "sheetProtection", "autoFilter"); err != nil {
		return err
	}
	if len(root.all(nsSheet, "sheetData")) != 1 {
		return invalid("invalid_sheet_data")
	}
	var merges []mergeArea
	for _, n := range root.child(nsSheet, "mergeCells").all(nsSheet, "mergeCell") {
		if len(merges) >= 4096 {
			return limit("merge_ranges")
		}
		m, e := parseMerge(n.attr("", "ref"))
		if e != nil {
			return e
		}
		for _, old := range merges {
			if m.first.row <= old.last.row && m.last.row >= old.first.row && m.first.col <= old.last.col && m.last.col >= old.first.col {
				return invalid("overlapping_merge_ranges")
			}
		}
		merges = append(merges, m)
		sheet.Merges = append(sheet.Merges, m.ref)
	}
	// Difference counts bound work by the number of declarations plus the fixed
	// XLSX column domain, even when many ranges cover the whole sheet.
	var visibleDelta, hiddenDelta [16386]int
	for _, cols := range root.all(nsSheet, "cols") {
		for _, n := range cols.all(nsSheet, "col") {
			if err := pkg.p.ctx.Err(); err != nil {
				return err
			}
			first, e := positiveInt(n.attr("", "min"), 16384)
			if e != nil {
				return e
			}
			last, e := positiveInt(n.attr("", "max"), 16384)
			if e != nil || last < first {
				return invalid("invalid_column_range")
			}
			hidden, e := xmlBool(n.attr("", "hidden"))
			if e != nil {
				return e
			}
			sheet.Columns = append(sheet.Columns, ColumnEvidence{first, last, hidden})
			delta := &visibleDelta
			if hidden {
				delta = &hiddenDelta
			}
			delta[first]++
			delta[last+1]--
		}
	}
	var visibility [16385]uint8 // 0 visible/default, 1 hidden, 2 conflicting
	visible, hidden := 0, 0
	conflict := false
	for col := 1; col <= 16384; col++ {
		if col%1024 == 0 {
			if err := pkg.p.ctx.Err(); err != nil {
				return err
			}
		}
		visible += visibleDelta[col]
		hidden += hiddenDelta[col]
		if hidden > 0 {
			visibility[col] = 1
			if visible > 0 {
				visibility[col] = 2
				conflict = true
			}
		}
	}
	if conflict {
		pkg.skip("conflicting_column_visibility", part)
	}
	previousRow := 0
	data := root.child(nsSheet, "sheetData")
	if data == nil {
		return invalid("missing_sheet_data")
	}
	for _, row := range data.children {
		if row.name.Space != nsSheet || row.name.Local != "row" {
			pkg.skip("xlsx_unknown_content", part)
			continue
		}
		index := previousRow + 1
		if raw := row.attr("", "r"); raw != "" {
			index, err = positiveInt(raw, 1048576)
			if err != nil {
				return err
			}
		}
		if index <= previousRow || index > 1048576 {
			return invalid("invalid_row_order")
		}
		previousRow = index
		hidden, e := xmlBool(row.attr("", "hidden"))
		if e != nil {
			return e
		}
		sheet.Rows = append(sheet.Rows, RowEvidence{index, hidden})
		previousCol := 0
		for _, c := range row.children {
			if c.name.Space != nsSheet || c.name.Local != "c" {
				pkg.skip("xlsx_unknown_content", part)
				continue
			}
			pkg.p.out.Counts.Cells++
			if pkg.p.out.Counts.Cells > pkg.p.limits.Cells {
				return limit("cells")
			}
			ref := c.attr("", "r")
			if ref == "" {
				if previousCol >= 16384 {
					return invalid("invalid_cell_coordinate")
				}
				ref = coordinate(index, previousCol+1)
			}
			point, e := parseA1(ref)
			if e != nil {
				return e
			}
			if point.row != index || point.col <= previousCol {
				return invalid("invalid_cell_order")
			}
			previousCol = point.col
			segment, e := pkg.readCell(c, shared, styles, date1904, part)
			if e != nil {
				return e
			}
			segment.Locator = Locator{Kind: "xlsx", XLSX: &XLSXLocation{sheet.ID, sheet.Name, sheet.PartURI, ref}}
			segment.Cell.RowHidden = hidden
			segment.Cell.ColumnHidden = visibility[point.col] == 1
			segment.Cell.ColumnHiddenUnresolved = visibility[point.col] == 2
			for _, m := range merges {
				if point.row >= m.first.row && point.row <= m.last.row && point.col >= m.first.col && point.col <= m.last.col {
					segment.Cell.MergeRange = m.ref
					segment.Cell.MergeAnchor = m.anchor
					break
				}
			}
			if err := pkg.p.add(segment); err != nil {
				return err
			}
		}
	}
	for _, n := range root.children {
		if n.name.Space != nsSheet {
			pkg.skip("xlsx_unknown_content", part)
			continue
		}
		switch n.name.Local {
		case "dimension", "sheetData", "cols", "mergeCells", "sheetViews", "sheetFormatPr", "pageMargins", "pageSetup", "printOptions", "sheetPr", "sheetProtection", "autoFilter":
		default:
			pkg.skip("xlsx_sheet_features", part)
		}
	}
	for _, r := range pkg.rels[part] {
		switch r.kind {
		case nsRelationships + "/drawing", nsRelationships + "/image", nsRelationships + "/comments", nsRelationships + "/vmlDrawing":
			pkg.skip("xlsx_attachments", r.target)
		}
	}
	return nil
}
func (pkg *officePackage) readCell(n *xmlNode, shared []string, styles []cellStyle, date1904 bool, part string) (Segment, error) {
	for _, name := range []string{"v", "f", "is"} {
		if len(n.all(nsSheet, name)) > 1 {
			return Segment{}, invalid("duplicate_cell_value")
		}
	}
	cell := &CellEvidence{Type: n.attr("", "t"), State: "blank"}
	if cell.Type == "" {
		cell.Type = "n"
	}
	if n.child(nsSheet, "is") != nil && cell.Type != "inlineStr" {
		return Segment{}, invalid("invalid_inline_string")
	}
	s := Segment{Cell: cell}
	v := n.child(nsSheet, "v")
	if v != nil {
		cell.RawValue = v.text
		cell.ValuePresent = true
	}
	if raw := n.attr("", "s"); raw != "" {
		index, e := strconv.Atoi(raw)
		if e != nil || index < 0 || index >= len(styles) {
			return s, invalid("invalid_style_index")
		}
		cell.StyleIndex = index
	}
	style := styles[cell.StyleIndex]
	cell.NumberFormatID = style.id
	cell.NumberFormatCode = style.code
	cell.StyleAttributes = style.attributes
	if f := n.child(nsSheet, "f"); f != nil {
		cell.Formula = &FormulaEvidence{Text: f.text, Attributes: map[string]string{}, CachePresent: v != nil, CachedRaw: cell.RawValue}
		for _, a := range f.attrs {
			key := a.Name.Local
			if a.Name.Space != "" {
				key = "{" + a.Name.Space + "}" + key
			}
			cell.Formula.Attributes[key] = a.Value
		}
		if v == nil {
			cell.State = "formula_missing_cache"
			pkg.p.warn("formula_missing_cache", "Formula has no stored cache; it was not calculated", "/"+part)
		} else {
			cell.State = "formula_cached"
		}
	}
	inline := n.child(nsSheet, "is")
	if cell.Formula == nil && (v != nil || inline != nil) {
		cell.State = "value"
	}
	switch cell.Type {
	case "inlineStr":
		if cell.Formula != nil || v != nil {
			return s, invalid("invalid_inline_string")
		}
		if inline != nil {
			value, e := pkg.richString(inline, part)
			if e != nil {
				return s, e
			}
			s.Text = value
			cell.ValuePresent = true
			cell.RawValue = value
			if value == "" {
				cell.State = "empty_string"
			}
		}
	case "s":
		if v != nil {
			index, e := strconv.Atoi(v.text)
			if e != nil || index < 0 || index >= len(shared) {
				return s, invalid("invalid_shared_string")
			}
			s.Text = shared[index]
			if cell.Formula == nil && s.Text == "" {
				cell.State = "empty_string"
			}
		}
	case "str":
		s.Text = cell.RawValue
		if v != nil && s.Text == "" && cell.Formula == nil {
			cell.State = "empty_string"
		}
	case "b":
		if v != nil {
			if v.text != "0" && v.text != "1" {
				return s, invalid("invalid_boolean_cell")
			}
			if v.text == "1" {
				s.Text = "TRUE"
			} else {
				s.Text = "FALSE"
			}
		}
	case "e":
		s.Text = cell.RawValue
	case "d":
		s.Text = cell.RawValue
		if v != nil {
			if !validISODate(v.text) {
				return s, invalid("invalid_date_cell")
			}
		}
	case "n":
		s.Text = cell.RawValue
		if v != nil && v.text != "" {
			num, e := strconv.ParseFloat(v.text, 64)
			// Decimal date bounds are checked from raw below, including values
			// outside float64's range. Syntax errors and nonfinite literals remain invalid.
			dateRange := (style.code == "yyyy-mm-dd" || style.code == "mm-dd-yy") && errors.Is(e, strconv.ErrRange)
			if !dateRange && (e != nil || math.IsNaN(num) || math.IsInf(num, 0)) {
				return s, invalid("invalid_numeric_cell")
			}
			display, ok, phantom := formatNumber(num, v.text, style.code, date1904)
			if style.unsupportedInheritance {
				ok = false
				pkg.skip("unsupported_style_inheritance", part)
			}
			if ok {
				cell.DisplayValue = &display
				s.Text = display
			} else {
				pkg.skip("unknown_number_format", part)
			}
			if phantom {
				pkg.p.warn("excel_phantom_date", "Serial 60 denotes Excel's fictional 1900-02-29, not a Gregorian date", "/"+part)
			}
		} else if v != nil && cell.Formula == nil {
			cell.State = "blank"
		}
	default:
		pkg.skip("unsupported_cell_type", part)
		s.Text = cell.RawValue
	}
	for _, c := range n.children {
		if c.name.Space != nsSheet || (c.name.Local != "v" && c.name.Local != "f" && c.name.Local != "is") {
			pkg.skip("xlsx_unknown_content", part)
		}
	}
	return s, nil
}
func validISODate(s string) bool {
	for _, layout := range []string{"2006-01-02", time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		if _, e := time.Parse(layout, s); e == nil {
			return true
		}
	}
	return false
}

// Only these finite formats are rendered. Other styles retain raw values;
// there is no locale/timezone guessing, Excel format interpreter, or calculation.
func formatNumber(n float64, raw, code string, date1904 bool) (string, bool, bool) {
	switch code {
	case "General", "@":
		return raw, true, false
	case "0":
		if math.Abs(n) > 1e15 {
			return "", false, false
		}
		display, ok := decimalDisplay(raw, 0, 0)
		return display, ok, false
	case "0.00":
		if math.Abs(n) > 1e15 {
			return "", false, false
		}
		display, ok := decimalDisplay(raw, 2, 0)
		return display, ok, false
	case "0%", "0.00%":
		if math.Abs(n) > 1e13 {
			return "", false, false
		}
		decimals := 0
		if code == "0.00%" {
			decimals = 2
		}
		display, ok := decimalDisplay(raw, decimals, 2)
		if !ok {
			return "", false, false
		}
		return display + "%", true, false
	case "yyyy-mm-dd", "mm-dd-yy":
		days, ok := decimalWholeDays(raw)
		if !ok {
			return "", false, false
		}
		phantom := !date1904 && days == 60
		if phantom {
			if code == "mm-dd-yy" {
				return "02-29-00", true, true
			}
			return "1900-02-29", true, true
		}
		base := time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
		if date1904 {
			base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
		} else if days > 60 {
			days--
		}
		date := base.AddDate(0, 0, days)
		if code == "mm-dd-yy" {
			return date.Format("01-02-06"), true, false
		}
		return fmt.Sprintf("%04d-%02d-%02d", date.Year(), date.Month(), date.Day()), true, false
	}
	return "", false, false
}

type sourceDecimal struct {
	negative bool
	digits   string // Leading zeroes removed; empty denotes zero.
	point    int    // Decimal point position relative to digits.
}

// Read the exact decimal without expanding an exponent or losing tiny nonzero
// fractions to float64 underflow. Source string length is bounded by XML limits.
func parseSourceDecimal(raw string) (sourceDecimal, bool) {
	negative := strings.HasPrefix(raw, "-")
	if strings.HasPrefix(raw, "-") || strings.HasPrefix(raw, "+") {
		raw = raw[1:]
	}
	exponent := 0
	if e := strings.IndexAny(raw, "eE"); e >= 0 {
		var err error
		exponent, err = strconv.Atoi(raw[e+1:])
		if err != nil || exponent < -1000000 || exponent > 1000000 {
			return sourceDecimal{}, false
		}
		raw = raw[:e]
	}
	whole := len(raw)
	if dot := strings.IndexByte(raw, '.'); dot >= 0 {
		whole = dot
		raw = raw[:dot] + raw[dot+1:]
	}
	if raw == "" {
		return sourceDecimal{}, false
	}
	for _, digit := range raw {
		if digit < '0' || digit > '9' {
			return sourceDecimal{}, false
		}
	}
	digits := strings.TrimLeft(raw, "0")
	return sourceDecimal{negative, digits, whole + exponent - (len(raw) - len(digits))}, true
}

func decimalWholeDays(raw string) (int, bool) {
	d, ok := parseSourceDecimal(raw)
	if !ok {
		return 0, false
	}
	if d.digits == "" {
		return 0, true
	}
	// At most seven whole digits are needed for the supported serial range.
	// Removing trailing zeroes allows exact integer spellings such as 610e-1.
	if d.negative || d.point <= 0 || d.point > 7 || len(strings.TrimRight(d.digits, "0")) > d.point {
		return 0, false
	}
	days := 0
	for i := 0; i < d.point; i++ {
		days *= 10
		if i < len(d.digits) {
			days += int(d.digits[i] - '0')
		}
	}
	return days, days <= 2957003
}

// decimalDisplay rounds the original decimal spelling, not its float64
// approximation. Moving the decimal point implements percent scaling exactly.
// The finite formats have at most two decimals and at most 18 scaled digits;
// exponents are bounded before arithmetic or padding, including tiny underflows.
func decimalDisplay(raw string, places, percentShift int) (string, bool) {
	d, ok := parseSourceDecimal(raw)
	if !ok {
		return "", false
	}
	digits := d.digits
	cut := d.point + places + percentShift
	units := "0"
	if digits != "" {
		if cut > 18 {
			return "", false
		}
		roundUp := cut >= 0 && cut < len(digits) && digits[cut] >= '5'
		if cut > 0 {
			if cut <= len(digits) {
				units = digits[:cut]
			} else {
				units = digits + strings.Repeat("0", cut-len(digits))
			}
		}
		if roundUp {
			bytes := []byte(units)
			carry := true
			for i := len(bytes) - 1; i >= 0; i-- {
				if bytes[i] == '9' {
					bytes[i] = '0'
				} else {
					bytes[i]++
					carry = false
					break
				}
			}
			units = string(bytes)
			if carry {
				units = "1" + units
			}
		}
	}
	nonzero := units != "0"
	if places > 0 {
		if len(units) <= places {
			units = strings.Repeat("0", places+1-len(units)) + units
		}
		split := len(units) - places
		units = units[:split] + "." + units[split:]
	}
	if d.negative && nonzero {
		units = "-" + units
	}
	return units, true
}
