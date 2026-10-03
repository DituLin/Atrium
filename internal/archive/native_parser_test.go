package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--protocol=1" {
		if _, ok := os.LookupEnv("ATRIUM_TEST_SECRET"); ok {
			os.Exit(10)
		}
		f := os.NewFile(3, "inherited")
		b := make([]byte, 4)
		n, e := f.ReadAt(b, 0)
		if e != nil || n != 4 || (string(b) != "test" && string(b) != "lang") {
			os.Exit(9)
		}
		w := nativeFixtureWire()
		if string(b) == "lang" {
			w.Status = ParseUnsupported
			w.Code = "parser_unavailable"
			w.Format = ""
			w.Segments = []nativeObservation{}
			w.Pages = []PageGeometry{}
			w.Counts = nativeCounts{}
			w.Coverage = nativeCoverage{Annotations: "not_applicable", Forms: "not_applicable"}
		}
		json.NewEncoder(os.Stdout).Encode(w)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func nativeFixtureWire() nativeWire {
	return nativeWire{Protocol: nativeProtocol, Status: ParseParsed, Format: "image", Engine: NativeEngine{Helper: "atrium-native/1", Build: strings.Repeat("a", 64), OSBuild: "test", VisionRevision: 3, Languages: []string{"zh-Hans", "en-US"}, Recognition: "accurate", LanguageCorrection: false}, Pages: []PageGeometry{{Page: 1, Width: 100, Height: 100, Orientation: 1, OCRComplete: true}}, Segments: []nativeObservation{{Page: 1, Text: "Lin 12.34", Origin: "ocr", Rect: Rect{.1, .2, .7, .1}, Confidence: .9}}, Coverage: nativeCoverage{Complete: true, TotalPages: 1, ProcessedPages: 1, Annotations: "not_applicable", Forms: "not_applicable"}, Counts: nativeCounts{SourceBytes: 4, TextBytes: 9, Segments: 1}}
}
func nativeOriginal(t *testing.T) NativeSource {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "original")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	f.WriteString("test")
	s := sha256.Sum256([]byte("test"))
	return NativeSource{File: f, SHA256: hex.EncodeToString(s[:])}
}
func nativeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestNativeUnavailableAndBorrowedDescriptor(t *testing.T) {
	s := nativeOriginal(t)
	p := NativeParser{HelperPath: "/no/such/archive-helper"}
	_, err := p.Parse(context.Background(), s)
	var e *NativeParserError
	if !errors.As(err, &e) || e.Code != "parser_unavailable" {
		t.Fatalf("got %v", err)
	}
	if _, err = s.File.ReadAt(make([]byte, 4), 0); err != nil {
		t.Fatalf("caller descriptor closed: %v", err)
	}
}
func TestNativeDescriptorAndProtocol(t *testing.T) {
	t.Setenv("ATRIUM_TEST_SECRET", "should-not-reach-helper")
	if runtime.GOOS == "windows" {
		t.Skip("inherited Unix descriptors")
	}
	s := nativeOriginal(t)
	if err := os.Remove(s.File.Name()); err != nil {
		t.Fatal(err)
	}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := NativeParser{HelperPath: helper}
	r, err := p.parseProcess(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ParseParsed || len(r.Segments) != 1 || r.Segments[0].Locator.Page.Page != 1 || len(r.Segments[0].ID) != 64 {
		t.Fatalf("bad result %+v", r)
	}
	r2, err := p.parseProcess(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if offset, _ := s.File.Seek(0, 1); offset != 4 {
		t.Fatalf("caller offset changed: %d", offset)
	}
	if r.Segments[0].ID != r2.Segments[0].ID {
		t.Fatal("unstable IDs")
	}
}
func TestNativeProtocolRejectsPoison(t *testing.T) {
	base := nativeFixtureWire()
	b, _ := json.Marshal(base)
	cases := map[string]string{"half": string(b[:30]), "multiple": string(b) + string(b), "trailing": string(b) + "poison", "unknown": strings.Replace(string(b), `"protocol":1`, `"protocol":1,"unknown":true`, 1), "duplicate": strings.Replace(string(b), `"protocol":1`, `"protocol":1,"protocol":1`, 1), "version": strings.Replace(string(b), `"protocol":1`, `"protocol":2`, 1), "nan": strings.Replace(string(b), `[0.1,0.2,0.7,0.1]`, `[NaN,0.2,0.7,0.1]`, 1), "rect": strings.Replace(string(b), `[0.1,0.2,0.7,0.1]`, `[0.8,0.2,0.7,0.1]`, 1), "count": strings.Replace(string(b), `"text_bytes":9`, `"text_bytes":0`, 1), "page": strings.Replace(string(b), `"page":1`, `"page":0`, 1), "missing": `{}`}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := decodeNative([]byte(raw), 4)
			if err == nil {
				t.Fatalf("accepted %s", raw)
			}
		})
	}
}
func TestNativeGeometryGrouping(t *testing.T) {
	obs := []nativeObservation{{Page: 1, Text: "金额 12.34", Origin: "native", Rect: Rect{.1, .1, .7, .1}}, {Page: 1, Text: "金额12.34", Origin: "ocr", Rect: Rect{.1, .1, .7, .1}}, {Page: 1, Text: "TOTAL 9.80", Origin: "native", Rect: Rect{.1, .4, .7, .1}}, {Page: 1, Text: "TOTAL 9.30", Origin: "ocr", Rect: Rect{.1, .4, .7, .1}}, {Page: 1, Text: "other", Origin: "ocr", Rect: Rect{.1, .8, .7, .1}}}
	groups := mustNativeGroups(t, obs)
	if len(groups) != 3 {
		t.Fatalf("got %d groups", len(groups))
	}
	if groups[0].Visual.RequiresReview || len(groups[0].Visual.Alternatives) != 2 {
		t.Fatal("equal whitespace text not confirmed")
	}
	if !groups[1].Visual.RequiresReview || len(groups[1].Visual.Alternatives) != 2 || groups[1].Text != "" {
		t.Fatal("conflict must have no preferred fact text")
	}
	if groups[2].Text != "other" {
		t.Fatal("OCR-only lost")
	}
	obs[1].Text = "金额12．34"
	if !mustNativeGroups(t, obs)[0].Visual.RequiresReview {
		t.Fatal("normalized punctuation")
	}
	obs[1].Text = "金额12.34"
	obs = append(obs, nativeObservation{Page: 1, Text: "12.34", Origin: "ocr", Rect: Rect{.4, .1, .3, .1}})
	if g := mustNativeGroups(t, obs); len(g) != 3 || !g[0].Visual.RequiresReview || len(g[0].Visual.Alternatives) != 3 {
		t.Fatal("ambiguous one-to-many became separate facts")
	}
	obs = []nativeObservation{{Page: 1, Text: "cropped", Origin: "native", Rect: Rect{0, 0, .2, .1}, Clipped: true}, {Page: 1, Text: "cropped", Origin: "ocr", Rect: Rect{0, 0, .2, .1}}}
	if g := mustNativeGroups(t, obs); !g[0].Visual.RequiresReview || g[0].Visual.State != "native_unconfirmed" {
		t.Fatal("clipped native confirmed")
	}
}
func TestNativeProcessesBoundedAndSafe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process fixture")
	}
	for name, body := range map[string]string{"hang": "sleep 30", "stdout": "while :; do printf '%01000d' 0; done", "stderr": "while :; do printf '%01000d' 0 >&2; done", "exit": "echo 'PRIVATE RECOGNIZED TEXT' >&2; exit 23"} {
		t.Run(name, func(t *testing.T) {
			p := NativeParser{HelperPath: nativeScript(t, body), Timeout: 200 * time.Millisecond}
			start := time.Now()
			_, err := p.parseProcess(context.Background(), nativeOriginal(t))
			if err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("unsafe error %v", err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("process exceeded cancellation bound")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := NativeParser{HelperPath: nativeScript(t, "sleep 30")}
	_, err := p.parseProcess(ctx, nativeOriginal(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestNativeProtocolRejectsMissingNullAndArrayArity(t *testing.T) {
	b, _ := json.Marshal(nativeFixtureWire())
	raw := string(b)
	for _, broken := range []string{strings.Replace(raw, `"clipped":false,`, "", 1), strings.Replace(raw, `"clipped":false`, `"clipped":null`, 1), strings.Replace(raw, `[0.1,0.2,0.7,0.1]`, `[0.1,0.2,0.7,0.1,0]`, 1), strings.Replace(raw, `"processed_pages":1`, `"processed_pages":null`, 1)} {
		if _, err := decodeNative([]byte(broken), 4); err == nil {
			t.Fatalf("accepted malformed shape: %s", broken)
		}
	}
}
func TestNativeCountsIncludePreferredAndAlternatives(t *testing.T) {
	w := nativeFixtureWire()
	w.Segments[0].Text = strings.Repeat("x", nativeTextLimit/2+1)
	w.Counts.TextBytes = int64(len(w.Segments[0].Text))
	r, err := nativeResult(context.Background(), w, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ParseLimitExceeded || r.Counts.TextBytes > nativeTextLimit {
		t.Fatal("combined preferred/alternative text exceeded budget")
	}
}
func TestNativeFloodReportsOutputLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixture")
	}
	for _, body := range []string{"head -c 9000000 /dev/zero", "head -c 70000 /dev/zero >&2"} {
		p := NativeParser{HelperPath: nativeScript(t, body), Timeout: 3 * time.Second}
		_, err := p.parseProcess(context.Background(), nativeOriginal(t))
		var pe *NativeParserError
		if !errors.As(err, &pe) || pe.Code != "parser_output_limit" {
			t.Fatalf("got %v", err)
		}
	}
}
func TestNativeCoverageContradictions(t *testing.T) {
	for _, change := range []func(*nativeWire){func(w *nativeWire) { w.Coverage.Annotations = "excluded" }, func(w *nativeWire) {
		w.Status = ParseEmpty
		w.Code = ""
		w.Segments = []nativeObservation{}
		w.Counts.TextBytes = 0
		w.Counts.Segments = 0
	}, func(w *nativeWire) {
		w.Coverage.TotalPages = 2
		w.Coverage.ProcessedPages = 2
		w.Pages = append(w.Pages, w.Pages[0])
		w.Pages[1].Page = 2
	}, func(w *nativeWire) { w.Code = "invalid_input" }} {
		w := nativeFixtureWire()
		change(&w)
		b, _ := json.Marshal(w)
		if _, err := decodeNative(b, 4); err == nil {
			t.Fatal("accepted contradictory coverage")
		}
	}
}

func mustNativeGroups(t *testing.T, obs []nativeObservation) []Segment {
	t.Helper()
	groups, err := groupNative(context.Background(), obs)
	if err != nil {
		t.Fatal(err)
	}
	return groups
}
func TestNativeGroupingHonorsTotalDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := groupNative(ctx, nativeFixtureWire().Segments); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestNativeProtocolRejectsUnpairedUTF16Escape(t *testing.T) {
	w := nativeFixtureWire()
	w.Segments[0].Text = "X"
	w.Counts.TextBytes = 3
	b, _ := json.Marshal(w)
	b = []byte(strings.Replace(string(b), `"text":"X"`, `"text":"\ud800"`, 1))
	if _, err := decodeNative(b, 4); err == nil {
		t.Fatal("accepted invalid Unicode escape")
	}
}

func TestNativeProcessRejectsExitAndInvalidJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process fixture")
	}
	for name, tc := range map[string]struct{ body, code string }{"exit": {"echo PRIVATE_RECOGNIZED_TEXT >&2; exit 23", "parser_failed"}, "half": {"printf '{\"protocol\":'", "parser_protocol"}, "multiple": {"printf '{}{}'", "parser_protocol"}, "bad_version": {"printf '{\"protocol\":2}'", "parser_protocol"}} {
		t.Run(name, func(t *testing.T) {
			_, err := (NativeParser{HelperPath: nativeScript(t, tc.body), Timeout: 3 * time.Second}).parseProcess(context.Background(), nativeOriginal(t))
			var e *NativeParserError
			if !errors.As(err, &e) || e.Code != tc.code || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("got %v", err)
			}
		})
	}
}
func TestNativeRejectsNonregularAndOversizeWithoutClosing(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	_, err = (NativeParser{}).parseProcess(context.Background(), NativeSource{r, strings.Repeat("a", 64)})
	var pe *NativeParserError
	if !errors.As(err, &pe) || pe.Code != "parser_invalid_source" {
		t.Fatal(err)
	}
	source := nativeOriginal(t)
	if err := source.File.Truncate(32<<20 + 1); err != nil {
		t.Fatal(err)
	}
	result, err := (NativeParser{}).parseProcess(context.Background(), source)
	if err != nil || result.Status != ParseLimitExceeded {
		t.Fatalf("got %+v %v", result, err)
	}
	if _, err := source.File.Stat(); err != nil {
		t.Fatal("closed oversized original")
	}
}

func TestNativeCapabilityUnavailableIsTyped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inherited Unix descriptor")
	}
	s := nativeOriginal(t)
	if _, err := s.File.WriteAt([]byte("lang"), 0); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("lang"))
	s.SHA256 = hex.EncodeToString(sum[:])
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = (NativeParser{HelperPath: helper}).parseProcess(context.Background(), s)
	var e *NativeParserError
	if !errors.As(err, &e) || e.Code != "parser_unavailable" {
		t.Fatalf("got %v", err)
	}
}
