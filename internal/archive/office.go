package archive

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"hash/crc32"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	nsPackageRelationships = "http://schemas.openxmlformats.org/package/2006/relationships"
	nsContentTypes         = "http://schemas.openxmlformats.org/package/2006/content-types"
	nsRelationships        = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsWord                 = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsSheet                = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	docxMain               = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	xlsxMain               = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"
)

type xmlNode struct {
	name     xml.Name
	attrs    []xml.Attr
	text     string
	children []*xmlNode
}

func (n *xmlNode) attr(ns, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.attrs {
		if a.Name.Space == ns && a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}
func (n *xmlNode) child(ns, name string) *xmlNode {
	if n != nil {
		for _, c := range n.children {
			if c.name.Space == ns && c.name.Local == name {
				return c
			}
		}
	}
	return nil
}
func (n *xmlNode) all(ns, name string) []*xmlNode {
	var out []*xmlNode
	if n != nil {
		for _, c := range n.children {
			if c.name.Space == ns && c.name.Local == name {
				out = append(out, c)
			}
		}
	}
	return out
}

type relationship struct {
	id, kind, target string
	external         bool
}
type officePackage struct {
	p     *parser
	parts map[string][]byte
	xml   map[string]*xmlNode
	rels  map[string][]relationship
	used  map[string]bool
}

// Preflight scans the directory's bounded byte slice before archive/zip can
// allocate File objects from attacker-controlled entry counts. ZIP64/multidisk
// are outside this initial parser, including sentinel fields in small sources.
func (p *parser) preflightZIP(data []byte) error {
	end := -1
	for i := len(data) - 22; i >= 0 && i >= len(data)-22-65535; i-- {
		if binary.LittleEndian.Uint32(data[i:i+4]) == 0x06054b50 && i+22+int(binary.LittleEndian.Uint16(data[i+20:i+22])) == len(data) {
			end = i
			break
		}
	}
	if end < 0 {
		return invalid("invalid_zip")
	}
	e := data[end:]
	count := int(binary.LittleEndian.Uint16(e[10:12]))
	size := uint64(binary.LittleEndian.Uint32(e[12:16]))
	offset := uint64(binary.LittleEndian.Uint32(e[16:20]))
	if count == 65535 || size == 0xffffffff || offset == 0xffffffff {
		return reject(ParseUnsupported, "unsupported_zip64", "ZIP64 packages are unsupported")
	}
	if binary.LittleEndian.Uint16(e[4:6]) != 0 || binary.LittleEndian.Uint16(e[6:8]) != 0 || int(binary.LittleEndian.Uint16(e[8:10])) != count {
		return invalid("invalid_zip")
	}
	if count > p.limits.ZIPEntries {
		return limit("zip_entries")
	}
	if size > uint64(len(data)) || offset > uint64(end) || offset+size != uint64(end) {
		return invalid("invalid_zip")
	}
	seen := 0
	for at := int(offset); at < end; {
		if at+46 > end || binary.LittleEndian.Uint32(data[at:at+4]) != 0x02014b50 {
			return invalid("invalid_zip")
		}
		seen++
		if seen > p.limits.ZIPEntries {
			return limit("zip_entries")
		}
		extra := int(binary.LittleEndian.Uint16(data[at+28:at+30])) + int(binary.LittleEndian.Uint16(data[at+30:at+32])) + int(binary.LittleEndian.Uint16(data[at+32:at+34]))
		at += 46 + extra
		if at > end {
			return invalid("invalid_zip")
		}
	}
	if seen != count {
		return invalid("invalid_zip")
	}
	return nil
}
func canonicalPart(name string) (string, error) {
	decoded, err := url.PathUnescape(name)
	if err != nil || decoded == "" || len(decoded) > 1024 || !utf8.ValidString(decoded) || strings.ContainsAny(decoded, "\\\\:#?\x00") || strings.HasPrefix(decoded, "/") || path.Clean(decoded) != decoded {
		return "", invalid("invalid_zip_path")
	}
	for _, r := range decoded {
		if r < 32 || r == 127 {
			return "", invalid("invalid_zip_path")
		}
	}
	for _, part := range strings.Split(decoded, "/") {
		if part == "" || part == "." || part == ".." {
			return "", invalid("invalid_zip_path")
		}
	}
	return decoded, nil
}
func (p *parser) parseOffice(data []byte) error {
	p.out.DetectedFormat = "zip"
	if err := p.preflightZIP(data); err != nil {
		return err
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return invalid("invalid_zip")
	}
	if err := validateLocalZIPHeaders(data); err != nil {
		return err
	}
	if len(z.File) > p.limits.ZIPEntries {
		return limit("zip_entries")
	}
	p.out.Counts.ZIPEntries = len(z.File)
	pkg := &officePackage{p: p, parts: map[string][]byte{}, xml: map[string]*xmlNode{}, rels: map[string][]relationship{}, used: map[string]bool{}}
	seen := map[string]bool{}
	var declared uint64
	// Validate all directory entries before any expansion.
	names := make([]string, len(z.File))
	for i, f := range z.File {
		name, e := canonicalPart(f.Name)
		if e != nil {
			return e
		}
		key := strings.ToLower(name)
		if seen[key] {
			return invalid("duplicate_zip_part")
		}
		seen[key] = true
		names[i] = name
		if !f.Mode().IsRegular() {
			return invalid("nonregular_zip_part")
		}
		if f.Flags&1 != 0 {
			return reject(ParseEncrypted, "encrypted_zip", "ZIP encryption flag is set")
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return reject(ParseUnsupported, "unsupported_zip_method", "Only Store and Deflate are supported")
		}
		if f.UncompressedSize64 > uint64(p.limits.PartBytes) {
			return limit("part_bytes")
		}
		declared += f.UncompressedSize64
		if declared > uint64(p.limits.ExpandedBytes) {
			return limit("expanded_bytes")
		}
		if f.UncompressedSize64 > 0 && (f.CompressedSize64 == 0 || float64(f.UncompressedSize64)/float64(f.CompressedSize64) > float64(p.limits.CompressionRatio)) {
			return limit("compression_ratio")
		}
	}
	for i, f := range z.File {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		raw, e := f.OpenRaw()
		if e != nil {
			return invalid("corrupt_zip_part")
		}
		var r io.ReadCloser = io.NopCloser(raw)
		if f.Method == zip.Deflate {
			r = flate.NewReader(raw)
		}
		var out bytes.Buffer
		buf := make([]byte, 32*1024)
		for {
			if e = p.ctx.Err(); e != nil {
				r.Close()
				return e
			}
			remaining := min(p.limits.PartBytes-int64(out.Len()), p.limits.ExpandedBytes-p.out.Counts.ExpandedBytes)
			n, readErr := r.Read(buf[:min(int64(len(buf)), remaining+1)])
			p.out.Counts.ExpandedBytes += int64(n)
			if int64(out.Len()+n) > p.limits.PartBytes || p.out.Counts.ExpandedBytes > p.limits.ExpandedBytes {
				r.Close()
				return limit("actual_expanded_bytes")
			}
			out.Write(buf[:n])
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				r.Close()
				return invalid("corrupt_zip_part")
			}
			if n == 0 {
				r.Close()
				return invalid("corrupt_zip_part")
			}
		}
		if e = r.Close(); e != nil {
			return invalid("corrupt_zip_part")
		}
		if uint64(out.Len()) != f.UncompressedSize64 || crc32.ChecksumIEEE(out.Bytes()) != f.CRC32 {
			return invalid("corrupt_zip_part")
		}
		p.out.Counts.PartsRead++
		pkg.parts[names[i]] = out.Bytes()
		p.out.Coverage.ReadParts = append(p.out.Coverage.ReadParts, "/"+names[i])
	}
	sort.Strings(p.out.Coverage.ReadParts)
	// All XML parts, including skipped parts, obey XML resource/entity limits.
	for _, part := range p.out.Coverage.ReadParts {
		name := strings.TrimPrefix(part, "/")
		if strings.HasSuffix(strings.ToLower(name), ".xml") || strings.HasSuffix(name, ".rels") {
			root, e := p.readXML(pkg.parts[name])
			if e != nil {
				return e
			}
			pkg.xml[name] = root
		}
	}
	types, e := pkg.root("[Content_Types].xml", nsContentTypes, "Types")
	if e != nil {
		if errors.Is(e, errMissingPart) {
			return invalid("missing_content_types")
		}
		return e
	}
	overrides := map[string]string{}
	defaults := map[string]string{}
	for _, n := range types.children {
		if n.name.Space != nsContentTypes {
			return invalid("invalid_content_types")
		}
		switch n.name.Local {
		case "Override":
			raw := n.attr("", "PartName")
			if !strings.HasPrefix(raw, "/") {
				return invalid("invalid_content_types")
			}
			name, e := canonicalPart(raw[1:])
			if e != nil {
				return e
			}
			if _, ok := overrides[name]; ok {
				return invalid("invalid_content_types")
			}
			overrides[name] = n.attr("", "ContentType")
		case "Default":
			ext := n.attr("", "Extension")
			if _, ok := defaults[ext]; ok {
				return invalid("invalid_content_types")
			}
			defaults[ext] = n.attr("", "ContentType")
		default:
			return invalid("invalid_content_types")
		}
	}
	for _, part := range p.out.Coverage.ReadParts {
		name := strings.TrimPrefix(part, "/")
		if strings.HasSuffix(name, ".rels") {
			if e := pkg.readRelationships(name); e != nil {
				return e
			}
		}
	}
	var main string
	for _, r := range pkg.rels[""] {
		if r.kind == nsRelationships+"/officeDocument" && !r.external {
			if main != "" {
				return invalid("multiple_main_parts")
			}
			main = r.target
		}
	}
	if main == "" {
		return reject(ParseUnsupported, "unsupported_main_relationship", "No Transitional Office main relationship")
	}
	typ := overrides[main]
	if typ == "" {
		typ = defaults[strings.TrimPrefix(path.Ext(main), ".")]
	}
	switch typ {
	case docxMain:
		p.out.DetectedFormat = "docx"
		err = pkg.parseDOCX(main)
	case xlsxMain:
		p.out.DetectedFormat = "xlsx"
		err = pkg.parseXLSX(main)
	default:
		return reject(ParseUnsupported, "unsupported_main_content_type", "Main part is not supported DOCX/XLSX")
	}
	pkg.finishCoverage()
	return err
}
func (p *parser) readXML(b []byte) (*xmlNode, error) {
	utf16XML := bytes.HasPrefix(b, []byte{255, 254}) || bytes.HasPrefix(b, []byte{254, 255})
	if utf16XML {
		previousEncoding := p.out.Coverage.Encoding
		decoded, err := p.decodeText(b)
		p.out.Coverage.Encoding = previousEncoding
		if err != nil {
			return nil, err
		}
		b = []byte(decoded)
	} else {
		// A UTF-8 signature is encoding metadata, not character data before root.
		b = bytes.TrimPrefix(b, []byte{239, 187, 191})
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	d.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if utf16XML && strings.EqualFold(charset, "utf-16") {
			return input, nil
		}
		return nil, invalid("unsupported_xml_encoding")
	}
	var stack []*xmlNode
	var root *xmlNode
	for {
		if err := p.ctx.Err(); err != nil {
			return nil, err
		}
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, invalid("invalid_xml")
		}
		switch n := tok.(type) {
		case xml.Directive:
			return nil, invalid("xml_directive")
		case xml.StartElement:
			p.out.Counts.XMLNodes++
			if p.out.Counts.XMLNodes > p.limits.XMLNodes {
				return nil, limit("xml_nodes")
			}
			if len(stack) >= p.limits.XMLDepth {
				return nil, limit("xml_depth")
			}
			node := &xmlNode{name: n.Name, attrs: n.Attr}
			attrs := map[xml.Name]bool{}
			for _, a := range n.Attr {
				if attrs[a.Name] {
					return nil, invalid("duplicate_xml_attribute")
				}
				attrs[a.Name] = true
				if err := p.xmlString(a.Value); err != nil {
					return nil, err
				}
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, invalid("invalid_xml")
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, invalid("invalid_xml")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(n)) != "" {
					return nil, invalid("invalid_xml")
				}
				continue
			}
			node := stack[len(stack)-1]
			if len(node.text)+len(n) > p.limits.StringBytes {
				return nil, limit("string_bytes")
			}
			if err := p.xmlString(string(n)); err != nil {
				return nil, err
			}
			node.text += string(n)
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, invalid("invalid_xml")
	}
	return root, nil
}
func (p *parser) xmlString(s string) error {
	p.out.Counts.DecodedTextBytes += int64(len(s))
	if p.out.Counts.DecodedTextBytes > p.limits.TextBytes {
		return limit("decoded_text_bytes")
	}
	p.out.Counts.Strings++
	if p.out.Counts.Strings > p.limits.Strings {
		return limit("strings")
	}
	if len(s) > p.limits.StringBytes {
		return limit("string_bytes")
	}
	return nil
}

var errMissingPart = errors.New("missing part")

func (pkg *officePackage) root(name, ns, tag string) (*xmlNode, error) {
	n, ok := pkg.xml[name]
	if !ok {
		data, exists := pkg.parts[name]
		if !exists {
			return nil, errMissingPart
		}
		var err error
		n, err = pkg.p.readXML(data)
		if err != nil {
			return nil, err
		}
		pkg.xml[name] = n
	}
	pkg.used[name] = true
	if n.name.Space != ns {
		return nil, reject(ParseUnsupported, "unsupported_xml_namespace", "Only Transitional OOXML namespaces are supported")
	}
	if n.name.Local != tag {
		return nil, invalid("invalid_xml_root")
	}
	return n, nil
}
func (pkg *officePackage) readRelationships(name string) error {
	var source string
	if name != "_rels/.rels" {
		dir := path.Dir(name)
		if path.Base(dir) != "_rels" {
			return invalid("invalid_relationship_part")
		}
		source = path.Join(path.Dir(dir), strings.TrimSuffix(path.Base(name), ".rels"))
		if _, ok := pkg.parts[source]; !ok {
			return invalid("orphan_relationship_part")
		}
	}
	root, err := pkg.root(name, nsPackageRelationships, "Relationships")
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, n := range root.children {
		if n.name.Space != nsPackageRelationships || n.name.Local != "Relationship" {
			return invalid("invalid_relationship")
		}
		pkg.p.out.Counts.Relationships++
		if pkg.p.out.Counts.Relationships > pkg.p.limits.Relationships {
			return limit("relationships")
		}
		r := relationship{id: n.attr("", "Id"), kind: n.attr("", "Type"), external: n.attr("", "TargetMode") == "External"}
		if r.id == "" || ids[r.id] || r.kind == "" {
			return invalid("invalid_relationship")
		}
		ids[r.id] = true
		if r.external {
			pkg.p.partial("external_relationship", "External targets are never fetched", "/"+source)
		} else {
			if mode := n.attr("", "TargetMode"); mode != "" && mode != "Internal" {
				return invalid("invalid_relationship")
			}
			target := n.attr("", "Target")
			u, e := url.Parse(target)
			if e != nil || u.Scheme != "" || u.Host != "" || u.RawQuery != "" || strings.ContainsAny(u.Path, "\\\\\x00") {
				return invalid("relationship_escape")
			}
			if u.Path == "" {
				r.target = source
			} else if strings.HasPrefix(u.Path, "/") {
				r.target = strings.TrimPrefix(u.Path, "/")
			} else {
				r.target = path.Join(path.Dir(source), u.Path)
			}
			if r.target == ".." || strings.HasPrefix(r.target, "../") {
				return invalid("relationship_escape")
			}
			r.target, e = canonicalPart(r.target)
			if e != nil {
				return invalid("relationship_escape")
			}
			if _, ok := pkg.parts[r.target]; !ok {
				return invalid("missing_relationship_target")
			}
		}
		pkg.rels[source] = append(pkg.rels[source], r)
	}
	return nil
}
func (pkg *officePackage) skip(code, part string) {
	pkg.p.partial(code, "Feature retained in original; extraction is incomplete", "/"+part)
	for _, s := range pkg.p.out.Coverage.SkippedFeatures {
		if s == code {
			return
		}
	}
	pkg.p.out.Coverage.SkippedFeatures = append(pkg.p.out.Coverage.SkippedFeatures, code)
}

// ZIP metadata must agree at each layer. Do not let archive/zip's intentionally
// permissive local-header handling reinterpret a package differently from us.
func validateLocalZIPHeaders(data []byte) error {
	// The directory was already fully range-checked by preflightZIP.
	end := len(data) - 22
	for end >= 0 {
		if binary.LittleEndian.Uint32(data[end:end+4]) == 0x06054b50 && end+22+int(binary.LittleEndian.Uint16(data[end+20:end+22])) == len(data) {
			break
		}
		end--
	}
	if end < 0 {
		return invalid("invalid_zip")
	}
	start := int(binary.LittleEndian.Uint32(data[end+16 : end+20]))
	var spans [][2]int
	for at := start; at < end; {
		dir := data[at:]
		nameLen := int(binary.LittleEndian.Uint16(dir[28:30]))
		extraLen := int(binary.LittleEndian.Uint16(dir[30:32]))
		commentLen := int(binary.LittleEndian.Uint16(dir[32:34]))
		localAt := int(binary.LittleEndian.Uint32(dir[42:46]))
		if localAt < 0 || localAt+30 > start || binary.LittleEndian.Uint32(data[localAt:localAt+4]) != 0x04034b50 {
			return invalid("invalid_zip_local_header")
		}
		local := data[localAt:]
		flags := binary.LittleEndian.Uint16(dir[8:10])
		localFlags := binary.LittleEndian.Uint16(local[6:8])
		if flags&1 != 0 || localFlags&1 != 0 {
			return reject(ParseEncrypted, "encrypted_zip", "ZIP encryption flag is set")
		}
		if flags != localFlags || !bytes.Equal(dir[10:12], local[8:10]) || binary.LittleEndian.Uint16(dir[34:36]) != 0 {
			return invalid("invalid_zip_local_header")
		}
		localNameLen := int(binary.LittleEndian.Uint16(local[26:28]))
		localExtraLen := int(binary.LittleEndian.Uint16(local[28:30]))
		offset := localAt + 30 + localNameLen + localExtraLen
		if offset > start || localNameLen != nameLen || !bytes.Equal(data[localAt+30:localAt+30+localNameLen], dir[46:46+nameLen]) {
			return invalid("invalid_zip_local_header")
		}
		compressed := uint64(binary.LittleEndian.Uint32(dir[20:24]))
		expanded := binary.LittleEndian.Uint32(dir[24:28])
		if compressed == 0xffffffff || expanded == 0xffffffff {
			return reject(ParseUnsupported, "unsupported_zip64", "ZIP64 packages are unsupported")
		}
		finish := uint64(offset) + compressed
		if finish > uint64(start) {
			return invalid("invalid_zip_local_header")
		}
		if flags&8 == 0 {
			if !bytes.Equal(local[14:26], dir[16:28]) {
				return invalid("corrupt_zip_part")
			}
		} else {
			descriptor := int(finish)
			if descriptor+4 <= start && binary.LittleEndian.Uint32(data[descriptor:descriptor+4]) == 0x08074b50 {
				descriptor += 4
			}
			if descriptor+12 > start || !bytes.Equal(data[descriptor:descriptor+12], dir[16:28]) {
				return invalid("corrupt_zip_part")
			}
			finish = uint64(descriptor + 12)
		}
		spans = append(spans, [2]int{localAt, int(finish)})
		at += 46 + nameLen + extraLen + commentLen
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	for i := 1; i < len(spans); i++ {
		if spans[i][0] < spans[i-1][1] {
			return invalid("overlapping_zip_parts")
		}
	}
	return nil
}

func (pkg *officePackage) finishCoverage() {
	// These parts describe presentation/application metadata, outside text/cell
	// evidence. Inventory them, but do not claim they were semantically decoded.
	ancillary := map[string]bool{}
	for _, rels := range pkg.rels {
		for _, r := range rels {
			switch r.kind {
			case nsRelationships + "/styles", nsRelationships + "/stylesWithEffects", nsRelationships + "/settings", nsRelationships + "/webSettings", nsRelationships + "/fontTable", nsRelationships + "/theme", nsRelationships + "/extended-properties", nsPackageRelationships + "/metadata/core-properties", nsPackageRelationships + "/metadata/thumbnail":
				ancillary[r.target] = true
			}
		}
	}
	for _, part := range pkg.p.out.Coverage.ReadParts {
		name := part[1:]
		if pkg.used[name] {
			continue
		}
		pkg.p.out.Coverage.SkippedParts = append(pkg.p.out.Coverage.SkippedParts, part)
		if ancillary[name] {
			continue
		}
		code := "unparsed_package_part"
		if n := pkg.xml[name]; n != nil && n.name.Space == nsWord {
			switch n.name.Local {
			case "hdr", "ftr":
				code = "docx_headers"
			case "footnotes", "endnotes":
				code = "docx_notes"
			case "comments":
				code = "docx_comments"
			}
		}
		pkg.skip(code, name)
	}
	pkg.p.out.Coverage.Notes = append(pkg.p.out.Coverage.Notes, "OOXML text/cell evidence only; read_parts are CRC-checked package bytes. skipped_parts were not semantically extracted; known presentation/application metadata does not make body/cell coverage partial. Nested archives are never expanded.")
}
