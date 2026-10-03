package archive

import (
	"strconv"
	"strings"
)

func (pkg *officePackage) parseDOCX(main string) error {
	root, err := pkg.root(main, nsWord, "document")
	if err != nil {
		return err
	}
	body := root.child(nsWord, "body")
	if body == nil || len(root.all(nsWord, "body")) != 1 {
		return invalid("missing_docx_body")
	}
	for _, r := range pkg.rels[main] {
		switch r.kind {
		case nsRelationships + "/header", nsRelationships + "/footer":
			pkg.skip("docx_headers", r.target)
		case nsRelationships + "/footnotes", nsRelationships + "/endnotes":
			pkg.skip("docx_notes", r.target)
		case nsRelationships + "/image":
			pkg.skip("docx_images", r.target)
		case nsRelationships + "/comments":
			pkg.skip("docx_comments", r.target)
		}
	}
	if docxHasRevision(body) {
		pkg.skip("docx_revisions", main)
	}
	return pkg.docxBlocks(body, main, nil, 0, 0, nil)
}
func (pkg *officePackage) docxBlocks(parent *xmlNode, part string, tablePath []int, row, column int, merge *DOCXMerge) error {
	paragraph, table := 0, 0
	var walk func(*xmlNode) error
	walk = func(parent *xmlNode) error {
		for _, n := range parent.children {
			if n.name.Space != nsWord {
				pkg.skip("docx_unknown_content", part)
				continue
			}
			switch n.name.Local {
			case "p":
				paragraph++
				var b strings.Builder
				if err := pkg.docxRuns(n, part, &b); err != nil {
					return err
				}
				l := &DOCXLocation{PartURI: "/" + part, Paragraph: paragraph, TablePath: append([]int(nil), tablePath...), Row: row, GridColumn: column}
				if err := pkg.p.add(Segment{Text: b.String(), Locator: Locator{Kind: "docx", DOCX: l}, DOCXMerge: merge}); err != nil {
					return err
				}
			case "tbl":
				table++
				nested := append([]int(nil), tablePath...)
				if len(tablePath) > 0 {
					nested = append(nested, row, column)
				}
				nested = append(nested, table)
				if err := pkg.docxTable(n, part, nested); err != nil {
					return err
				}
			case "del", "moveFrom":
				pkg.skip("docx_revisions", part)
			case "ins", "moveTo":
				pkg.skip("docx_revisions", part)
				if err := walk(n); err != nil {
					return err
				}
			case "sdt":
				pkg.skip("docx_content_controls", part)
				if inner := n.child(nsWord, "sdtContent"); inner != nil {
					if err := walk(inner); err != nil {
						return err
					}
				}
			case "sectPr", "tcPr", "tblPr", "tblGrid", "bookmarkStart", "bookmarkEnd", "proofErr":
			default:
				pkg.skip("docx_unknown_content", part)
			}
		}
		return nil
	}
	return walk(parent)
}
func (pkg *officePackage) docxRuns(n *xmlNode, part string, b *strings.Builder) error {
	for _, c := range n.children {
		if c.name.Space != nsWord {
			pkg.skip("docx_unknown_content", part)
			continue
		}
		switch c.name.Local {
		case "t":
			b.WriteString(c.text)
		case "tab":
			b.WriteByte('\t')
		case "br", "cr":
			b.WriteByte('\n')
		case "del", "moveFrom":
			pkg.skip("docx_revisions", part)
		case "ins", "moveTo":
			pkg.skip("docx_revisions", part)
			if err := pkg.docxRuns(c, part, b); err != nil {
				return err
			}
		case "drawing", "pict", "object":
			pkg.skip("docx_images", part)
		case "txbxContent":
			pkg.skip("docx_textboxes", part)
		case "footnoteReference", "endnoteReference":
			pkg.skip("docx_notes", part)
		case "instrText", "fldChar":
			pkg.skip("docx_fields", part)
		case "r", "hyperlink", "smartTag":
			if err := pkg.docxRuns(c, part, b); err != nil {
				return err
			}
		case "fldSimple":
			pkg.skip("docx_fields", part)
			if err := pkg.docxRuns(c, part, b); err != nil {
				return err
			}
		case "rPr", "pPr", "bookmarkStart", "bookmarkEnd", "proofErr", "lastRenderedPageBreak":
		default:
			pkg.skip("docx_unknown_content", part)
		}
		if b.Len() > pkg.p.limits.StringBytes {
			return limit("string_bytes")
		}
	}
	return nil
}
func docxPositive(n *xmlNode, tag string, otherwise int) (int, error) {
	c := n.child(nsWord, tag)
	if c == nil {
		return otherwise, nil
	}
	v, e := strconv.Atoi(c.attr(nsWord, "val"))
	if e != nil || v < 1 || v > 16384 {
		return 0, invalid("invalid_docx_grid")
	}
	return v, nil
}

type mergeGroup struct {
	anchor  *DOCXMerge
	members []*DOCXMerge
}

func (pkg *officePackage) docxTable(table *xmlNode, part string, tablePath []int) error {
	active := map[int]*mergeGroup{}
	var groups []*mergeGroup
	rows := table.all(nsWord, "tr")
	// Source grid coordinates remain physical. Revising rows/cells can change
	// merge topology, so do not invent current vertical links across that grid.
	// Nested tables decide independently from their own immediate properties.
	revisedGrid := docxHasRevision(table.child(nsWord, "tblGrid")) || docxHasRevision(table.child(nsWord, "tblPr"))
	for _, tr := range rows {
		revisedGrid = revisedGrid || docxHasRevision(tr.child(nsWord, "trPr"))
		for _, tc := range tr.all(nsWord, "tc") {
			revisedGrid = revisedGrid || docxHasRevision(tc.child(nsWord, "tcPr"))
		}
	}
	for rowIndex, tr := range rows {
		row := rowIndex + 1
		column := 1
		next := map[int]*mergeGroup{}
		deletedRow := tr.child(nsWord, "trPr").child(nsWord, "del") != nil
		if pr := tr.child(nsWord, "trPr"); pr != nil {
			if before := pr.child(nsWord, "gridBefore"); before != nil {
				v, e := strconv.Atoi(before.attr(nsWord, "val"))
				if e != nil || v < 0 || v > 16384 {
					return invalid("invalid_docx_grid")
				}
				column += v
			}
		}
		for _, tc := range tr.all(nsWord, "tc") {
			pkg.p.out.Counts.Cells++
			if pkg.p.out.Counts.Cells > pkg.p.limits.Cells {
				return limit("cells")
			}
			if deletedRow {
				continue
			}
			props := tc.child(nsWord, "tcPr")
			span, err := docxPositive(props, "gridSpan", 1)
			if err != nil {
				return err
			}
			if column+span-1 > 16384 {
				return invalid("invalid_docx_grid")
			}
			if props.child(nsWord, "cellDel") != nil {
				column += span
				continue
			}
			merge := &DOCXMerge{AnchorRow: row, AnchorColumn: column, ColumnSpan: span, RowSpan: 1}
			if vm := props.child(nsWord, "vMerge"); vm != nil {
				if revisedGrid {
					merge = &DOCXMerge{ColumnSpan: span, Continuation: vm.attr(nsWord, "val") != "restart", VerticalUnresolved: true}
					pkg.skip("docx_revision_merge", part)
				} else {
					switch vm.attr(nsWord, "val") {
					case "restart":
						g := &mergeGroup{anchor: merge, members: []*DOCXMerge{merge}}
						next[column] = g
						groups = append(groups, g)
					case "", "continue":
						g := active[column]
						if g == nil || g.anchor.ColumnSpan != span {
							return invalid("invalid_docx_merge")
						}
						g.anchor.RowSpan++
						merge.AnchorRow = g.anchor.AnchorRow
						merge.Continuation = true
						g.members = append(g.members, merge)
						next[column] = g
					default:
						return invalid("invalid_docx_merge")
					}
				}
			}
			if props.child(nsWord, "hMerge") != nil {
				pkg.skip("docx_legacy_merge", part)
			}
			if err := pkg.docxBlocks(tc, part, tablePath, row, column, merge); err != nil {
				return err
			}
			column += span
		}
		active = next
		// Unsupported table wrappers/revisions must never disappear silently.
		for _, n := range tr.children {
			if n.name.Space != nsWord || (n.name.Local != "tc" && n.name.Local != "trPr") {
				pkg.skip("docx_unknown_content", part)
			}
		}
	}
	for _, g := range groups {
		for _, m := range g.members {
			m.RowSpan = g.anchor.RowSpan
		}
	}
	for _, n := range table.children {
		if n.name.Space != nsWord || (n.name.Local != "tr" && n.name.Local != "tblPr" && n.name.Local != "tblGrid") {
			pkg.skip("docx_unknown_content", part)
		}
	}
	return nil
}

// Property containers can hold tracked revisions even when no revision wrapper
// surrounds their text. Inspect them for coverage; never extract old properties.
func docxHasRevision(n *xmlNode) bool {
	if n == nil {
		return false
	}
	if n.name.Space == nsWord {
		switch n.name.Local {
		case "del", "ins", "moveFrom", "moveTo", "cellDel", "cellIns", "cellMerge", "moveFromRangeStart", "moveFromRangeEnd", "moveToRangeStart", "moveToRangeEnd":
			return true
		}
		if strings.HasSuffix(n.name.Local, "Change") {
			return true
		}
	}
	for _, child := range n.children {
		if docxHasRevision(child) {
			return true
		}
	}
	return false
}
