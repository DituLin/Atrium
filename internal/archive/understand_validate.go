package archive

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type EvidenceReference struct {
	ViewID          string `json:"view_id"`
	Start           int    `json:"start"`
	End             int    `json:"end"`
	Quote           string `json:"quote"`
	DerivedValueRef string `json:"derived_value_ref"`
}
type LocalSubject struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Role      string              `json:"role"`
	Ambiguous bool                `json:"ambiguous"`
	Evidence  []EvidenceReference `json:"evidence"`
}
type Normalization struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
type LiteralDimension struct {
	Kind     string              `json:"kind"`
	Raw      string              `json:"raw"`
	Evidence []EvidenceReference `json:"evidence"`
}
type ExtractedField struct {
	Dimensions     []LiteralDimension  `json:"dimensions"`
	SubjectRefs    []string            `json:"subject_refs"`
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Raw            string              `json:"raw"`
	Evidence       []EvidenceReference `json:"evidence"`
	Normalizations []Normalization     `json:"normalizations"`
}
type EventProposal struct {
	ID              string              `json:"id"`
	Type            string              `json:"type"`
	FieldRefs       []string            `json:"field_refs"`
	SubjectRefs     []string            `json:"subject_refs"`
	SectionEvidence []EvidenceReference `json:"section_evidence"`
}
type UnitCoverage struct {
	ViewID      string `json:"view_id"`
	Disposition string `json:"disposition"`
}
type ExtractionIssue struct {
	Code     string   `json:"code"`
	ViewRefs []string `json:"view_refs"`
}
type Extraction struct {
	Labels       []string          `json:"labels"`
	Domain       string            `json:"domain"`
	DocumentType string            `json:"document_type"`
	Subjects     []LocalSubject    `json:"subjects"`
	Fields       []ExtractedField  `json:"fields"`
	Events       []EventProposal   `json:"events"`
	Coverage     []UnitCoverage    `json:"coverage"`
	Issues       []ExtractionIssue `json:"issues"`
}
type ChunkUnderstanding struct {
	Receipt      *CompletionReceipt `json:"receipt,omitempty"`
	InputDigest  string             `json:"input_digest"`
	InvocationID string             `json:"invocation_id"`
	Index        int                `json:"index"`
	Result       Extraction         `json:"result"`
	Digest       string             `json:"digest"`
}

func (r ChunkUnderstanding) digest() string { r.Digest = ""; return digestJSON(r) }

type ScopedLocalReference struct {
	ChunkIndex int    `json:"chunk_index"`
	LocalID    string `json:"local_id"`
}

const NativeHeadingAnchorPolicy = "native-report-heading-anchor/1"
const NativeReportSectionPolicy = "native-report-header-section/1"

// CanonicalEvidence identifies original evidence independently of a chunk view.
// Original view references remain in SectionEvidence and each proposal.
type CanonicalEvidence struct {
	ParseDigest     string `json:"parse_digest"`
	ParserVersion   string `json:"parser_version"`
	ParentSegmentID string `json:"parent_segment_id"`
	ComponentID     string `json:"component_id"`
	ComponentKind   string `json:"component_kind"`
	AlternativeID   string `json:"alternative_id"`
	Origin          string `json:"origin"`
	Start           int    `json:"start"`
	End             int    `json:"end"`
	Quote           string `json:"quote"`
	DerivedValueRef string `json:"derived_value_ref"`
}

func canonicalEvidence(v EvidenceView, r EvidenceReference) CanonicalEvidence {
	return CanonicalEvidence{v.ParseDigest, v.ParserVersion, v.ParentSegmentID, v.ComponentID, v.ComponentKind, v.AlternativeID, v.Origin, r.Start, r.End, r.Quote, r.DerivedValueRef}
}
func (r CanonicalEvidence) componentIdentity() string {
	r.Start = 0
	r.End = 0
	r.Quote = ""
	r.DerivedValueRef = ""
	return digestJSON(r)
}

type MergedEvent struct {
	CanonicalEvidence []CanonicalEvidence    `json:"canonical_evidence"`
	AnchorPolicy      string                 `json:"anchor_policy"`
	ID                string                 `json:"id"`
	Type              string                 `json:"type"`
	SectionEvidence   []EvidenceReference    `json:"section_evidence"`
	FieldRefs         []ScopedLocalReference `json:"field_refs"`
	SubjectRefs       []ScopedLocalReference `json:"subject_refs"`
	Proposals         []ScopedLocalReference `json:"proposals"`
}
type UnderstandingLabel struct {
	Label        string `json:"label"`
	ChunkIndexes []int  `json:"chunk_indexes"`
}
type Understanding struct {
	Labels         []UnderstandingLabel `json:"labels"`
	LinkIssues     []string             `json:"link_issues"`
	Events         []MergedEvent        `json:"events"`
	Version        string               `json:"version"`
	ManifestDigest string               `json:"manifest_digest"`
	ParseDigest    string               `json:"parse_digest"`
	Complete       bool                 `json:"complete"`
	RequiresReview bool                 `json:"requires_review"`
	Status         ParseStatus          `json:"parse_status"`
	Coverage       ParseCoverage        `json:"parse_coverage"`
	Warnings       []ParseWarning       `json:"parse_warnings"`
	Sheets         []SheetEvidence      `json:"sheets"`
	Native         *NativeMetadata      `json:"native"`
	Chunks         []ChunkUnderstanding `json:"chunks"`
	Digest         string               `json:"digest"`
}

// strictJSON rejects duplicate keys, null, excessive depth, malformed UTF-8,
// unknown fields, trailing values and type coercion before semantic validation.
func strictJSON(data []byte, dst any) error {
	if len(data) == 0 || len(data) > MaxChunkOutput || !utf8.Valid(data) || !validJSONScalars(data) {
		return understandingError("understand_schema")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return understandingError("understand_schema")
		}
		t, err := d.Token()
		if err != nil || t == nil {
			return understandingError("understand_schema")
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				keys := map[string]bool{}
				for d.More() {
					k, err := d.Token()
					if err != nil {
						return err
					}
					s, ok := k.(string)
					if !ok || keys[s] {
						return understandingError("understand_schema")
					}
					keys[s] = true
					if err := walk(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for d.More() {
					if err := walk(depth + 1); err != nil {
						return err
					}
				}
			default:
				return understandingError("understand_schema")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := walk(0); err != nil {
		return understandingError("understand_schema")
	}
	if _, err := d.Token(); err != io.EOF {
		return understandingError("understand_schema")
	}
	if !requiredJSONFields(data, reflect.TypeOf(dst).Elem()) {
		return understandingError("understand_schema")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return understandingError("understand_schema")
	}
	return nil
}

func ValidateChunk(p UnderstandingPlan, c UnderstandingChunk, data []byte) (ChunkUnderstanding, error) {
	bad := func(code string) (ChunkUnderstanding, error) { return ChunkUnderstanding{}, understandingError(code) }
	if !validPlan(p) || c.Index < 0 || c.Index >= len(p.Chunks) || c.InputDigest != c.inputDigest() || digestJSON(c) != digestJSON(p.Chunks[c.Index]) {
		return bad("understand_identity")
	}
	var e Extraction
	if err := strictJSON(data, &e); err != nil {
		return ChunkUnderstanding{}, err
	}
	if !boundedLabel(e.DocumentType) || !boundedLabel(e.Domain) || e.Subjects == nil || e.Fields == nil || e.Events == nil || e.Coverage == nil || e.Issues == nil || len(e.Subjects) > 128 || len(e.Fields) > 256 || len(e.Events) > 128 || len(e.Issues) > 128 {
		return bad("understand_schema")
	}
	if e.Labels == nil || len(e.Labels) > 16 {
		return bad("understand_schema")
	}
	labelSeen := map[string]bool{}
	for _, label := range e.Labels {
		if !boundedLabel(label) || strings.TrimSpace(label) == "" || labelSeen[label] {
			return bad("understand_schema")
		}
		labelSeen[label] = true
	}
	switch e.Domain {
	case "health", "education", "finance", "property", "other", "unknown":
	default:
		return bad("understand_schema")
	}
	views := map[string]EvidenceView{}
	owned := map[string]bool{}
	parents := map[string]string{}
	for _, ch := range p.Chunks {
		for _, v := range ch.Owned {
			parents[v.ComponentID] += v.Text
		}
	}
	for _, v := range c.Evidence() {
		views[v.ViewID] = v
	}
	for _, v := range c.Owned {
		owned[v.ViewID] = true
	}
	validRefs := func(refs []EvidenceReference, allowContext bool) bool {
		if len(refs) == 0 || len(refs) > 32 {
			return false
		}
		hasOwned := false
		seen := map[string]bool{}
		for _, r := range refs {
			v, ok := views[r.ViewID]
			if !ok {
				return false
			}
			if owned[r.ViewID] {
				hasOwned = true
			}
			key := digestJSON(r)
			if seen[key] {
				return false
			}
			seen[key] = true
			if r.DerivedValueRef != "" {
				if v.ComponentKind != "derived_display" || v.Derived == nil || r.DerivedValueRef != v.Derived.Ref || r.Quote != "" || r.Start != 0 || r.End != 0 {
					return false
				}
				continue
			}
			if v.ComponentKind == "derived_display" || r.Start < v.Start || r.End < r.Start || r.End > v.End || r.Start == r.End && parents[v.ComponentID] != "" {
				return false
			}
			rr := []rune(v.Text)
			if string(rr[r.Start-v.Start:r.End-v.Start]) != r.Quote {
				return false
			}
		}
		return hasOwned || allowContext
	}
	covered := map[string]string{}
	for _, u := range e.Coverage {
		if !owned[u.ViewID] || covered[u.ViewID] != "" {
			return bad("understand_coverage")
		}
		switch u.Disposition {
		case "extracted", "irrelevant", "insufficient":
		default:
			return bad("understand_coverage")
		}
		covered[u.ViewID] = u.Disposition
	}
	if len(covered) != len(owned) {
		return bad("understand_coverage")
	}
	used := map[string]bool{}
	mark := func(refs []EvidenceReference) {
		for _, r := range refs {
			if owned[r.ViewID] {
				used[r.ViewID] = true
			}
		}
	}
	subjects := map[string]bool{}
	fields := map[string]bool{}
	events := map[string]bool{}
	for _, s := range e.Subjects {
		if !boundedLabel(s.ID) || !boundedLabel(s.Name) || !boundedLabel(s.Role) || subjects[s.ID] || !validRefs(s.Evidence, true) {
			return bad("understand_reference")
		}
		literal := false
		for _, r := range s.Evidence {
			if strings.Contains(r.Quote, s.Name) {
				literal = true
			}
		}
		if !literal {
			return bad("understand_reference")
		}
		if (s.Name == "媽媽" || s.Name == "妈妈" || s.Name == "爸爸" || s.Name == "母亲" || s.Name == "父亲") && !s.Ambiguous {
			return bad("understand_ambiguity")
		}
		subjects[s.ID] = true
		mark(s.Evidence)
	}
	for _, f := range e.Fields {
		if f.Dimensions == nil || len(f.Dimensions) > 2 {
			return bad("understand_schema")
		}
		dimensionKinds := map[string]bool{}
		for _, d := range f.Dimensions {
			if (d.Kind != "unit" && d.Kind != "currency") || dimensionKinds[d.Kind] || !boundedLabel(d.Raw) || !validRefs(d.Evidence, true) {
				return bad("understand_reference")
			}
			dimensionKinds[d.Kind] = true
			match := false
			for _, r := range d.Evidence {
				if r.DerivedValueRef == "" && r.Quote == d.Raw {
					match = true
				}
			}
			if !match {
				return bad("understand_value")
			}
			mark(d.Evidence)
		}
		if f.SubjectRefs == nil || len(f.SubjectRefs) > 128 {
			return bad("understand_reference")
		}
		seenSubjects := map[string]bool{}
		for _, id := range f.SubjectRefs {
			if !subjects[id] || seenSubjects[id] {
				return bad("understand_reference")
			}
			seenSubjects[id] = true
		}
		if !boundedLabel(f.ID) || !boundedLabel(f.Name) || len(f.Raw) > 8192 || fields[f.ID] || f.Normalizations == nil || len(f.Normalizations) > 4 || !validRefs(f.Evidence, false) {
			return bad("understand_reference")
		}
		rawMatch := false
		derivedRaw := false
		for _, r := range f.Evidence {
			v := views[r.ViewID]
			if r.DerivedValueRef != "" {
				if v.Derived.Value == f.Raw {
					derivedRaw = true
					rawMatch = owned[r.ViewID] || rawMatch
				}
			} else if r.Quote == f.Raw {
				rawMatch = owned[r.ViewID] || rawMatch
			}
			if v.ComponentKind == "formula_text" || v.ComponentKind == "format_code" {
				return bad("understand_value")
			}
		}
		if _, numeric := decimalRat(f.Raw); numeric && !derivedRaw && !validateNormalization(Normalization{Kind: "decimal", Value: f.Raw}, f, views, parents) {
			return bad("understand_value")
		}
		if !rawMatch {
			return bad("understand_value")
		}
		for _, n := range f.Normalizations {
			if !validateNormalization(n, f, views, parents) {
				return bad("understand_normalization")
			}
		}
		fields[f.ID] = true
		mark(f.Evidence)
	}
	for _, ev := range e.Events {
		if !boundedLabel(ev.ID) || !boundedLabel(ev.Type) || events[ev.ID] || ev.FieldRefs == nil || ev.SubjectRefs == nil || len(ev.FieldRefs) > 256 || len(ev.SubjectRefs) > 128 || !validRefs(ev.SectionEvidence, len(ev.FieldRefs) > 0) {
			return bad("understand_reference")
		}
		events[ev.ID] = true
		seen := map[string]bool{}
		for _, id := range ev.FieldRefs {
			if !fields[id] || seen["f"+id] {
				return bad("understand_reference")
			}
			seen["f"+id] = true
		}
		for _, id := range ev.SubjectRefs {
			if !subjects[id] || seen["s"+id] {
				return bad("understand_reference")
			}
			seen["s"+id] = true
		}
		for _, f := range e.Fields {
			for _, id := range ev.FieldRefs {
				if f.ID == id {
					for _, subject := range f.SubjectRefs {
						if !seen["s"+subject] {
							return bad("understand_reference")
						}
					}
				}
			}
		}
		mark(ev.SectionEvidence)
	}
	for _, is := range e.Issues {
		if !boundedLabel(is.Code) || is.ViewRefs == nil || len(is.ViewRefs) > 128 {
			return bad("understand_schema")
		}
		for _, ref := range is.ViewRefs {
			if !owned[ref] {
				return bad("understand_reference")
			}
		}
	}
	for id, disposition := range covered {
		if used[id] && disposition != "extracted" || !used[id] && disposition == "extracted" {
			return bad("understand_coverage")
		}
	}
	r := ChunkUnderstanding{InputDigest: c.InputDigest, InvocationID: c.InvocationID, Index: c.Index, Result: e}
	r.Digest = r.digest()
	return r, nil
}

var decimalToken = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]{1,4})?$`)

func decimalRat(s string) (*big.Rat, bool) {
	if len(s) > 512 || !decimalToken.MatchString(s) {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	return r, ok
}
func numericRune(r rune) bool { return unicode.IsDigit(r) || strings.ContainsRune(".+-eE", r) }
func validDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	d, err := time.Parse("2006-01-02", s)
	return err == nil && d.Format("2006-01-02") == s
}
func validateNormalization(n Normalization, f ExtractedField, views map[string]EvidenceView, parents map[string]string) bool {
	if len(n.Value) > 512 {
		return false
	}
	for _, r := range f.Evidence {
		v := views[r.ViewID]
		if n.Kind == "date" {
			if !validDate(n.Value) || n.Value != f.Raw {
				return false
			}
			if r.DerivedValueRef != "" && v.Derived.Value == n.Value {
				return true
			}
			if r.Quote == n.Value {
				return true
			}
			continue
		}
		if n.Kind == "date_partial" {
			if n.Value == f.Raw && r.Quote == f.Raw && partialDate.MatchString(n.Value) {
				return true
			}
			continue
		}
		if n.Kind != "decimal" && n.Kind != "percentage" {
			return false
		}
		if r.DerivedValueRef != "" || v.ComponentKind == "derived_display" || v.ComponentKind == "formula_text" || v.ComponentKind == "format_code" {
			continue
		}
		// Numeric strings in shared-string or boolean cells are not numeric cells.
		if v.ComponentKind == "cell_text" && v.Structure.CellType != "" && v.Structure.CellType != "n" {
			continue
		}
		if r.Quote != f.Raw {
			continue
		}
		source, ok := decimalRat(r.Quote)
		if !ok {
			continue
		}
		value, ok := decimalRat(n.Value)
		if !ok {
			continue
		}
		full := []rune(parents[v.ComponentID])
		if r.Start > 0 && numericRune(full[r.Start-1]) || r.End < len(full) && numericRune(full[r.End]) {
			continue
		}
		if v.ComponentKind == "numeric_raw" || v.ComponentKind == "historical_cache" {
			if r.Start != 0 || r.End != len(full) {
				continue
			}
		}
		if n.Kind == "percentage" {
			if v.ComponentKind != "numeric_raw" && v.ComponentKind != "historical_cache" {
				continue
			}
			format := parents[v.ParentSegmentID+"/format_code"]
			if format != "0%" && format != "0.00%" {
				continue
			}
			source.Mul(source, big.NewRat(100, 1))
		}
		if source.Cmp(value) == 0 {
			return true
		}
	}
	return false
}

// MergeChunks deliberately retains extraction-local events and conflicting
// observations. Equal names/values are never identity evidence. Chunk order is
// canonical; replayed chunks are rejected rather than counted twice.
func MergeChunks(p UnderstandingPlan, results []ChunkUnderstanding) (Understanding, error) {
	if !validPlan(p) || len(results) != len(p.Chunks) {
		return Understanding{}, understandingError("understand_manifest")
	}
	out := Understanding{Version: UnderstandingVersion, ManifestDigest: p.ManifestDigest, ParseDigest: p.ParseDigest, Complete: !p.StructuralContextLimited && p.Coverage.Complete && (p.Status == ParseParsed || p.Status == ParseEmpty), Status: p.Status, Coverage: p.Coverage, Warnings: p.Warnings, Sheets: p.Sheets, Native: p.Native, Chunks: []ChunkUnderstanding{}}
	seen := map[int]bool{}
	for _, r := range results {
		if r.Index < 0 || r.Index >= len(p.Chunks) || seen[r.Index] || r.Digest != r.digest() {
			return Understanding{}, understandingError("understand_manifest")
		}
		seen[r.Index] = true
		b, _ := json.Marshal(r.Result)
		checked, err := ValidateChunk(p, p.Chunks[r.Index], b)
		if !validReceiptForChunk(r.Receipt, p.Chunks[r.Index]) {
			return Understanding{}, understandingError("understand_manifest")
		}
		checked.Receipt = r.Receipt
		checked.Digest = checked.digest()
		if err != nil || checked.Digest != r.Digest {
			return Understanding{}, understandingError("understand_manifest")
		}
		checked.Result = deduplicateExtraction(checked.Result)
		checked.Digest = checked.digest()
		out.Chunks = append(out.Chunks, checked)
		for _, u := range r.Result.Coverage {
			if u.Disposition == "insufficient" {
				out.Complete = false
				out.RequiresReview = true
			}
		}
		if len(r.Result.Issues) > 0 {
			out.RequiresReview = true
		}
	}
	for _, c := range p.Chunks {
		for _, v := range c.Owned {
			if v.RequiresReview || v.VisualState == "native_unconfirmed" {
				out.RequiresReview = true
			}
		}
	}
	if !out.Complete {
		out.RequiresReview = true
	}
	sort.Slice(out.Chunks, func(i, j int) bool { return out.Chunks[i].Index < out.Chunks[j].Index })
	out.Labels = []UnderstandingLabel{}
	labelChunks := map[string][]int{}
	for _, c := range out.Chunks {
		for _, label := range c.Result.Labels {
			labelChunks[label] = append(labelChunks[label], c.Index)
		}
	}
	for label, indexes := range labelChunks {
		out.Labels = append(out.Labels, UnderstandingLabel{label, indexes})
	}
	sort.Slice(out.Labels, func(i, j int) bool { return out.Labels[i].Label < out.Labels[j].Label })
	out.Events = mergeEventAnchors(p, out.Chunks)
	out.LinkIssues = []string{}
	if overlappingReportAnchors(out.Events) {
		out.RequiresReview = true
		out.LinkIssues = append(out.LinkIssues, "overlapping_report_anchors")
	}
	out.Digest = digestJSON(out)
	return out, nil
}

func requiredJSONFields(data json.RawMessage, t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		return requiredJSONFields(data, t.Elem())
	}
	switch t.Kind() {
	case reflect.Struct:
		var m map[string]json.RawMessage
		if json.Unmarshal(data, &m) != nil || m == nil {
			return false
		}
		// encoding/json accepts case-insensitive aliases even with
		// DisallowUnknownFields. Enforce exact tags before decoding instead.
		allowed := map[string]bool{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if tag == "" {
				tag = f.Name
			}
			allowed[tag] = true
			b, ok := m[tag]
			if !ok {
				if strings.Contains(f.Tag.Get("json"), "omitempty") {
					continue
				}
				return false
			}
			if !requiredJSONFields(b, f.Type) {
				return false
			}
		}
		for key := range m {
			if !allowed[key] {
				return false
			}
		}
	case reflect.Slice:
		var a []json.RawMessage
		if json.Unmarshal(data, &a) != nil || a == nil {
			return false
		}
		for _, b := range a {
			if !requiredJSONFields(b, t.Elem()) {
				return false
			}
		}
	}
	return true
}

// Deduplicate only exact semantic copies anchored to the same evidence. Nothing
// is joined by a person's name or a measurement's value. Different sections and
// competing interpretations remain separate.
func deduplicateExtraction(e Extraction) Extraction {
	subjectMap := map[string]string{}
	subjectKeys := map[string]string{}
	subjects := []LocalSubject{}
	for _, s := range e.Subjects {
		id := s.ID
		s.ID = ""
		key := digestJSON(s)
		if existing, ok := subjectKeys[key]; ok {
			subjectMap[id] = existing
			continue
		}
		s.ID = id
		subjectMap[id] = id
		subjectKeys[key] = id
		subjects = append(subjects, s)
	}
	e.Subjects = subjects
	remap := func(ids []string, m map[string]string) []string {
		out := []string{}
		seen := map[string]bool{}
		for _, id := range ids {
			v := m[id]
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		return out
	}
	fieldMap := map[string]string{}
	fieldKeys := map[string]string{}
	fields := []ExtractedField{}
	for _, f := range e.Fields {
		id := f.ID
		f.SubjectRefs = remap(f.SubjectRefs, subjectMap)
		f.ID = ""
		key := digestJSON(f)
		if existing, ok := fieldKeys[key]; ok {
			fieldMap[id] = existing
			continue
		}
		f.ID = id
		fieldMap[id] = id
		fieldKeys[key] = id
		fields = append(fields, f)
	}
	e.Fields = fields
	eventKeys := map[string]bool{}
	events := []EventProposal{}
	for _, ev := range e.Events {
		id := ev.ID
		ev.SubjectRefs = remap(ev.SubjectRefs, subjectMap)
		ev.FieldRefs = remap(ev.FieldRefs, fieldMap)
		ev.ID = ""
		key := digestJSON(ev)
		if eventKeys[key] {
			continue
		}
		eventKeys[key] = true
		ev.ID = id
		events = append(events, ev)
	}
	e.Events = events
	return e
}

func validJSONScalars(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		n, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

var partialDate = regexp.MustCompile(`^(?:[0-9]{4}-(?:0[1-9]|1[0-2])|(?:0[1-9]|1[0-2])-(?:0[1-9]|[12][0-9]|3[01])|[0-9]{2}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12][0-9]|3[01])|(?:[1-9]|1[0-2])月(?:[1-9]|[12][0-9]|3[01])日)$`)

// A shared, exact section anchor links report fragments. Row anchors include
// their original view/range, so repeated equal values are never identity keys.
func mergeEventAnchors(p UnderstandingPlan, chunks []ChunkUnderstanding) []MergedEvent {
	out := []MergedEvent{}
	indices := map[string]int{}
	headings := nativeReportHeadings(p)
	sections := nativeReportSections(p, headings)
	for _, c := range chunks {
		for _, ev := range c.Result.Events {
			refs := append([]EvidenceReference{}, ev.SectionEvidence...)
			// Native heading observations are atomic parser sections. A valid prefix
			// citation and a valid full bilingual heading cite the same section. Expand
			// only report anchors at the start of a native alternative view; never field
			// quotes, arbitrary body ranges, row events, or invalid evidence.
			policy := "exact-parent-evidence/2"
			views := map[string]EvidenceView{}
			for _, v := range p.Chunks[c.Index].Evidence() {
				views[v.ViewID] = v
			}
			if ev.Type == "report" {
				sectionDates := map[string]EvidenceReference{}
				for i, r := range refs {
					v := views[r.ViewID]
					if headings[v.ViewID] && r.DerivedValueRef == "" && r.Start == v.Start && r.End > r.Start {
						policy = NativeHeadingAnchorPolicy
						refs[i].End = v.End
						refs[i].Quote = v.Text
						if date, ok := sections[v.ViewID]; ok {
							if _, visible := views[date.ViewID]; visible {
								sectionDates[date.ViewID] = date
							}
						}
					}
				}
				for id, date := range sectionDates {
					policy = NativeReportSectionPolicy
					// Normalize only a verified complete date citation from this explicit
					// date component; preserve partial dates and all additional evidence.
					for i, r := range refs {
						if r.ViewID == id && r.DerivedValueRef == "" && r.Start <= date.Start && r.End >= date.End {
							refs[i] = date
						}
					}
					refs = append(refs, date)
				}
			}

			sort.Slice(refs, func(i, j int) bool { return digestJSON(refs[i]) < digestJSON(refs[j]) })
			canonical := []CanonicalEvidence{}
			seen := map[string]bool{}
			for _, r := range refs {
				value := canonicalEvidence(views[r.ViewID], r)
				key := digestJSON(value)
				if !seen[key] {
					canonical = append(canonical, value)
					seen[key] = true
				}
			}
			sort.Slice(canonical, func(i, j int) bool { return digestJSON(canonical[i]) < digestJSON(canonical[j]) })
			key := digestJSON(struct {
				Type     string
				Evidence []CanonicalEvidence
			}{ev.Type, canonical})
			index, ok := indices[key]
			if !ok {
				index = len(out)
				indices[key] = index
				out = append(out, MergedEvent{CanonicalEvidence: canonical, AnchorPolicy: policy, ID: UnderstandingVersion + "/event/" + key, Type: ev.Type, SectionEvidence: refs, FieldRefs: []ScopedLocalReference{}, SubjectRefs: []ScopedLocalReference{}, Proposals: []ScopedLocalReference{}})
			}
			m := &out[index]
			m.Proposals = append(m.Proposals, ScopedLocalReference{c.Index, ev.ID})
			for _, id := range ev.FieldRefs {
				m.FieldRefs = append(m.FieldRefs, ScopedLocalReference{c.Index, id})
			}
			for _, id := range ev.SubjectRefs {
				m.SubjectRefs = append(m.SubjectRefs, ScopedLocalReference{c.Index, id})
			}
		}
	}
	return out
}

// Heading identity is deliberately narrow: a short, single-line first native
// component on a page, one alternative, no inherited uncertainty, and an
// explicit report-title marker. General body prefixes and conflict groups are
// never expanded. Date/other section evidence remains part of the exact key.
func nativeReportHeadings(p UnderstandingPlan) map[string]bool {
	out := map[string]bool{}
	if p.Native == nil {
		return out
	}
	first := map[int]string{}
	counts := map[string]int{}
	for _, c := range p.Chunks {
		for _, v := range c.Owned {
			counts[v.ParentSegmentID]++
			if l := v.Structure.Locator.Page; l != nil && first[l.Page] == "" {
				first[l.Page] = v.ViewID
			}
		}
	}
	for _, c := range p.Chunks {
		for _, v := range c.Owned {
			l := v.Structure.Locator.Page
			if l == nil || first[l.Page] != v.ViewID || counts[v.ParentSegmentID] != 1 || v.AlternativeID == "" || v.ComponentKind != "body" || v.RequiresReview || v.VisualState == "native_unconfirmed" || v.Start != 0 || v.End > 256 || strings.ContainsAny(v.Text, "\r\n") || l.Rect[1] > 0.25 {
				continue
			}
			lower := strings.ToLower(v.Text)
			if strings.Contains(lower, "日期") || strings.Contains(lower, "date") {
				continue
			}
			if reportTitleMarker.MatchString(v.Text) {
				out[v.ViewID] = true
			}
		}
	}
	return out
}

// A host section is available only when the whole page contains exactly one
// short native report title and one explicit report-date line, both atomic and
// certain, with the date in that title's header region. Counting all page views
// (not only nativeReportHeadings' first component) prevents guessing through
// multiple reports, repeated titles, alternative dates, or ambiguous headers.
func nativeReportSections(p UnderstandingPlan, headings map[string]bool) map[string]EvidenceReference {
	out := map[string]EvidenceReference{}
	if p.Native == nil {
		return out
	}
	titles, dates := map[int][]EvidenceView{}, map[int][]EvidenceView{}
	count := map[string]int{}
	for _, c := range p.Chunks {
		for _, v := range c.Owned {
			count[v.ParentSegmentID]++
			if l := v.Structure.Locator.Page; l != nil {
				if reportDateLabel.MatchString(v.Text) {
					dates[l.Page] = append(dates[l.Page], v)
				} else if reportTitleMarker.MatchString(v.Text) {
					titles[l.Page] = append(titles[l.Page], v)
				}
			}
		}
	}
	for page, ts := range titles {
		ds := dates[page]
		if len(ts) != 1 || len(ds) != 1 {
			continue
		}
		title, date := ts[0], ds[0]
		if !headings[title.ViewID] || count[date.ParentSegmentID] != 1 || date.AlternativeID == "" || date.ComponentKind != "body" || date.RequiresReview || date.VisualState == "native_unconfirmed" || date.Start != 0 || date.End > 128 || strings.ContainsAny(date.Text, "\r\n") {
			continue
		}
		tl, dl := title.Structure.Locator.Page, date.Structure.Locator.Page
		if dl.Rect[1] <= tl.Rect[1] || dl.Rect[1] > 0.35 || dl.Rect[1]+dl.Rect[3] > 0.4 {
			continue
		}
		loc := reportDateLine.FindStringSubmatchIndex(date.Text)
		if len(loc) != 4 || !validDate(date.Text[loc[2]:loc[3]]) {
			continue
		}
		out[title.ViewID] = EvidenceReference{ViewID: date.ViewID, Start: utf8.RuneCountInString(date.Text[:loc[2]]), End: utf8.RuneCountInString(date.Text[:loc[3]]), Quote: date.Text[loc[2]:loc[3]]}
	}
	return out
}

var reportDateLabel = regexp.MustCompile(`(?i)^\s*(?:报告日期|報告日期|report\s+date)(?:\s|[:：]|$)`)
var reportDateLine = regexp.MustCompile(`(?i)^\s*(?:报告日期|報告日期|report\s+date)\s*[:：]\s*([0-9]{4}-[0-9]{2}-[0-9]{2})\s*$`)

var reportTitleMarker = regexp.MustCompile(`(?i)(?:报告|報告|report)(?:\s|$)`)

// O(R log R) for R canonical ranges, rather than pairwise events/anchors.
// Only overlap between different merged events is significant.
func overlappingReportAnchors(events []MergedEvent) bool {
	type interval struct{ start, end, event int }
	groups := map[string][]interval{}
	for i, e := range events {
		if e.Type != "report" {
			continue
		}
		for _, r := range e.CanonicalEvidence {
			if r.End > r.Start {
				key := r.componentIdentity()
				groups[key] = append(groups[key], interval{r.Start, r.End, i})
			}
		}
	}
	for _, ranges := range groups {
		sort.Slice(ranges, func(i, j int) bool {
			if ranges[i].start == ranges[j].start {
				return ranges[i].end < ranges[j].end
			}
			return ranges[i].start < ranges[j].start
		})
		first, second := interval{event: -1}, interval{event: -1}
		for _, r := range ranges {
			if first.event >= 0 && first.event != r.event && first.end > r.start || second.event >= 0 && second.event != r.event && second.end > r.start {
				return true
			}
			switch {
			case first.event == r.event:
				if r.end > first.end {
					first = r
				}
			case second.event == r.event:
				if r.end > second.end {
					second = r
				}
				if second.end > first.end {
					first, second = second, first
				}
			case first.event < 0 || r.end > first.end:
				second, first = first, r
			case second.event < 0 || r.end > second.end:
				second = r
			}
		}
	}
	return false
}
