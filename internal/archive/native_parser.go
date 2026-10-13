package archive

// The native adapter is a pure extraction boundary. It accepts a fresh descriptor
// returned by OpenVerified, borrowed exclusively for the duration of Parse. The
// caller keeps ownership and closes it after Parse returns, including on failure.
// It never opens File.Name, closes the caller's handle, imports, or authorizes.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const nativeProtocol = 1

// NativeParserVersion identifies native extraction evidence and protocol rules.
const NativeParserVersion = "archive-native/1"
const nativeStdoutLimit = 8 << 20
const nativeTextLimit = 2 << 20

var nativeSlots = make(chan struct{}, 1)

// NativeSource is an authorized immutable original opened by the host.
type NativeSource struct {
	File   *os.File
	SHA256 string
}

// NativeParser uses a trusted HelperPath from administrator configuration, never document input. No
// arguments/environment are accepted from a document. Zero Timeout is 120s;
// callers may shorten it. The helper is only supported on macOS.
type NativeParser struct {
	HelperPath string
	Timeout    time.Duration
}

// NativeParserError reports a stable native-parser failure code.
type NativeParserError struct {
	Code  string
	cause error
}

func (e *NativeParserError) Error() string       { return "archive: " + e.Code }
func (e *NativeParserError) Unwrap() error       { return e.cause }
func nativeError(code string, cause error) error { return &NativeParserError{code, cause} }

// Rect is normalized display-top-left [x,y,width,height]. No reading order or
// table relationship is implied. PageSpace/Transform use PDF points -> pixels.
type Rect [4]float64

// PageLocation locates visual evidence on a page.
type PageLocation struct {
	Page int  `json:"page"`
	Rect Rect `json:"rect"`
}

// NativeEngine identifies the native helper and recognition settings.
type NativeEngine struct {
	Helper             string   `json:"helper"`
	Build              string   `json:"build"`
	OSBuild            string   `json:"os_build"`
	VisionRevision     int      `json:"vision_revision"`
	Languages          []string `json:"languages"`
	Recognition        string   `json:"recognition"`
	LanguageCorrection bool     `json:"language_correction"`
}

// PageGeometry records the rendering geometry and crop transform for a page.
type PageGeometry struct {
	Page           int         `json:"page"`
	Width          int         `json:"width"`
	Height         int         `json:"height"`
	Orientation    int         `json:"orientation"`
	Rotation       int         `json:"rotation"`
	EffectiveCrop  *Rect       `json:"effective_crop"`
	Transform      *[6]float64 `json:"transform"`
	OCRComplete    bool        `json:"ocr_complete"`
	NativeComplete bool        `json:"native_complete"`
}

// VisualAlternative retains a competing recognition result with its origin.
type VisualAlternative struct {
	Origin     string  `json:"origin"`
	Text       string  `json:"text"`
	Rect       Rect    `json:"rect"`
	PageSpace  *Rect   `json:"page_space"`
	Clipped    bool    `json:"clipped"`
	Confidence float64 `json:"confidence"`
}

// VisualEvidence records recognition confidence, alternatives and review requirements.
type VisualEvidence struct {
	State          string              `json:"state"`
	RequiresReview bool                `json:"requires_review"`
	Alternatives   []VisualAlternative `json:"alternatives"`
}

// NativeMetadata summarizes the native engine, pages and excluded features.
type NativeMetadata struct {
	Engine         NativeEngine   `json:"engine"`
	Pages          []PageGeometry `json:"pages"`
	TotalPages     int            `json:"total_pages"`
	ProcessedPages int            `json:"processed_pages"`
	Annotations    string         `json:"annotations"`
	Forms          string         `json:"forms"`
	Encrypted      bool           `json:"encrypted"`
	AllowsCopying  bool           `json:"allows_copying"`
}
type nativeObservation struct {
	Page       int     `json:"page"`
	Text       string  `json:"text"`
	Origin     string  `json:"origin"`
	Rect       Rect    `json:"rect"`
	PageSpace  *Rect   `json:"page_space"`
	Clipped    bool    `json:"clipped"`
	Confidence float64 `json:"confidence"`
}
type nativeCoverage struct {
	Complete       bool   `json:"complete"`
	TotalPages     int    `json:"total_pages"`
	ProcessedPages int    `json:"processed_pages"`
	Annotations    string `json:"annotations"`
	Forms          string `json:"forms"`
}
type nativeCounts struct {
	SourceBytes int64 `json:"source_bytes"`
	TextBytes   int64 `json:"text_bytes"`
	Segments    int   `json:"segments"`
}
type nativeWire struct {
	Protocol      int                 `json:"protocol"`
	Format        string              `json:"format"`
	Status        ParseStatus         `json:"status"`
	Code          string              `json:"code"`
	Engine        NativeEngine        `json:"engine"`
	Pages         []PageGeometry      `json:"pages"`
	Segments      []nativeObservation `json:"segments"`
	Counts        nativeCounts        `json:"counts"`
	Coverage      nativeCoverage      `json:"coverage"`
	Encrypted     bool                `json:"encrypted"`
	AllowsCopying bool                `json:"allows_copying"`
}

// Parse extracts bounded native evidence from an already opened original.
func (p NativeParser) Parse(ctx context.Context, s NativeSource) (ParseResult, error) {
	if runtime.GOOS != "darwin" {
		return ParseResult{}, nativeError("parser_unavailable", nil)
	}
	return p.parseProcess(ctx, s)
}
func (p NativeParser) parseProcess(ctx context.Context, s NativeSource) (ParseResult, error) {
	bad := func(code string, cause error) (ParseResult, error) { return ParseResult{}, nativeError(code, cause) }
	if err := ctx.Err(); err != nil {
		return bad("parser_canceled", err)
	}
	hash, err := hex.DecodeString(s.SHA256)
	if s.File == nil || err != nil || len(hash) != 32 || s.SHA256 != strings.ToLower(s.SHA256) {
		return bad("parser_invalid_source", nil)
	}
	info, err := s.File.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return bad("parser_invalid_source", nil)
	}
	if info.Size() > 32<<20 {
		return ParseResult{ParserVersion: NativeParserVersion, Status: ParseLimitExceeded, Warnings: []ParseWarning{{Code: "source_bytes", Message: "source_bytes"}}}, nil
	}
	if !filepath.IsAbs(p.HelperPath) {
		return bad("parser_unavailable", nil)
	}
	hi, err := os.Lstat(p.HelperPath)
	if err != nil || !hi.Mode().IsRegular() || hi.Mode().Perm()&0111 == 0 {
		return bad("parser_unavailable", nil)
	}
	timeout := p.Timeout
	if timeout <= 0 || timeout > 120*time.Second {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case nativeSlots <- struct{}{}:
	case <-ctx.Done():
		return bad("parser_canceled", ctx.Err())
	}
	defer func() { <-nativeSlots }()
	cmd := exec.CommandContext(ctx, p.HelperPath, "--protocol=1") // #nosec G204 -- HelperPath is trusted host configuration, checked above as an absolute regular executable; arguments are fixed and the source travels only by FD.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8"}
	cmd.Stdin = strings.NewReader("{\"protocol\":1}\n")
	cmd.ExtraFiles = []*os.File{s.File}
	cmd.WaitDelay = 500 * time.Millisecond
	configureNativeProcess(cmd)
	stdout := &nativeBoundedBuffer{limit: nativeStdoutLimit, cancel: cancel}
	stderr := &nativeBoundedBuffer{limit: 64 << 10, cancel: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err = cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return bad("parser_canceled", ctx.Err())
		}
		return bad("parser_unavailable", nil)
	}
	err = cmd.Wait() // Exactly once; slot held until child and managed pipe writers are reaped.
	finishNativeProcess(cmd)
	if stdout.exceeded || stderr.exceeded {
		return bad("parser_output_limit", nil)
	}
	if ctx.Err() != nil {
		code := "parser_canceled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "parser_timeout"
		}
		return bad(code, ctx.Err())
	}
	if err != nil {
		return bad("parser_failed", nil)
	}
	w, err := decodeNative(stdout.data.Bytes(), info.Size())
	if err != nil {
		return bad("parser_protocol", nil)
	}
	if w.Code == "parser_unavailable" {
		return bad("parser_unavailable", nil)
	}
	r, err := nativeResult(ctx, w, s.SHA256)
	if err != nil {
		return bad("parser_canceled", err)
	}
	return r, nil
}

type nativeBoundedBuffer struct {
	mu       sync.Mutex
	data     bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *nativeBoundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	room := b.limit - b.data.Len()
	if len(p) > room {
		b.data.Write(p[:room])
		b.exceeded = true
		b.cancel()
	} else {
		b.data.Write(p)
	}
	return n, nil
}

// Token validation rejects duplicate fields (encoding/json otherwise selects the
// last value). Typed decoding separately rejects unknown fields and wrong types.
func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 24 {
		return errors.New("depth")
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	v, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch v {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := k.(string)
			if !ok || seen[s] {
				return errors.New("field")
			}
			seen[s] = true
			if err = uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("delimiter")
	}
	_, err = d.Token()
	return err
}

// Every wire field is required. Null is allowed only for pointer fields, and
// fixed coordinate arrays must have exactly their documented number of values.
func nativeJSONShape(b []byte, t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(b, []byte("null")) {
			return true
		}
		return nativeJSONShape(b, t.Elem())
	}
	if bytes.Equal(b, []byte("null")) {
		return false
	}
	switch t.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(b, &fields) != nil || len(fields) != t.NumField() {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			v, ok := fields[f.Tag.Get("json")]
			if !ok || !nativeJSONShape(v, f.Type) {
				return false
			}
		}
	case reflect.Array, reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(b, &values) != nil {
			return false
		}
		if t.Kind() == reflect.Array && len(values) != t.Len() {
			return false
		}
		for _, v := range values {
			if !nativeJSONShape(v, t.Elem()) {
				return false
			}
		}
	}
	return true
}
func validRect(r Rect) bool {
	for _, x := range r {
		if math.IsNaN(x) || math.IsInf(x, 0) || x < 0 || x > 1 {
			return false
		}
	}
	return r[2] > 0 && r[3] > 0 && r[0]+r[2] <= 1.00000001 && r[1]+r[3] <= 1.00000001
}
func finiteBox(r Rect) bool {
	for _, x := range r {
		if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) > 1e9 {
			return false
		}
	}
	return r[2] > 0 && r[3] > 0
}

// encoding/json replaces isolated UTF-16 escapes with U+FFFD. The native
// boundary must reject corrupt strings rather than invent replacement evidence.
func validJSONUnicode(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		if i+4 >= len(b) {
			return false
		}
		v, err := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if v >= 0xdc00 && v <= 0xdfff {
			return false
		}
		if v >= 0xd800 && v <= 0xdbff {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			v, err = strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
			if err != nil || v < 0xdc00 || v > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
func decodeNative(b []byte, size int64) (nativeWire, error) {
	var w nativeWire
	bad := func() (nativeWire, error) { return w, errors.New("protocol") }
	if len(b) > nativeStdoutLimit || !utf8.Valid(b) || !validJSONUnicode(b) {
		return bad()
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if uniqueJSON(d, 0) != nil {
		return bad()
	}
	if _, err := d.Token(); err != io.EOF {
		return bad()
	}
	if !nativeJSONShape(b, reflect.TypeOf(w)) {
		return bad()
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&w) != nil || w.Protocol != 1 {
		return bad()
	}
	if w.Engine.Helper != "atrium-native/1" || len(w.Engine.Build) != 64 || w.Engine.OSBuild == "" || len(w.Engine.OSBuild) > 128 || w.Engine.VisionRevision != 3 || w.Engine.Recognition != "accurate" || w.Engine.LanguageCorrection || len(w.Engine.Languages) != 2 || w.Engine.Languages[0] != "zh-Hans" || w.Engine.Languages[1] != "en-US" {
		return bad()
	}
	if _, err := hex.DecodeString(w.Engine.Build); err != nil {
		return bad()
	}
	allowedCodes := map[string]bool{"": true, "no_text_detected": true, "invalid_input": true, "unsupported_image": true, "multiframe_image": true, "source_bytes": true, "image_dimensions": true, "jpeg_resources": true, "webp_resources": true, "unsupported_jpeg_precision": true, "image_integrity_unverified": true, "page_limit": true, "text_limit": true, "segment_limit": true, "page_failed": true, "locked_pdf": true, "parser_unavailable": true, "annotations_forms_excluded": true}
	if !allowedCodes[w.Code] || (w.Counts.SourceBytes != size && (w.Code != "parser_unavailable" || w.Counts.SourceBytes != 0)) || w.Counts.Segments != len(w.Segments) || len(w.Segments) > 20000 || w.Counts.TextBytes > nativeTextLimit || w.Counts.TextBytes < 0 || w.Coverage.TotalPages < 0 || w.Coverage.TotalPages > 2147483647 || w.Coverage.ProcessedPages < 0 || w.Coverage.ProcessedPages > w.Coverage.TotalPages || len(w.Pages) > 100 {
		return bad()
	}
	if w.Coverage.TotalPages > 100 && (w.Status != ParseLimitExceeded || w.Code != "page_limit" || len(w.Pages) != 0 || len(w.Segments) != 0 || w.Coverage.ProcessedPages != 0) {
		return bad()
	}
	if w.Format == "image" && (w.Coverage.TotalPages != 1 || w.Coverage.Annotations != "not_applicable" || w.Coverage.Forms != "not_applicable") {
		return bad()
	}
	if w.Format == "pdf" && (w.Coverage.Annotations != "excluded" || w.Coverage.Forms != "excluded") {
		return bad()
	}
	if w.Status == ParseParsed && w.Code != "" || w.Status == ParseEmpty && w.Code != "no_text_detected" {
		return bad()
	}
	if w.Code == "image_integrity_unverified" && (w.Format != "image" || w.Status != ParsePartial) {
		return bad()
	}
	complete := w.Status == ParseParsed || w.Status == ParseEmpty
	switch w.Status {
	case ParseParsed, ParseEmpty, ParsePartial, ParseUnsupported, ParseEncrypted, ParseInvalid, ParseLimitExceeded:
	default:
		return bad()
	}
	if w.Coverage.Complete != complete || complete && (w.Coverage.TotalPages == 0 || w.Coverage.ProcessedPages != w.Coverage.TotalPages || len(w.Pages) != w.Coverage.TotalPages) || w.Status == ParseEmpty && len(w.Segments) != 0 || w.Status == ParseParsed && len(w.Segments) == 0 {
		return bad()
	}
	if w.Format != "image" && w.Format != "pdf" && (w.Format != "" || complete) {
		return bad()
	}
	if (w.Coverage.Annotations != "excluded" && w.Coverage.Annotations != "not_applicable") || (w.Coverage.Forms != "excluded" && w.Coverage.Forms != "not_applicable") {
		return bad()
	}
	pages := map[int]PageGeometry{}
	processed := 0
	for _, p := range w.Pages {
		if p.Page < 1 || p.Page > w.Coverage.TotalPages || p.Width < 1 || p.Height < 1 || p.Width > 4096 || p.Height > 4096 || int64(p.Width)*int64(p.Height) > 16000000 || p.Orientation < 1 || p.Orientation > 8 {
			return bad()
		}
		if _, ok := pages[p.Page]; ok {
			return bad()
		}
		pages[p.Page] = p
		if p.OCRComplete {
			processed++
		}
		if complete && (!p.OCRComplete || (w.Format == "pdf" && !p.NativeComplete)) {
			return bad()
		}
		if w.Format == "pdf" {
			if p.EffectiveCrop == nil || p.Transform == nil || !finiteBox(*p.EffectiveCrop) || p.Rotation%90 != 0 || p.Rotation < 0 || p.Rotation >= 360 {
				return bad()
			}
			for _, v := range *p.Transform {
				if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e9 {
					return bad()
				}
			}
			t := p.Transform
			if math.Abs(t[0]*t[3]-t[1]*t[2]) < 1e-12 {
				return bad()
			}
		} else if p.Transform != nil || p.EffectiveCrop != nil || p.NativeComplete || p.Rotation != 0 {
			return bad()
		}
	}
	if processed != w.Coverage.ProcessedPages {
		return bad()
	}
	var n int64
	for _, s := range w.Segments {
		p, ok := pages[s.Page]
		if !ok || !validRect(s.Rect) || !utf8.ValidString(s.Text) || strings.TrimSpace(s.Text) == "" || len(s.Text) > nativeTextLimit || math.IsNaN(s.Confidence) || math.IsInf(s.Confidence, 0) || s.Confidence < 0 || s.Confidence > 1 {
			return bad()
		}
		switch s.Origin {
		case "native":
			if w.Format != "pdf" || !p.NativeComplete || s.PageSpace == nil || !finiteBox(*s.PageSpace) {
				return bad()
			}
		case "ocr":
			if !p.OCRComplete || s.Clipped || s.PageSpace != nil {
				return bad()
			}
		default:
			return bad()
		}
		n += int64(len(s.Text))
	}
	if n != w.Counts.TextBytes {
		return bad()
	}
	return w, nil
}
func nativeResult(ctx context.Context, w nativeWire, source string) (ParseResult, error) {
	groups, err := groupNative(ctx, w.Segments)
	if err != nil {
		return ParseResult{}, err
	}
	engine, _ := json.Marshal(w.Engine)
	fingerprint := sha256.Sum256(engine)
	version := NativeParserVersion + "/" + hex.EncodeToString(fingerprint[:])
	r := ParseResult{ParserVersion: version, DetectedFormat: w.Format, Status: w.Status, Segments: groups, Coverage: ParseCoverage{Complete: w.Coverage.Complete, OffsetUnit: "page_rect", Notes: []string{"geometry_order_is_not_reading_order", "parsed_is_not_ocr_accuracy"}}, Native: &NativeMetadata{Engine: w.Engine, Pages: w.Pages, TotalPages: w.Coverage.TotalPages, ProcessedPages: w.Coverage.ProcessedPages, Annotations: w.Coverage.Annotations, Forms: w.Coverage.Forms, Encrypted: w.Encrypted, AllowsCopying: w.AllowsCopying}}
	if w.Code != "" {
		r.Warnings = []ParseWarning{{Code: w.Code, Message: w.Code}}
	}
	if w.Format == "pdf" {
		r.Coverage.SkippedFeatures = []string{"annotations", "form_values"}
	}
	if w.Code == "image_integrity_unverified" {
		r.Coverage.SkippedFeatures = []string{"image_integrity_validation"}
	}
	kept := r.Segments[:0]
	for i := range r.Segments {
		if err := ctx.Err(); err != nil {
			return ParseResult{}, err
		}
		s := &r.Segments[i]
		// A recovered or otherwise incomplete image cannot supply automatically
		// confirmed facts. Carry this requirement into downstream evidence before
		// hashing its identity; retain its actual OCR text and source rectangles.
		if w.Format == "image" && !w.Coverage.Complete {
			s.Visual.RequiresReview = true
		}
		n := int64(len(s.Text))
		for _, a := range s.Visual.Alternatives {
			n += int64(len(a.Text))
		}
		if r.Counts.TextBytes+n > nativeTextLimit {
			r.Status = ParseLimitExceeded
			r.Coverage.Complete = false
			r.Warnings = []ParseWarning{{Code: "text_limit", Message: "text_limit"}}
			break
		}
		identity, _ := json.Marshal(struct {
			Index   int
			Locator Locator
			Visual  *VisualEvidence
		}{i, s.Locator, s.Visual})
		id := sha256.Sum256([]byte(source + "\x00" + version + "\x00" + string(identity)))
		s.ID = hex.EncodeToString(id[:])
		r.Counts.TextBytes += n
		kept = append(kept, *s)
	}
	r.Segments = kept
	r.Counts.SourceBytes = w.Counts.SourceBytes
	r.Counts.DecodedTextBytes = w.Counts.TextBytes
	r.Counts.Segments = len(r.Segments)
	return r, nil
}
func whitespaceText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
func intersectArea(a, b Rect) float64 {
	return math.Max(0, math.Min(a[0]+a[2], b[0]+b[2])-math.Max(a[0], b[0])) * math.Max(0, math.Min(a[1]+a[3], b[1]+b[3])-math.Max(a[1], b[1]))
}
func unionRect(a, b Rect) Rect {
	x := math.Min(a[0], b[0])
	y := math.Min(a[1], b[1])
	return Rect{x, y, math.Max(a[0]+a[2], b[0]+b[2]) - x, math.Max(a[1]+a[3], b[1]+b[3]) - y}
}

// Connected overlap components prevent one-to-many observations from becoming
// independent facts. Only a unique 1:1 high-overlap whitespace-equal pair agrees.
func groupNative(ctx context.Context, input []nativeObservation) ([]Segment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	obs := append([]nativeObservation(nil), input...)
	sort.SliceStable(obs, func(i, j int) bool {
		a, b := obs[i], obs[j]
		if a.Page != b.Page {
			return a.Page < b.Page
		}
		for _, k := range []int{1, 0, 2, 3} {
			if a.Rect[k] != b.Rect[k] {
				return a.Rect[k] < b.Rect[k]
			}
		}
		if a.Origin != b.Origin {
			return a.Origin < b.Origin
		}
		return a.Text < b.Text
	})
	parent := make([]int, len(obs))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	// Sweep by page/y limits comparison to vertically overlapping observations.
	for i, a := range obs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for j := i + 1; j < len(obs); j++ {
			b := obs[j]
			if b.Page != a.Page || b.Rect[1] > a.Rect[1]+a.Rect[3] {
				break
			}
			area := intersectArea(a.Rect, b.Rect)
			if area > 0 && area/math.Min(a.Rect[2]*a.Rect[3], b.Rect[2]*b.Rect[3]) >= .25 {
				parent[find(j)] = find(i)
			}
		}
	}
	groups := map[int][]nativeObservation{}
	order := []int{}
	for i, o := range obs {
		key := find(i)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], o)
	}
	out := make([]Segment, 0, len(order))
	for _, key := range order {
		g := groups[key]
		rect := g[0].Rect
		v := &VisualEvidence{State: "ocr", RequiresReview: false}
		native, ocr := 0, 0
		clipped := false
		for _, o := range g {
			rect = unionRect(rect, o.Rect)
			if o.Origin == "native" {
				native++
			} else {
				ocr++
			}
			clipped = clipped || o.Clipped
			v.Alternatives = append(v.Alternatives, VisualAlternative{Origin: o.Origin, Text: o.Text, Rect: o.Rect, PageSpace: o.PageSpace, Clipped: o.Clipped, Confidence: o.Confidence})
		}
		text := g[0].Text
		if native > 0 {
			v.State = "native_unconfirmed"
			v.RequiresReview = true
		}
		if len(g) > 1 {
			v.State = "conflict"
			v.RequiresReview = true
			text = ""
		}
		if clipped {
			v.State = "native_unconfirmed"
		}
		if native == 1 && ocr == 1 && !clipped {
			a, b := g[0], g[1]
			area := intersectArea(a.Rect, b.Rect)
			iou := area / (a.Rect[2]*a.Rect[3] + b.Rect[2]*b.Rect[3] - area)
			if iou >= .65 && whitespaceText(a.Text) == whitespaceText(b.Text) {
				v.State = "confirmed"
				v.RequiresReview = false
				for _, o := range g {
					if o.Origin == "native" {
						text = o.Text
					}
				}
			}
		}
		out = append(out, Segment{Text: text, Locator: Locator{Kind: "page", Page: &PageLocation{g[0].Page, rect}}, Visual: v})
	}
	return out, nil
}
