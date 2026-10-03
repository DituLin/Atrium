package archive

// Understanding is an extraction boundary, not identity resolution or a durable
// consumer. All IDs below are scoped to this immutable plan or one extraction.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"
)

const UnderstandingVersion = "archive-understand/2"
const UnderstandingSchema = "archive-extraction/2"
const ChunkVersion = "archive-chunk/1"
const WorkerProtocol = 1
const MaxChunkInput = 32 << 10
const MaxChunkOutput = 64 << 10
const maxEvidenceBytes = 24 << 10
const maxChunks = 256

type UnderstandingError struct{ Code string }

func (e *UnderstandingError) Error() string { return "archive: " + e.Code }
func understandingError(code string) error  { return &UnderstandingError{code} }
func errorCode(err error) string {
	if e, ok := err.(*UnderstandingError); ok {
		return e.Code
	}
	return ""
}
func digestJSON(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type DerivedValue struct {
	Ref                 string `json:"ref"`
	OriginalComponentID string `json:"original_component_id"`
	FormatComponentID   string `json:"format_component_id"`
	ParseDigest         string `json:"parse_digest"`
	ParserVersion       string `json:"parser_version"`
	Value               string `json:"value"`
}
type EvidenceStructure struct {
	Locator                Locator    `json:"locator"`
	CellState              string     `json:"cell_state"`
	CellType               string     `json:"cell_type"`
	ValuePresent           bool       `json:"value_present"`
	CachePresent           bool       `json:"cache_present"`
	RowHidden              bool       `json:"row_hidden"`
	ColumnHidden           bool       `json:"column_hidden"`
	ColumnHiddenUnresolved bool       `json:"column_hidden_unresolved"`
	MergeRange             string     `json:"merge_range"`
	MergeAnchor            string     `json:"merge_anchor"`
	DOCXMerge              *DOCXMerge `json:"docx_merge"`
}
type EvidenceSpan struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}
type EvidenceView struct {
	QuoteHints      []EvidenceSpan    `json:"quote_hints"`
	ViewID          string            `json:"view_id"`
	ParentSegmentID string            `json:"parent_segment_id"`
	AlternativeID   string            `json:"alternative_id"`
	ComponentID     string            `json:"component_id"`
	ComponentKind   string            `json:"component_kind"`
	Origin          string            `json:"origin"`
	Start           int               `json:"start"`
	End             int               `json:"end"`
	Text            string            `json:"text"`
	ParseDigest     string            `json:"parse_digest"`
	ParserVersion   string            `json:"parser_version"`
	RequiresReview  bool              `json:"requires_review"`
	VisualState     string            `json:"visual_state"`
	Structure       EvidenceStructure `json:"structure"`
	Derived         *DerivedValue     `json:"derived"`
}
type UnderstandingChunk struct {
	ProtocolVersion   int            `json:"protocol_version"`
	InvocationID      string         `json:"invocation_id"`
	InputDigest       string         `json:"input_digest"`
	SchemaVersion     string         `json:"schema_version"`
	ExtractionVersion string         `json:"extraction_version"`
	ChunkVersion      string         `json:"chunk_version"`
	ParseDigest       string         `json:"parse_digest"`
	Index             int            `json:"index"`
	Owned             []EvidenceView `json:"owned"`
	Context           []EvidenceView `json:"context"`
}

func (c UnderstandingChunk) Evidence() []EvidenceView {
	return append(append([]EvidenceView{}, c.Owned...), c.Context...)
}
func (c UnderstandingChunk) inputDigest() string { c.InputDigest = ""; return digestJSON(c) }

type UnderstandingPlan struct {
	StructuralContextLimited bool                 `json:"structural_context_limited"`
	Version                  string               `json:"version"`
	ParseDigest              string               `json:"parse_digest"`
	ParserVersion            string               `json:"parser_version"`
	Status                   ParseStatus          `json:"status"`
	Coverage                 ParseCoverage        `json:"coverage"`
	Warnings                 []ParseWarning       `json:"warnings"`
	Sheets                   []SheetEvidence      `json:"sheets"`
	Native                   *NativeMetadata      `json:"native"`
	Chunks                   []UnderstandingChunk `json:"chunks"`
	ManifestDigest           string               `json:"manifest_digest"`
}

func (p UnderstandingPlan) manifestDigest() string { p.ManifestDigest = ""; return digestJSON(p) }

// PlanChunks snapshots a trusted ParseResult. JSON serialization is bounded by
// actual wire size, not a character heuristic. It never trims whitespace.
func PlanChunks(parsed ParseResult) (UnderstandingPlan, error) {
	var p UnderstandingPlan
	if !validUnderstandingParserVersion(parsed.ParserVersion) {
		return p, understandingError("understand_parse_version")
	}
	if len(parsed.Segments) > 100000 {
		return p, understandingError("understand_plan_limit")
	}
	if !allEvidenceStringsValid(reflect.ValueOf(parsed)) {
		return p, understandingError("understand_parse_invalid")
	}
	data, err := json.Marshal(parsed)
	if err != nil || len(data) > 32<<20 {
		return p, understandingError("understand_parse_invalid")
	}
	// A deep snapshot prevents aliasing parser maps/pointers into the manifest.
	var snap ParseResult
	_ = json.Unmarshal(data, &snap)
	p = UnderstandingPlan{Version: UnderstandingVersion, ParseDigest: digestJSON(snap), ParserVersion: snap.ParserVersion, Status: snap.Status, Coverage: snap.Coverage, Warnings: snap.Warnings, Sheets: snap.Sheets, Native: snap.Native, Chunks: []UnderstandingChunk{}}
	current := []EvidenceView{}
	seen := map[string]bool{}
	flush := func() error {
		if len(current) == 0 {
			return nil
		}
		if len(p.Chunks) >= maxChunks {
			return understandingError("understand_plan_limit")
		}
		c := UnderstandingChunk{ProtocolVersion: WorkerProtocol, SchemaVersion: UnderstandingSchema, ExtractionVersion: UnderstandingVersion, ChunkVersion: ChunkVersion, ParseDigest: p.ParseDigest, Index: len(p.Chunks), Owned: current, Context: []EvidenceView{}}
		c.InvocationID = fmt.Sprintf("%s:%d", p.ParseDigest, c.Index)
		c.InputDigest = c.inputDigest()
		b, _ := json.Marshal(c)
		if len(b) > MaxChunkInput {
			return understandingError("understand_input_limit")
		}
		p.Chunks = append(p.Chunks, c)
		current = []EvidenceView{}
		return nil
	}
	sizeOK := func(v []EvidenceView) bool {
		b, _ := json.Marshal(v)
		if len(v) > 128 || len(b) > maxEvidenceBytes {
			return false
		}
		c := UnderstandingChunk{ProtocolVersion: WorkerProtocol, InvocationID: strings.Repeat("a", 160), InputDigest: strings.Repeat("a", 64), SchemaVersion: UnderstandingSchema, ExtractionVersion: UnderstandingVersion, ChunkVersion: ChunkVersion, ParseDigest: p.ParseDigest, Index: 255, Owned: v, Context: []EvidenceView{}}
		return completionInputBytes(c) <= MaxChunkInput
	}
	rawAdd := func(group []EvidenceView) error {
		if !sizeOK(group) {
			return understandingError("understand_group_limit")
		}
		test := append(append([]EvidenceView{}, current...), group...)
		if !sizeOK(test) || len(current) > 0 && contextBudgetBytes(p.ParseDigest, test) > MaxChunkInput-(8<<10) {
			if err := flush(); err != nil {
				return err
			}
		}
		current = append(current, group...)
		return nil
	}

	pending := [][]EvidenceView{}
	pendingBytes, pendingUnits := 0, 0
	flushRow := func() error {
		if len(pending) == 0 {
			return nil
		}
		combined := []EvidenceView{}
		for _, g := range pending {
			combined = append(combined, g...)
		}
		if sizeOK(combined) {
			if err := rawAdd(combined); err != nil {
				return err
			}
		} else {
			p.StructuralContextLimited = true
			for _, g := range pending {
				if err := rawAdd(g); err != nil {
					return err
				}
			}
		}
		pending = nil
		pendingBytes, pendingUnits = 0, 0
		return nil
	}
	add := func(group []EvidenceView) error {
		if len(pending) > 0 && !sameStructuralRow(pending[0][0], group[0]) {
			if err := flushRow(); err != nil {
				return err
			}
		}
		if hasStructuralRow(group[0]) {
			b, _ := json.Marshal(group)
			if len(pending) > 0 && (pendingBytes+len(b) > maxEvidenceBytes || pendingUnits+len(group) > 128) {
				p.StructuralContextLimited = true
				if err := flushRow(); err != nil {
					return err
				}
			}
			pendingBytes += len(b)
			pendingUnits += len(group)
			pending = append(pending, group)
			return nil
		}
		return rawAdd(group)
	}
	for _, s := range snap.Segments {
		if s.ID == "" || len(s.ID) > 256 || seen[s.ID] || !utf8.ValidString(s.Text) {
			return UnderstandingPlan{}, understandingError("understand_parse_invalid")
		}
		seen[s.ID] = true
		views := componentViews(s, p.ParseDigest, p.ParserVersion)
		// Visual alternatives are atomic. A single group
		// that cannot fit fails closed; it is never silently truncated.
		if s.Visual != nil {
			if err := add(views); err != nil {
				return UnderstandingPlan{}, err
			}
			continue
		}
		for _, v := range views {
			if v.ComponentKind == "numeric_raw" || v.ComponentKind == "historical_cache" || v.ComponentKind == "derived_display" || v.ComponentKind == "format_code" {
				if err := add([]EvidenceView{v}); err != nil {
					return UnderstandingPlan{}, err
				}
				continue
			}
			runes := []rune(v.Text)
			if len(runes) == 0 {
				if err := add([]EvidenceView{v}); err != nil {
					return UnderstandingPlan{}, err
				}
				continue
			}
			for start := 0; start < len(runes); {
				// Bound each text fragment to leave room for structural metadata. Binary
				// search uses JSON escaping (including CR/LF) and UTF-8 bytes exactly.
				hi := len(runes)
				if hi-start > 16000 {
					hi = start + 16000
				}
				lo := start + 1
				best := start
				for lo <= hi {
					mid := (lo + hi) / 2
					f := fragment(v, runes, start, mid)
					if sizeOK([]EvidenceView{f}) && contextBudgetBytes(p.ParseDigest, []EvidenceView{f}) <= MaxChunkInput-(8<<10) {
						best = mid
						lo = mid + 1
					} else {
						hi = mid - 1
					}
				}
				if best == start {
					return UnderstandingPlan{}, understandingError("understand_group_limit")
				}
				// Prefer paragraph endings; do not drop the delimiter or surrounding text.
				if best < len(runes) {
					for i := best - 1; i > start+(best-start)/2; i-- {
						if runes[i] == '\n' {
							best = i + 1
							break
						}
					}
				}
				if err := add([]EvidenceView{fragment(v, runes, start, best)}); err != nil {
					return UnderstandingPlan{}, err
				}
				start = best
			}
		}
	}
	if err := flushRow(); err != nil {
		return UnderstandingPlan{}, err
	}
	if err := flush(); err != nil {
		return UnderstandingPlan{}, err
	}
	addUnderstandingContext(&p)
	p.ManifestDigest = p.manifestDigest()
	return p, nil
}
func fragment(v EvidenceView, r []rune, start, end int) EvidenceView {
	v.Start = start
	v.End = end
	v.Text = string(r[start:end])
	v.QuoteHints = []EvidenceSpan{}
	for _, loc := range quoteTokens.FindAllStringIndex(v.Text, 128) {
		a := utf8.RuneCountInString(v.Text[:loc[0]]) + start
		b := utf8.RuneCountInString(v.Text[:loc[1]]) + start
		v.QuoteHints = append(v.QuoteHints, EvidenceSpan{a, b, v.Text[loc[0]:loc[1]]})
	}
	v.ViewID = fmt.Sprintf("%s/%d:%d", v.ComponentID, start, end)
	return v
}
func componentViews(s Segment, parseDigest, version string) []EvidenceView {
	st := EvidenceStructure{Locator: s.Locator, DOCXMerge: s.DOCXMerge}
	review := false
	visualState := ""
	if s.Visual != nil {
		review = s.Visual.RequiresReview || s.Visual.State == "native_unconfirmed"
		for _, a := range s.Visual.Alternatives {
			if len(s.Visual.Alternatives) > 0 && a.Text != s.Visual.Alternatives[0].Text {
				review = true
			}
		}
		visualState = s.Visual.State
	}
	var out []EvidenceView
	add := func(kind, origin, text, alternative string) {
		id := s.ID + "/" + kind + alternative
		v := EvidenceView{ParentSegmentID: s.ID, AlternativeID: alternative, ComponentID: id, ComponentKind: kind, Origin: origin, Text: text, ParseDigest: parseDigest, ParserVersion: version, RequiresReview: review, VisualState: visualState, Structure: st}
		v = fragment(v, []rune(text), 0, utf8.RuneCountInString(text))
		out = append(out, v)
	}
	if s.Visual != nil && len(s.Visual.Alternatives) > 0 {
		for i, a := range s.Visual.Alternatives {
			add("body", a.Origin, a.Text, fmt.Sprintf("/alternative-%d", i))
		}
		return out
	}
	if s.Cell == nil {
		add("body", "parsed", s.Text, "")
		return out
	}
	c := s.Cell
	st.CellState = c.State
	st.CellType = c.Type
	st.ValuePresent = c.ValuePresent
	st.RowHidden = c.RowHidden
	st.ColumnHidden = c.ColumnHidden
	st.ColumnHiddenUnresolved = c.ColumnHiddenUnresolved
	st.MergeRange = c.MergeRange
	st.MergeAnchor = c.MergeAnchor
	numeric := c.Type == "" || c.Type == "n"
	rawID := ""
	if c.Formula != nil {
		st.CachePresent = c.Formula.CachePresent
		add("formula_text", "parsed_formula", c.Formula.Text, "")
		if c.Formula.CachePresent && numeric {
			add("historical_cache", "historical_formula_cache", c.Formula.CachedRaw, "")
			rawID = out[len(out)-1].ComponentID
		} else if c.Formula.CachePresent {
			add("cell_text", "historical_formula_cache", s.Text, "")
		}
	}
	if c.Formula == nil && numeric && c.ValuePresent {
		add("numeric_raw", "parsed_raw", c.RawValue, "")
		rawID = out[len(out)-1].ComponentID
	}
	if c.Formula == nil && (!numeric || !c.ValuePresent) {
		add("cell_text", "parsed", s.Text, "")
	}
	if c.NumberFormatCode != "" {
		add("format_code", "parsed_format", c.NumberFormatCode, "")
	}
	if c.DisplayValue != nil && rawID != "" {
		add("derived_display", "parser_derived", *c.DisplayValue, "")
		v := &out[len(out)-1]
		v.Derived = &DerivedValue{Ref: parseDigest + "/" + v.ComponentID, OriginalComponentID: rawID, FormatComponentID: s.ID + "/format_code", ParseDigest: parseDigest, ParserVersion: version, Value: *c.DisplayValue}
	}
	// Formula without a cache still has an explicit empty cell evidence unit.
	if c.Formula != nil && !c.Formula.CachePresent {
		add("cell_text", "missing_cache", "", "")
	}
	return out
}

func validPlan(p UnderstandingPlan) bool {
	return p.Version == UnderstandingVersion && p.ManifestDigest == p.manifestDigest() && len(p.Chunks) <= maxChunks
}
func boundedLabel(s string) bool {
	return s != "" && len(s) <= 128 && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}

var quoteTokens = regexp.MustCompile(`[+-]?[0-9]+(?:\.[0-9]+)?(?:-[0-9]+)*(?:[eE][+-]?[0-9]+)?|[\p{L}\p{M}]+(?:/[\p{L}]+)*`)

func validUnderstandingParserVersion(v string) bool {
	if v == ParserVersion || v == NativeParserVersion {
		return true
	}
	if !strings.HasPrefix(v, NativeParserVersion+"/") {
		return false
	}
	suffix := strings.TrimPrefix(v, NativeParserVersion+"/")
	b, err := hex.DecodeString(suffix)
	return err == nil && len(b) == 32 && suffix == strings.ToLower(suffix)
}

func allEvidenceStringsValid(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Pointer:
		if !v.IsNil() {
			return allEvidenceStringsValid(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !allEvidenceStringsValid(v.Field(i)) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !allEvidenceStringsValid(v.Index(i)) {
				return false
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if !allEvidenceStringsValid(k) || !allEvidenceStringsValid(v.MapIndex(k)) {
				return false
			}
		}
	}
	return true
}

func contextBudgetBytes(parseDigest string, views []EvidenceView) int {
	return completionInputBytes(UnderstandingChunk{ProtocolVersion: WorkerProtocol, InvocationID: strings.Repeat("a", 160), InputDigest: strings.Repeat("a", 64), SchemaVersion: UnderstandingSchema, ExtractionVersion: UnderstandingVersion, ChunkVersion: ChunkVersion, ParseDigest: parseDigest, Index: 255, Owned: views, Context: []EvidenceView{}})
}

// Context is a read-only copy of existing host views. Header and nearby boundary
// units help retain a report's subject and row label/unit relationships. They
// never alter the owned partition, and remain inside every actual wire budget.
func addUnderstandingContext(p *UnderstandingPlan) {
	all := []EvidenceView{}
	positions := map[string]int{}
	groups := map[string][]EvidenceView{}
	for _, c := range p.Chunks {
		for _, v := range c.Owned {
			positions[v.ViewID] = len(all)
			all = append(all, v)
			groups[v.ParentSegmentID] = append(groups[v.ParentSegmentID], v)
		}
	}
	pageKey := func(v EvidenceView) string {
		if v.Structure.Locator.Page != nil {
			return fmt.Sprintf("page:%d", v.Structure.Locator.Page.Page)
		}
		if v.Structure.Locator.XLSX != nil {
			return "sheet:" + v.Structure.Locator.XLSX.SheetID
		}
		return "document"
	}
	headers := map[string][]int{}
	for i, v := range all {
		key := pageKey(v)
		if len(headers[key]) < 4 {
			headers[key] = append(headers[key], i)
		}
	}
	for i := range p.Chunks {
		c := &p.Chunks[i]
		owned := map[string]bool{}
		for _, v := range c.Owned {
			owned[v.ViewID] = true
		}
		seen := map[string]bool{}
		candidate := []int{}
		pages := map[string]bool{}
		for _, v := range c.Owned {
			key := pageKey(v)
			if !pages[key] {
				pages[key] = true
				h := headers[key]
				if len(h) > 4 {
					h = h[:4]
				}
				candidate = append(candidate, h...)
			}
		}
		first, last := positions[c.Owned[0].ViewID], positions[c.Owned[len(c.Owned)-1].ViewID]
		for distance := 1; distance <= 3; distance++ {
			if last+distance < len(all) && pages[pageKey(all[last+distance])] {
				candidate = append(candidate, last+distance)
			}
			if first-distance >= 0 && pages[pageKey(all[first-distance])] {
				candidate = append(candidate, first-distance)
			}
		}
		for _, v := range c.Owned {
			candidate = append(candidate, headers[pageKey(v)]...)
		}
		for _, pos := range candidate {
			v := all[pos]
			groupKey := v.ViewID
			if v.AlternativeID != "" {
				groupKey = v.ParentSegmentID
			}
			if owned[v.ViewID] || seen[groupKey] {
				continue
			}
			seen[groupKey] = true
			group := []EvidenceView{v}
			if v.AlternativeID != "" {
				group = []EvidenceView{}
				for _, g := range groups[v.ParentSegmentID] {
					if !owned[g.ViewID] {
						group = append(group, g)
					}
				}
			} else if len(v.Text) > 1024 && (v.ComponentKind == "body" || v.ComponentKind == "cell_text") {
				group = []EvidenceView{boundaryContext(v, pos < first)}
			}
			trial := *c
			trial.Context = append(append([]EvidenceView{}, c.Context...), group...)
			b, _ := json.Marshal(trial.Evidence())
			if len(trial.Owned)+len(trial.Context) <= 128 && len(b) <= maxEvidenceBytes && completionInputBytes(trial) <= MaxChunkInput-256 {
				c.Context = trial.Context
			}
		}
		c.InputDigest = c.inputDigest()
	}
}

func hasStructuralRow(v EvidenceView) bool {
	l := v.Structure.Locator
	return l.CSV != nil || l.XLSX != nil || l.DOCX != nil && l.DOCX.Row > 0 || l.Page != nil && l.Page.Rect[3] > 0
}
func sameStructuralRow(a, b EvidenceView) bool {
	x, y := a.Structure.Locator, b.Structure.Locator
	if x.CSV != nil && y.CSV != nil {
		return x.CSV.Record == y.CSV.Record
	}
	if x.XLSX != nil && y.XLSX != nil {
		return x.XLSX.SheetID == y.XLSX.SheetID && strings.TrimLeft(x.XLSX.Cell, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") == strings.TrimLeft(y.XLSX.Cell, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
	}
	if x.DOCX != nil && y.DOCX != nil {
		return x.DOCX.Row > 0 && x.DOCX.Row == y.DOCX.Row && x.DOCX.PartURI == y.DOCX.PartURI && fmt.Sprint(x.DOCX.TablePath) == fmt.Sprint(y.DOCX.TablePath)
	}
	if x.Page != nil && y.Page != nil && x.Page.Page == y.Page.Page {
		r, s := x.Page.Rect, y.Page.Rect
		overlap := math.Min(r[1]+r[3], s[1]+s[3]) - math.Max(r[1], s[1])
		return r[3] > 0 && s[3] > 0 && overlap >= 0.4*math.Min(r[3], s[3])
	}
	return false
}
func boundaryContext(v EvidenceView, tail bool) EvidenceView {
	runes := []rune(v.Text)
	start, end := 0, len(runes)
	if end > 256 {
		if tail {
			start = end - 256
		} else {
			end = 256
		}
	}
	out := fragment(v, runes, start, end)
	offset := v.Start
	out.Start += offset
	out.End += offset
	for i := range out.QuoteHints {
		out.QuoteHints[i].Start += offset
		out.QuoteHints[i].End += offset
	}
	out.ViewID = fmt.Sprintf("%s/%d:%d", out.ComponentID, out.Start, out.End)
	return out
}
