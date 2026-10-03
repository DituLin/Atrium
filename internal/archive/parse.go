package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ParserVersion scopes evidence IDs; they are stable only within this version.
const ParserVersion = "archive-parse/1"

type ParseStatus string

const (
	ParseParsed        ParseStatus = "parsed"
	ParsePartial       ParseStatus = "partial"
	ParseEmpty         ParseStatus = "empty"
	ParseUnsupported   ParseStatus = "unsupported"
	ParseEncrypted     ParseStatus = "encrypted"
	ParseInvalid       ParseStatus = "invalid"
	ParseLimitExceeded ParseStatus = "limit_exceeded"
)

// ParseSource is an already authorized, immutable original. SHA256 identifies
// that original; callers verify it before parsing. No filesystem path is accepted.
type ParseSource struct {
	Reader     io.ReaderAt `json:"-"`
	Size       int64       `json:"size"`
	SHA256     string      `json:"sha256"`
	FormatHint string      `json:"format_hint"`
}
type ParseLimits struct {
	SourceBytes      int64 `json:"source_bytes"`
	PartBytes        int64 `json:"part_bytes"`
	ExpandedBytes    int64 `json:"expanded_bytes"`
	TextBytes        int64 `json:"text_bytes"`
	ZIPEntries       int   `json:"zip_entries"`
	XMLDepth         int   `json:"xml_depth"`
	XMLNodes         int   `json:"xml_nodes"`
	Strings          int   `json:"strings"`
	StringBytes      int   `json:"string_bytes"`
	Cells            int   `json:"cells"`
	Segments         int   `json:"segments"`
	Relationships    int   `json:"relationships"`
	CompressionRatio int   `json:"compression_ratio"`
}

func DefaultParseLimits() ParseLimits {
	return ParseLimits{32 << 20, 16 << 20, 64 << 20, 2 << 20, 4096, 64, 200000, 100000, 256 << 10, 100000, 100000, 8192, 1000}
}

type ParseCounts struct {
	DecodedTextBytes int64 `json:"decoded_text_bytes"`
	SourceBytes      int64 `json:"source_bytes"`
	ExpandedBytes    int64 `json:"expanded_bytes"`
	TextBytes        int64 `json:"text_bytes"`
	ZIPEntries       int   `json:"zip_entries"`
	PartsRead        int   `json:"parts_read"`
	XMLNodes         int   `json:"xml_nodes"`
	Strings          int   `json:"strings"`
	Cells            int   `json:"cells"`
	Segments         int   `json:"segments"`
	Relationships    int   `json:"relationships"`
}
type ParseCoverage struct {
	Complete        bool     `json:"complete"`
	Encoding        string   `json:"encoding"`
	OffsetUnit      string   `json:"offset_unit"`
	Notes           []string `json:"notes"`
	ReadParts       []string `json:"read_parts"`
	SkippedParts    []string `json:"skipped_parts"`
	SkippedFeatures []string `json:"skipped_features"`
}
type ParseWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	PartURI string `json:"part_uri"`
}
type ParseResult struct {
	DetectedFormat string          `json:"detected_format"`
	ParserVersion  string          `json:"parser_version"`
	Status         ParseStatus     `json:"status"`
	Segments       []Segment       `json:"segments"`
	Warnings       []ParseWarning  `json:"warnings"`
	Counts         ParseCounts     `json:"counts"`
	Coverage       ParseCoverage   `json:"coverage"`
	Sheets         []SheetEvidence `json:"sheets"`
}
type Segment struct {
	ID        string        `json:"id"`
	Text      string        `json:"text"`
	Locator   Locator       `json:"locator"`
	Cell      *CellEvidence `json:"cell"`
	DOCXMerge *DOCXMerge    `json:"docx_merge"`
}
type Locator struct {
	Kind string        `json:"kind"`
	Text *TextLocation `json:"text"`
	CSV  *CSVLocation  `json:"csv"`
	DOCX *DOCXLocation `json:"docx"`
	XLSX *XLSXLocation `json:"xlsx"`
}

// Ranges are half open Unicode scalar (Go rune) positions in decoded text, not original bytes.
type TextLocation struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Unit  string `json:"unit"`
}

// CSV row/column are one based logical positions. PhysicalRow/ByteColumn are
// encoding/csv.FieldPos positions in decoded input; CRLF inside fields becomes LF.
type CSVLocation struct {
	Record      int `json:"record"`
	Column      int `json:"column"`
	PhysicalRow int `json:"physical_row"`
	ByteColumn  int `json:"byte_column"`
}

// TablePath alternates table ordinal, containing row, containing grid column,
// then nested table ordinal. Row/GridColumn/Paragraph are one based.
type DOCXLocation struct {
	PartURI    string `json:"part_uri"`
	Paragraph  int    `json:"paragraph"`
	TablePath  []int  `json:"table_path"`
	Row        int    `json:"row"`
	GridColumn int    `json:"grid_column"`
}
type XLSXLocation struct {
	SheetID   string `json:"sheet_id"`
	SheetName string `json:"sheet_name"`
	PartURI   string `json:"part_uri"`
	Cell      string `json:"cell"`
}
type DOCXMerge struct {
	// Revisions can leave vertical topology unknown. In that case anchors and
	// RowSpan are zero, but the physical ColumnSpan and continuation marker survive.
	VerticalUnresolved bool `json:"vertical_unresolved"`
	AnchorRow          int  `json:"anchor_row"`
	AnchorColumn       int  `json:"anchor_column"`
	ColumnSpan         int  `json:"column_span"`
	RowSpan            int  `json:"row_span"`
	Continuation       bool `json:"continuation"`
}
type FormulaEvidence struct {
	Text         string            `json:"text"`
	Attributes   map[string]string `json:"attributes"`
	CachePresent bool              `json:"cache_present"`
	CachedRaw    string            `json:"cached_raw"`
}
type CellEvidence struct {
	StyleAttributes  map[string]string `json:"style_attributes"`
	State            string            `json:"state"`
	Type             string            `json:"type"`
	RawValue         string            `json:"raw_value"`
	ValuePresent     bool              `json:"value_present"`
	Formula          *FormulaEvidence  `json:"formula"`
	StyleIndex       int               `json:"style_index"`
	NumberFormatID   int               `json:"number_format_id"`
	NumberFormatCode string            `json:"number_format_code"`
	DisplayValue     *string           `json:"display_value"`
	MergeRange       string            `json:"merge_range"`
	MergeAnchor      string            `json:"merge_anchor"`
	RowHidden        bool              `json:"row_hidden"`
	ColumnHidden     bool              `json:"column_hidden"`
}
type SheetEvidence struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	PartURI string           `json:"part_uri"`
	State   string           `json:"state"`
	Rows    []RowEvidence    `json:"rows"`
	Columns []ColumnEvidence `json:"columns"`
	Merges  []string         `json:"merges"`
}
type RowEvidence struct {
	Index  int  `json:"index"`
	Hidden bool `json:"hidden"`
}
type ColumnEvidence struct {
	First  int  `json:"first"`
	Last   int  `json:"last"`
	Hidden bool `json:"hidden"`
}

// Parse never modifies or opens an original. A nil error can accompany a rejected
// document; callers must inspect Status and Coverage. I/O and cancellation are errors.
func Parse(ctx context.Context, source ParseSource, limits ParseLimits) (ParseResult, error) {
	p := &parser{ctx: ctx, source: source, limits: normalizedLimits(limits), out: ParseResult{ParserVersion: ParserVersion, Status: ParseParsed}}
	if err := ctx.Err(); err != nil {
		return p.out, err
	}
	hash, err := hex.DecodeString(source.SHA256)
	if source.Reader == nil || source.Size < 0 || err != nil || len(hash) != 32 || source.SHA256 != strings.ToLower(source.SHA256) {
		return p.out, fmt.Errorf("archive: invalid parse source")
	}
	if !validLimits(p.limits) {
		return p.out, fmt.Errorf("archive: invalid parse limits")
	}
	err = p.run()
	if err != nil {
		var rejection *parseRejection
		if errors.As(err, &rejection) {
			p.out.Status = rejection.status
			p.warn(rejection.code, rejection.message, "")
		} else {
			return p.out, err
		}
	}
	if p.out.Status == ParseParsed && len(p.out.Segments) == 0 {
		p.out.Status = ParseEmpty
	}
	p.out.Coverage.Complete = p.out.Status == ParseParsed || p.out.Status == ParseEmpty
	return p.out, nil
}

type parser struct {
	ctx    context.Context
	source ParseSource
	limits ParseLimits
	out    ParseResult
}
type parseRejection struct {
	status        ParseStatus
	code, message string
}

func (r *parseRejection) Error() string { return r.message }
func reject(status ParseStatus, code, message string) error {
	return &parseRejection{status, code, message}
}
func invalid(code string) error { return reject(ParseInvalid, code, code) }
func limit(code string) error   { return reject(ParseLimitExceeded, code, code) }
func (p *parser) warn(code, message, part string) {
	for _, w := range p.out.Warnings {
		if w.Code == code {
			return
		}
	}
	p.out.Warnings = append(p.out.Warnings, ParseWarning{Code: code, Message: message})
}
func (p *parser) partial(code, message, part string) {
	p.out.Status = ParsePartial
	p.warn(code, message, part)
}
func (p *parser) add(s Segment) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	p.out.Counts.Strings++
	if p.out.Counts.Strings > p.limits.Strings {
		return limit("strings")
	}
	if len(s.Text) > p.limits.StringBytes {
		return limit("string_bytes")
	}
	if p.out.Counts.TextBytes+int64(len(s.Text)) > p.limits.TextBytes {
		return limit("text_bytes")
	}
	if len(p.out.Segments) >= p.limits.Segments {
		return limit("segments")
	}
	evidence, _ := json.Marshal(s.Locator)
	sum := sha256.Sum256([]byte(p.source.SHA256 + "\x00" + ParserVersion + "\x00" + string(evidence)))
	s.ID = hex.EncodeToString(sum[:])
	p.out.Segments = append(p.out.Segments, s)
	p.out.Counts.Segments++
	p.out.Counts.TextBytes += int64(len(s.Text))
	return nil
}
func normalizedLimits(l ParseLimits) ParseLimits {
	d := DefaultParseLimits()
	for _, x := range []struct {
		v *int64
		d int64
	}{{&l.SourceBytes, d.SourceBytes}, {&l.PartBytes, d.PartBytes}, {&l.ExpandedBytes, d.ExpandedBytes}, {&l.TextBytes, d.TextBytes}} {
		if *x.v == 0 {
			*x.v = x.d
		}
	}
	for _, x := range []struct {
		v *int
		d int
	}{{&l.ZIPEntries, d.ZIPEntries}, {&l.XMLDepth, d.XMLDepth}, {&l.XMLNodes, d.XMLNodes}, {&l.Strings, d.Strings}, {&l.StringBytes, d.StringBytes}, {&l.Cells, d.Cells}, {&l.Segments, d.Segments}, {&l.Relationships, d.Relationships}, {&l.CompressionRatio, d.CompressionRatio}} {
		if *x.v == 0 {
			*x.v = x.d
		}
	}
	return l
}
func validLimits(l ParseLimits) bool {
	for _, v := range []int64{l.SourceBytes, l.PartBytes, l.ExpandedBytes, l.TextBytes, int64(l.ZIPEntries), int64(l.XMLDepth), int64(l.XMLNodes), int64(l.Strings), int64(l.StringBytes), int64(l.Cells), int64(l.Segments), int64(l.Relationships), int64(l.CompressionRatio)} {
		if v < 1 {
			return false
		}
	}
	// Bound allocation arithmetic, including caller-supplied limits.
	return l.SourceBytes <= 1<<30 && l.PartBytes <= 1<<30 && l.ExpandedBytes <= 1<<30 && l.TextBytes <= 1<<30
}
func (p *parser) run() error {
	if p.source.Size > p.limits.SourceBytes {
		return limit("source_bytes")
	}
	data := make([]byte, int(p.source.Size))
	for offset := 0; offset < len(data); {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		end := min(offset+32*1024, len(data))
		n, err := p.source.Reader.ReadAt(data[offset:end], int64(offset))
		p.out.Counts.SourceBytes += int64(n)
		offset += n
		if err != nil && !(err == io.EOF && offset == len(data)) {
			return fmt.Errorf("archive: read original: %w", err)
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if bytes.HasPrefix(data, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		p.out.DetectedFormat = "compound"
		return reject(ParseUnsupported, "unsupported_compound_container", "Compound storage is not proof of encryption")
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		p.out.DetectedFormat = "pdf"
		return reject(ParseUnsupported, "unsupported_pdf", "PDF parsing belongs to the page parser")
	}
	if hasZIPSignature(data) || officeFormatHint(p.source.FormatHint) {
		return p.parseOffice(data)
	}
	switch strings.ToLower(p.source.FormatHint) {
	case "", "text", "txt", "text/plain":
		p.out.DetectedFormat = "text"
	case "csv", "text/csv":
		p.out.DetectedFormat = "csv"
	default:
		return reject(ParseUnsupported, "unsupported_format", "No supported package or text format")
	}
	decoded, err := p.decodeText(data)
	if err != nil {
		return err
	}
	p.out.Counts.DecodedTextBytes = int64(len(decoded))
	if p.out.DetectedFormat == "csv" {
		return p.parseCSV(decoded)
	}
	p.out.Coverage.OffsetUnit = "unicode_scalar"
	if len(decoded) == 0 {
		return nil
	}
	return p.add(Segment{Text: decoded, Locator: Locator{Kind: "text", Text: &TextLocation{0, len([]rune(decoded)), "unicode_scalar"}}})
}

// Prose can begin with the letters PK. ZIP detection requires a complete record
// signature; an explicit Office hint still routes malformed packages to invalid.
func hasZIPSignature(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	switch string(data[:4]) {
	case "PK\x03\x04", "PK\x05\x06", "PK\x07\x08", "PK\x01\x02", "PK\x06\x06", "PK\x06\x07":
		return true
	}
	return false
}
func officeFormatHint(hint string) bool {
	switch strings.ToLower(hint) {
	case "docx", "xlsx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return true
	}
	return false
}
