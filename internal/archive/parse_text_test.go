package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
)

func parseBytes(t *testing.T, b []byte, hint string, limits ParseLimits) ParseResult {
	t.Helper()
	sum := sha256.Sum256(b)
	r, e := Parse(context.Background(), ParseSource{bytes.NewReader(b), int64(len(b)), fmt.Sprintf("%x", sum), hint}, limits)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("../../testdata/archive/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func warning(r ParseResult, code string) bool {
	for _, w := range r.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
func TestTextExactEvidence(t *testing.T) {
	b := []byte("张明 1980-03-12 医生周青\r\n喂养70ml\r\n喂养70ml\r\n")
	r := parseBytes(t, b, "text", ParseLimits{})
	if r.Status != ParseParsed || r.DetectedFormat != "text" || len(r.Segments) != 1 {
		t.Fatalf("result: %+v", r)
	}
	s := r.Segments[0]
	if s.Text != string(b) || s.Locator.Text == nil || *s.Locator.Text != (TextLocation{0, len([]rune(string(b))), "unicode_scalar"}) {
		t.Fatalf("evidence: %+v", s)
	}
	again := parseBytes(t, b, "text", ParseLimits{})
	if s.ID == "" || s.ID != again.Segments[0].ID {
		t.Fatal("unstable ID")
	}
	if r.Counts.SourceBytes != int64(len(b)) || !r.Coverage.Complete {
		t.Fatalf("coverage: %+v", r)
	}
}
func TestTextEncodingStates(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		want ParseStatus
		text string
	}{
		{"utf8bom", []byte{239, 187, 191, 65}, ParseParsed, "A"},
		{"utf16le", []byte{255, 254, 0x2d, 0x4e, 0x3d, 0xd8, 0, 0xde}, ParseParsed, "中😀"},
		{"utf16be", []byte{254, 255, 0x4e, 0x2d}, ParseParsed, "中"},
		{"oddutf16", []byte{255, 254, 65}, ParseInvalid, ""},
		{"surrogate", []byte{255, 254, 0, 0xd8, 65, 0}, ParseInvalid, ""},
		{"badutf8", []byte{0xff, 65}, ParseInvalid, ""},
		{"nulbinary", []byte{0, 65}, ParseUnsupported, ""},
		{"empty", nil, ParseEmpty, ""},
		{"bomonly", []byte{239, 187, 191}, ParseEmpty, ""},
		{"pdf", []byte("%PDF-1.7\n"), ParseUnsupported, ""},
		{"cfb", []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}, ParseUnsupported, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := parseBytes(t, c.b, "text", ParseLimits{})
			if r.Status != c.want {
				t.Fatalf("%+v", r)
			}
			if c.text != "" && (len(r.Segments) != 1 || r.Segments[0].Text != c.text) {
				t.Fatalf("%+v", r.Segments)
			}
		})
	}
}
func TestCSVLogicalAndPhysicalEvidence(t *testing.T) {
	b := []byte("账单,金额,备注,\r\nTEST-BILL-001,128.10,\"第1行\r\n第2行\",\r\n =A1,20.00,USD\r\n")
	r := parseBytes(t, b, "csv", ParseLimits{})
	if r.Status != ParsePartial || len(r.Segments) != 11 || !warning(r, "csv_ragged_record") {
		t.Fatalf("%+v", r)
	}
	checks := []struct {
		i    int
		text string
		loc  CSVLocation
	}{{4, "TEST-BILL-001", CSVLocation{2, 1, 2, 1}}, {6, "第1行\n第2行", CSVLocation{2, 3, 2, 22}}, {7, "", CSVLocation{2, 4, 3, 10}}, {8, " =A1", CSVLocation{3, 1, 4, 1}}}
	for _, c := range checks {
		s := r.Segments[c.i]
		if s.Text != c.text || s.Locator.CSV == nil || !reflect.DeepEqual(*s.Locator.CSV, c.loc) {
			t.Fatalf("%d: %+v location %+v", c.i, s, s.Locator.CSV)
		}
	}
	if r.Coverage.OffsetUnit != "decoded_input_byte" {
		t.Fatalf("coverage %+v", r.Coverage)
	}
}
func TestCSVInvalidQuoteAndEmptyField(t *testing.T) {
	for _, b := range []string{"a,\"unterminated", "a,b\"c"} {
		r := parseBytes(t, []byte(b), "csv", ParseLimits{})
		if r.Status != ParseInvalid {
			t.Fatalf("%q: %+v", b, r)
		}
	}
	r := parseBytes(t, []byte(",,\n"), "csv", ParseLimits{})
	if r.Status != ParseParsed || len(r.Segments) != 3 {
		t.Fatalf("%+v", r)
	}
}
func TestParserLimitsAndIO(t *testing.T) {
	for _, l := range []ParseLimits{{SourceBytes: 2}, {TextBytes: 2}, {StringBytes: 2}} {
		r := parseBytes(t, []byte("hello"), "text", l)
		if r.Status != ParseLimitExceeded {
			t.Fatalf("%+v", r)
		}
	}
	r := parseBytes(t, []byte("a,b,c"), "csv", ParseLimits{Segments: 2})
	if r.Status != ParseLimitExceeded {
		t.Fatalf("%+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := Parse(ctx, ParseSource{Reader: bytes.NewReader([]byte("x")), Size: 1, SHA256: fmt.Sprintf("%064x", 1)}, ParseLimits{})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("%v", e)
	}
	_, e = Parse(context.Background(), ParseSource{Reader: brokenReader{}, Size: 1, SHA256: fmt.Sprintf("%064x", 1)}, ParseLimits{})
	if !errors.Is(e, errRead) {
		t.Fatalf("%v", e)
	}
}

var errRead = errors.New("original read failed")

type brokenReader struct{}

func (brokenReader) ReadAt(p []byte, off int64) (int, error) { return 0, errRead }
func TestParseResultStableJSONAndStringCounts(t *testing.T) {
	r := parseBytes(t, []byte("家庭文件"), "text", ParseLimits{})
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"detected_format"`, `"parser_version"`, `"status"`, `"segments"`, `"locator"`, `"unicode_scalar"`, `"source_bytes"`} {
		if !bytes.Contains(b, []byte(key)) {
			t.Errorf("missing stable JSON key %s: %s", key, b)
		}
	}
	if r.Counts.Strings != 1 {
		t.Fatalf("strings not counted: %+v", r.Counts)
	}
	r = parseBytes(t, []byte("a,b"), "csv", ParseLimits{Strings: 1})
	if r.Status != ParseLimitExceeded {
		t.Fatalf("string limit %+v", r)
	}
}
