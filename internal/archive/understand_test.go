package archive

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"unicode/utf8"
)

func understandingParse(s string) ParseResult {
	return ParseResult{ParserVersion: ParserVersion, Status: ParseParsed, Coverage: ParseCoverage{Complete: true}, Segments: []Segment{{ID: "s1", Text: s}}}
}
func TestUnderstandingPartition(t *testing.T) {
	s := strings.Repeat("中文😀\r\n \t", 30000)
	p, err := PlanChunks(understandingParse(s))
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	end := 0
	for _, c := range p.Chunks {
		if len(c.Owned) > 128 {
			t.Fatal("units")
		}
		b, _ := json.Marshal(c.Evidence())
		if len(b) > 24<<10 {
			t.Fatal("evidence bytes")
		}
		b, _ = json.Marshal(c)
		if len(b) > 32<<10 {
			t.Fatal("input bytes")
		}
		for _, v := range c.Owned {
			if v.Start != end || v.End-v.Start != utf8.RuneCountInString(v.Text) {
				t.Fatal("range")
			}
			end = v.End
			joined.WriteString(v.Text)
		}
	}
	if joined.String() != s {
		t.Fatal("partition lost text")
	}
}
func TestUnderstandingTypedCells(t *testing.T) {
	display := "151.23"
	p := understandingParse("")
	p.Segments = []Segment{
		{ID: "n", Text: display, Cell: &CellEvidence{Type: "n", State: "value", ValuePresent: true, RawValue: "151.234", DisplayValue: &display}},
		{ID: "f", Cell: &CellEvidence{Type: "n", State: "formula_cached", ValuePresent: true, RawValue: "7", Formula: &FormulaEvidence{Text: "SUM(A1:A2)", CachePresent: true, CachedRaw: "7"}}},
		{ID: "e", Cell: &CellEvidence{State: "blank"}},
		{ID: "s", Text: "名字", Cell: &CellEvidence{Type: "s", RawValue: "30", ValuePresent: true}},
		{ID: "b", Text: "TRUE", Cell: &CellEvidence{Type: "b", RawValue: "1", ValuePresent: true}},
	}
	plan, err := PlanChunks(p)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, c := range plan.Chunks {
		for _, v := range c.Owned {
			counts[v.ParentSegmentID+":"+v.ComponentKind]++
			if v.ParentSegmentID == "n" && v.ComponentKind == "numeric_raw" && v.Text != "151.234" {
				t.Fatal("rounded raw")
			}
		}
	}
	for _, k := range []string{"n:numeric_raw", "f:historical_cache", "f:formula_text", "e:cell_text", "s:cell_text", "b:cell_text"} {
		if counts[k] != 1 {
			t.Fatal(k, counts)
		}
	}
	if counts["f:numeric_raw"] != 0 || counts["s:numeric_raw"] != 0 || counts["b:numeric_raw"] != 0 {
		t.Fatal("non-values exposed")
	}
}
func TestUnderstandingAtomicAlternatives(t *testing.T) {
	p := understandingParse("")
	p.Segments[0].Visual = &VisualEvidence{State: "native_unconfirmed", RequiresReview: true, Alternatives: []VisualAlternative{{Origin: "ocr", Text: strings.Repeat("中", 9000)}, {Origin: "native", Text: "130"}}}
	if _, err := PlanChunks(p); errorCode(err) != "understand_group_limit" {
		t.Fatal(err)
	}
}

func validExtraction(c UnderstandingChunk) Extraction {
	e := Extraction{Domain: "health", DocumentType: "report", Subjects: []LocalSubject{}, Fields: []ExtractedField{}, Events: []EventProposal{}, Coverage: []UnitCoverage{}, Issues: []ExtractionIssue{}}
	for _, v := range c.Owned {
		e.Coverage = append(e.Coverage, UnitCoverage{ViewID: v.ViewID, Disposition: "irrelevant"})
	}
	return e
}
func extractionBytes(e Extraction) []byte { b, _ := json.Marshal(e); return b }
func TestUnderstandingValidation(t *testing.T) {
	p, _ := PlanChunks(understandingParse("体积130 ml，体积70 ml，体积70 ml"))
	c := p.Chunks[0]
	v := c.Owned[0]
	e := validExtraction(c)
	e.Fields = []ExtractedField{{ID: "f1", Name: "volume", Raw: "130", Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 2, End: 5, Quote: "130"}}, SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{{Kind: "decimal", Value: "130"}}}}
	e.Events = []EventProposal{{ID: "e1", Type: "measurement", FieldRefs: []string{"f1"}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{{ViewID: v.ViewID, Start: 0, End: 8, Quote: "体积130 ml"}}}}
	e.Coverage[0].Disposition = "extracted"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err != nil {
		t.Fatal(err)
	}
	e.Fields[0].Raw = "30"
	e.Fields[0].Evidence[0] = EvidenceReference{ViewID: v.ViewID, Start: 3, End: 5, Quote: "30"}
	e.Fields[0].Normalizations[0].Value = "30"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("substring number accepted")
	}
	e = validExtraction(c)
	e.Coverage = nil
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("coverage hole")
	}
	for _, bad := range []string{`{"domain":"health","domain":"finance"}`, `null`, `[]`, `{"global_entity_id":"x"}`, `{"domain":null}`, strings.Repeat("[", 70) + strings.Repeat("]", 70)} {
		if _, err := ValidateChunk(p, c, []byte(bad)); err == nil {
			t.Fatal("schema accepted", bad)
		}
	}
}
func TestUnderstandingNativeReviewMerge(t *testing.T) {
	p0 := understandingParse("70 ml")
	p0.Status = ParsePartial
	p0.Coverage.Complete = false
	p0.Segments[0].Visual = &VisualEvidence{State: "native_unconfirmed", RequiresReview: true, Alternatives: []VisualAlternative{{Origin: "ocr", Text: "70 ml"}, {Origin: "native", Text: "90 ml"}}}
	p, _ := PlanChunks(p0)
	c := p.Chunks[0]
	e := validExtraction(c)
	result, err := ValidateChunk(p, c, extractionBytes(e))
	if err != nil {
		t.Fatal(err)
	}
	merged, err := MergeChunks(p, []ChunkUnderstanding{result})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Complete || !merged.RequiresReview {
		t.Fatal("upgraded host state")
	}
	if _, err := MergeChunks(p, []ChunkUnderstanding{result, result}); err == nil {
		t.Fatal("duplicate chunk accepted")
	}
}

func TestUnderstandingLongCellAndNumericFabrication(t *testing.T) {
	p0 := understandingParse(strings.Repeat("长😀\r\n", 40000))
	p0.Segments[0].Cell = &CellEvidence{Type: "s", ValuePresent: true, RawValue: "123", State: "value"}
	p, err := PlanChunks(p0)
	if err != nil {
		t.Fatal(err)
	}
	var s string
	for _, c := range p.Chunks {
		for _, v := range c.Owned {
			if v.ComponentKind == "cell_text" {
				s += v.Text
			}
		}
	}
	if s != p0.Segments[0].Text {
		t.Fatal("cell loss")
	}
	p, _ = PlanChunks(understandingParse("130"))
	c := p.Chunks[0]
	e := validExtraction(c)
	e.Fields = []ExtractedField{{ID: "f", Name: "value", Raw: "30", Evidence: []EvidenceReference{{ViewID: c.Owned[0].ViewID, Start: 1, End: 3, Quote: "30"}}, SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{}}}
	e.Coverage[0].Disposition = "extracted"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("fabricated numeric raw accepted")
	}
}

func TestUnderstandingMultipleSubjectBinding(t *testing.T) {
	p, _ := PlanChunks(understandingParse("林明 1988-03-12 王华 1990-04-02"))
	c := p.Chunks[0]
	v := c.Owned[0]
	e := validExtraction(c)
	ref := func(a, b int) []EvidenceReference {
		return []EvidenceReference{{ViewID: v.ViewID, Start: a, End: b, Quote: string([]rune(v.Text)[a:b])}}
	}
	e.Subjects = []LocalSubject{{ID: "p1", Name: "林明", Role: "patient", Evidence: ref(0, 2)}, {ID: "p2", Name: "王华", Role: "patient", Evidence: ref(14, 16)}}
	e.Fields = []ExtractedField{{ID: "dob1", Name: "birth_date", Raw: "1988-03-12", SubjectRefs: []string{"p1"}, Evidence: ref(3, 13), Dimensions: []LiteralDimension{}, Normalizations: []Normalization{{Kind: "date", Value: "1988-03-12"}}}, {ID: "dob2", Name: "birth_date", Raw: "1990-04-02", SubjectRefs: []string{"p2"}, Evidence: ref(17, 27), Dimensions: []LiteralDimension{}, Normalizations: []Normalization{{Kind: "date", Value: "1990-04-02"}}}}
	e.Events = []EventProposal{{ID: "report", Type: "report", FieldRefs: []string{"dob1", "dob2"}, SubjectRefs: []string{"p1", "p2"}, SectionEvidence: ref(0, 27)}}
	e.Coverage[0].Disposition = "extracted"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err != nil {
		t.Fatal(err)
	}
	e.Fields[0].SubjectRefs = []string{"global123"}
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("unknown subject accepted")
	}
	e.Fields[0].SubjectRefs = []string{"p1"}
	e.Events[0].SubjectRefs = []string{"p2"}
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("incoherent event accepted")
	}
}

func TestUnderstandingNormalizationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, raw, display, format, kind, value string
		ok                                      bool
	}{
		{"precision", "151.234", "151.23", "0.00", "decimal", "151.234", true},
		{"rounding", "151.234", "151.23", "0.00", "decimal", "151.23", false},
		{"percent", "0.1234", "12.34%", "0.00%", "percentage", "12.34", true},
		{"guessed_percent", "0.1234", "", "General", "percentage", "12.34", false},
		{"serial1904_60", "60", "1904-03-01", "yyyy-mm-dd", "date", "1904-03-01", true},
		{"serial1900_60", "60", "1900-02-29", "yyyy-mm-dd", "date", "1900-02-29", false},
		{"two_digit_year", "60", "04-03-01", "yy-mm-dd", "date", "1904-03-01", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p0 := understandingParse(tc.display)
			p0.Segments[0].Cell = &CellEvidence{State: "value", Type: "n", ValuePresent: true, RawValue: tc.raw, DisplayValue: &tc.display, NumberFormatCode: tc.format}
			p, _ := PlanChunks(p0)
			c := p.Chunks[0]
			e := validExtraction(c)
			v := c.Owned[0]
			ref := EvidenceReference{ViewID: v.ViewID, Start: v.Start, End: v.End, Quote: v.Text}
			raw := v.Text
			if tc.kind == "date" {
				for v.ComponentKind != "derived_display" {
					for _, x := range c.Owned {
						if x.ComponentKind == "derived_display" {
							v = x
							break
						}
					}
				}
				ref = EvidenceReference{ViewID: v.ViewID, DerivedValueRef: v.Derived.Ref}
				raw = v.Text
			}
			e.Fields = []ExtractedField{{ID: "f", Name: "value", Raw: raw, SubjectRefs: []string{}, Evidence: []EvidenceReference{ref}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{{Kind: tc.kind, Value: tc.value}}}}
			for i, u := range e.Coverage {
				if u.ViewID == ref.ViewID {
					e.Coverage[i].Disposition = "extracted"
				}
			}
			_, err := ValidateChunk(p, c, extractionBytes(e))
			if (err == nil) != tc.ok {
				t.Fatal(err)
			}
		})
	}
}
func TestUnderstandingRequiredPropertiesAndRepeatedEvents(t *testing.T) {
	p, _ := PlanChunks(understandingParse("70 ml\n70 ml"))
	c := p.Chunks[0]
	e := validExtraction(c)
	v := c.Owned[0]
	ref := func(a, b int) EvidenceReference {
		return EvidenceReference{ViewID: v.ViewID, Start: a, End: b, Quote: string([]rune(v.Text)[a:b])}
	}
	for i, r := range []EvidenceReference{ref(0, 2), ref(6, 8)} {
		id := string(rune('a' + i))
		e.Fields = append(e.Fields, ExtractedField{ID: id, Name: "volume", Raw: "70", SubjectRefs: []string{}, Evidence: []EvidenceReference{r}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{{Kind: "decimal", Value: "70"}}})
		e.Events = append(e.Events, EventProposal{ID: id, Type: "measurement", FieldRefs: []string{id}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{ref(i*6, i*6+5)}})
	}
	e.Coverage[0].Disposition = "extracted"
	r, err := ValidateChunk(p, c, extractionBytes(e))
	if err != nil {
		t.Fatal(err)
	}
	m, err := MergeChunks(p, []ChunkUnderstanding{r})
	if err != nil || len(m.Chunks[0].Result.Events) != 2 {
		t.Fatal("collapsed events", err)
	}
	b := strings.Replace(string(extractionBytes(e)), `"derived_value_ref":"",`, "", 1)
	b = strings.Replace(b, `,"derived_value_ref":""`, "", 1)
	if _, err := ValidateChunk(p, c, []byte(b)); err == nil {
		t.Fatal("missing required property accepted")
	}
}
func TestUnderstandingQuoteHints(t *testing.T) {
	p, _ := PlanChunks(understandingParse("患者：林明\n血红蛋白：145.6 g/L\n"))
	found := false
	for _, v := range p.Chunks[0].Owned {
		for _, h := range v.QuoteHints {
			if string([]rune(v.Text)[h.Start-v.Start:h.End-v.Start]) != h.Text {
				t.Fatal("fake hint")
			}
			if h.Text == "145.6" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing numeric hint")
	}
}
func TestUnderstandingDimensions(t *testing.T) {
	p, _ := PlanChunks(understandingParse("USD 20; CNY 130.50"))
	c := p.Chunks[0]
	e := validExtraction(c)
	v := c.Owned[0]
	ref := func(a, b int) []EvidenceReference {
		return []EvidenceReference{{ViewID: v.ViewID, Start: a, End: b, Quote: string([]rune(v.Text)[a:b])}}
	}
	e.Fields = []ExtractedField{{ID: "usd", Name: "amount", Raw: "20", SubjectRefs: []string{}, Evidence: ref(4, 6), Normalizations: []Normalization{}, Dimensions: []LiteralDimension{{Kind: "currency", Raw: "USD", Evidence: ref(0, 3)}}}, {ID: "cny", Name: "amount", Raw: "130.50", SubjectRefs: []string{}, Evidence: ref(12, 18), Normalizations: []Normalization{}, Dimensions: []LiteralDimension{{Kind: "currency", Raw: "CNY", Evidence: ref(8, 11)}}}}
	e.Coverage[0].Disposition = "extracted"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err != nil {
		t.Fatal(err)
	}
	e.Fields[0].Dimensions[0].Raw = "CNY"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("fabricated currency accepted")
	}
}
func TestUnderstandingExactDuplicatesOnly(t *testing.T) {
	p, _ := PlanChunks(understandingParse("70"))
	c := p.Chunks[0]
	e := validExtraction(c)
	f := ExtractedField{ID: "a", Name: "volume", Raw: "70", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: c.Owned[0].ViewID, Start: 0, End: 2, Quote: "70"}}, Normalizations: []Normalization{}}
	e.Fields = []ExtractedField{f}
	f.ID = "b"
	e.Fields = append(e.Fields, f)
	e.Coverage[0].Disposition = "extracted"
	r, err := ValidateChunk(p, c, extractionBytes(e))
	if err != nil {
		t.Fatal(err)
	}
	m, err := MergeChunks(p, []ChunkUnderstanding{r})
	if err != nil || len(m.Chunks[0].Result.Fields) != 1 {
		t.Fatal("exact duplicate retained", err)
	}
}
func TestUnderstandingDigitsAcrossChunkAndMalformedUnicode(t *testing.T) {
	p, err := PlanChunks(understandingParse(strings.Repeat("9", 40000)))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Chunks) < 2 {
		t.Fatal("fixture")
	}
	c := p.Chunks[1]
	v := c.Owned[0]
	e := validExtraction(c)
	e.Fields = []ExtractedField{{ID: "f", Name: "amount", Raw: "9", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: v.Start, End: v.Start + 1, Quote: "9"}}, Normalizations: []Normalization{}}}
	e.Coverage[0].Disposition = "extracted"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("chunk-edge fake number")
	}
	for _, bad := range []string{`{"x":"\ud800"}`, `{"x":"\udc00"}`} {
		var dst struct {
			X string `json:"x"`
		}
		if strictJSON([]byte(bad), &dst) == nil {
			t.Fatal("invalid Unicode scalar")
		}
	}
}
func TestUnderstandingFormulaCacheStatesAndDerivedLiteral(t *testing.T) {
	p0 := understandingParse("")
	display := "151.23"
	p0.Segments = []Segment{{ID: "missing", Cell: &CellEvidence{Type: "n", Formula: &FormulaEvidence{Text: "SUM(A1:A2)"}}}, {ID: "empty", Cell: &CellEvidence{Type: "n", Formula: &FormulaEvidence{Text: "SUM(A1:A2)", CachePresent: true, CachedRaw: ""}}}, {ID: "zero", Cell: &CellEvidence{Type: "n", Formula: &FormulaEvidence{Text: "SUM(A1:A2)", CachePresent: true, CachedRaw: "0"}}}, {ID: "display", Cell: &CellEvidence{Type: "n", RawValue: "151.234", ValuePresent: true, DisplayValue: &display, NumberFormatCode: "0.00"}}}
	p, err := PlanChunks(p0)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Chunks[0]
	e := validExtraction(c)
	found := map[string]string{}
	for _, v := range c.Owned {
		if v.ComponentKind == "historical_cache" {
			found[v.ParentSegmentID] = v.Text
		}
		if v.ComponentKind == "derived_display" {
			e.Fields = []ExtractedField{{ID: "f", Name: "display", Raw: v.Text, SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: v.ViewID, DerivedValueRef: v.Derived.Ref}}, Normalizations: []Normalization{}}}
			for i, u := range e.Coverage {
				if u.ViewID == v.ViewID {
					e.Coverage[i].Disposition = "extracted"
				}
			}
		}
	}
	if _, ok := found["missing"]; ok {
		t.Fatal("missing cache fabricated")
	}
	if s, ok := found["empty"]; !ok || s != "" {
		t.Fatal("empty missing")
	}
	if found["zero"] != "0" {
		t.Fatal("zero lost")
	}
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err != nil {
		t.Fatal(err)
	}
}
func TestUnderstandingCompleteInputBudget(t *testing.T) {
	p, err := PlanChunks(understandingParse(strings.Repeat("\\\"\r\n😀中", 30000)))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Chunks {
		if completionInputBytes(c) > MaxChunkInput {
			t.Fatal("complete model input exceeds 32KiB")
		}
	}
}
func TestUnderstandingRejectsInvalidParentUTF8(t *testing.T) {
	p := understandingParse("ok")
	p.Segments[0].Cell = &CellEvidence{Type: "n", ValuePresent: true, RawValue: string([]byte{0xff})}
	if _, err := PlanChunks(p); err == nil {
		t.Fatal("invalid parent silently replaced")
	}
}
func TestUnderstandingNormalizationBelongsToField(t *testing.T) {
	p, _ := PlanChunks(understandingParse("70 130"))
	c := p.Chunks[0]
	e := validExtraction(c)
	id := c.Owned[0].ViewID
	e.Fields = []ExtractedField{{ID: "f", Name: "amount", Raw: "70", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: id, Start: 0, End: 2, Quote: "70"}, {ViewID: id, Start: 3, End: 6, Quote: "130"}}, Normalizations: []Normalization{{Kind: "decimal", Value: "130"}}}}
	e.Coverage[0].Disposition = "extracted"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("normalization bound to different raw field")
	}
}
func TestUnderstandingPropertyAndSharedOwners(t *testing.T) {
	p, _ := PlanChunks(understandingParse("林明 王华 新街1号"))
	c := p.Chunks[0]
	e := validExtraction(c)
	v := c.Owned[0]
	ref := func(a, b int) []EvidenceReference {
		return []EvidenceReference{{ViewID: v.ViewID, Start: a, End: b, Quote: string([]rune(v.Text)[a:b])}}
	}
	e.Subjects = []LocalSubject{{ID: "owner1", Name: "林明", Role: "owner", Evidence: ref(0, 2)}, {ID: "owner2", Name: "王华", Role: "owner", Evidence: ref(3, 5)}, {ID: "property1", Name: "新街1号", Role: "property", Evidence: ref(6, 10)}}
	e.Fields = []ExtractedField{{ID: "address", Name: "full_address", Raw: "新街1号", SubjectRefs: []string{"property1", "owner1", "owner2"}, Dimensions: []LiteralDimension{}, Evidence: ref(6, 10), Normalizations: []Normalization{}}}
	e.Events = []EventProposal{{ID: "property_report", Type: "ownership_record", SubjectRefs: []string{"owner1", "owner2", "property1"}, FieldRefs: []string{"address"}, SectionEvidence: ref(0, 10)}}
	e.Coverage[0].Disposition = "extracted"
	e.Domain = "property"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err != nil {
		t.Fatal(err)
	}
}
func TestUnderstandingUnitAndChunkBounds(t *testing.T) {
	p0 := understandingParse("")
	p0.Segments = nil
	for i := 0; i < 129; i++ {
		p0.Segments = append(p0.Segments, Segment{ID: fmt.Sprint(i), Cell: &CellEvidence{State: "blank"}})
	}
	p, err := PlanChunks(p0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, c := range p.Chunks {
		if len(c.Owned) > 128 {
			t.Fatal("unit cap")
		}
		count += len(c.Owned)
	}
	if count != 129 {
		t.Fatal("empty ownership loss")
	}
	p.Chunks[0].Owned[0].Text = "modified"
	if _, err := ValidateChunk(p, p.Chunks[0], extractionBytes(validExtraction(p.Chunks[0]))); err == nil {
		t.Fatal("modified manifest")
	}
	p0.Segments = nil
	for i := 0; i < 257; i++ {
		p0.Segments = append(p0.Segments, Segment{ID: fmt.Sprint(i), Visual: &VisualEvidence{Alternatives: []VisualAlternative{{Origin: "ocr", Text: strings.Repeat("中", 2000)}}}})
	}
	if _, err := PlanChunks(p0); errorCode(err) != "understand_plan_limit" {
		t.Fatal("chunk cap", err)
	}
}
func TestUnderstandingDecimalLeadingZeros(t *testing.T) {
	r, ok := decimalRat("070")
	if !ok || r.Cmp(big.NewRat(70, 1)) != 0 {
		t.Fatal("decimal interpreted as another radix", r)
	}
}
func TestUnderstandingSharedReportAnchorMerge(t *testing.T) {
	parsed := understandingParse("报告 A 2026-10-04")
	for i := 0; i < 35; i++ {
		parsed.Segments = append(parsed.Segments, Segment{ID: fmt.Sprintf("row%d", i), Text: "70 ml"})
	}
	p, err := PlanChunks(parsed)
	if err != nil || len(p.Chunks) < 2 {
		t.Fatal("fixture", err)
	}
	header := p.Chunks[0].Owned[0]
	results := []ChunkUnderstanding{}
	for _, c := range p.Chunks {
		e := validExtraction(c)
		var value EvidenceView
		for _, v := range c.Owned {
			if v.Text == "70 ml" {
				value = v
				break
			}
		}
		if value.ViewID == "" {
			t.Fatal("fixture value")
		}
		e.Fields = []ExtractedField{{ID: "f", Name: "volume", Raw: "70", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: value.ViewID, Start: 0, End: 2, Quote: "70"}}, Normalizations: []Normalization{}}}
		e.Events = []EventProposal{{ID: "report", Type: "report", FieldRefs: []string{"f"}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{{ViewID: header.ViewID, Start: header.Start, End: header.End, Quote: header.Text}}}}
		for i, u := range e.Coverage {
			if u.ViewID == value.ViewID || u.ViewID == header.ViewID {
				e.Coverage[i].Disposition = "extracted"
			}
		}
		r, err := ValidateChunk(p, c, extractionBytes(e))
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}
	m, err := MergeChunks(p, results)
	if err != nil || len(m.Events) != 1 || len(m.Events[0].Proposals) != len(p.Chunks) {
		t.Fatal("shared report not linked", err)
	}
}
func TestUnderstandingStructuralRowsAndLongBoundaryContext(t *testing.T) {
	parsed := understandingParse("")
	parsed.Segments = nil
	for row := 1; row <= 4; row++ {
		for col := 1; col <= 8; col++ {
			parsed.Segments = append(parsed.Segments, Segment{ID: fmt.Sprintf("r%dc%d", row, col), Text: "value", Locator: Locator{Kind: "csv", CSV: &CSVLocation{Record: row, Column: col}}})
		}
	}
	p, err := PlanChunks(parsed)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[int]int{}
	for _, c := range p.Chunks {
		for _, v := range c.Owned {
			r := v.Structure.Locator.CSV.Record
			if index, ok := rows[r]; ok && index != c.Index {
				t.Fatal("fitting row split")
			}
			rows[r] = c.Index
		}
	}
	p, err = PlanChunks(understandingParse(strings.Repeat("正文abcdefgh ", 12000)))
	if err != nil || len(p.Chunks) < 2 {
		t.Fatal("fixture", err)
	}
	if len(p.Chunks[1].Context) == 0 {
		t.Fatal("long parent lost all context")
	}
	for _, v := range p.Chunks[1].Context {
		if len(v.Text) > 2048 {
			t.Fatal("unbounded boundary context")
		}
	}
}
func TestUnderstandingNativeHeadingAnchorPolicy(t *testing.T) {
	title := "合成健康报告 SYNTHETIC REPORT"
	parsed := understandingParse(title)
	parsed.ParserVersion = NativeParserVersion
	parsed.Native = &NativeMetadata{}
	parsed.Segments[0].Locator.Page = &PageLocation{Page: 1, Rect: Rect{0, 0.05, 0.8, 0.03}}
	parsed.Segments[0].Visual = &VisualEvidence{State: "ocr_only", Alternatives: []VisualAlternative{{Origin: "ocr", Text: title}}}
	p, _ := PlanChunks(parsed)
	v := p.Chunks[0].Owned[0]
	ref := func(end int) EvidenceReference {
		return EvidenceReference{ViewID: v.ViewID, Start: 0, End: end, Quote: string([]rune(title)[:end])}
	}
	results := []ChunkUnderstanding{{Index: 0, Result: Extraction{Events: []EventProposal{{ID: "a", Type: "report", FieldRefs: []string{}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{ref(6)}}, {ID: "b", Type: "report", FieldRefs: []string{}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{ref(v.End)}}}}}}
	merged := mergeEventAnchors(p, results)
	if len(merged) != 1 || merged[0].AnchorPolicy != NativeHeadingAnchorPolicy {
		t.Fatal("native heading identity")
	}
	results[0].Result.Events[0].Type = "transaction"
	results[0].Result.Events[1].Type = "transaction"
	if len(mergeEventAnchors(p, results)) != 2 {
		t.Fatal("row prefix normalized")
	}
	for i := range results[0].Result.Events {
		results[0].Result.Events[i].Type = "report"
	}
	p.Chunks[0].Owned[0].RequiresReview = true
	if len(mergeEventAnchors(p, results)) != 2 {
		t.Fatal("uncertain heading normalized")
	}
	p.Chunks[0].Owned[0].RequiresReview = false
	p.Native = nil
	if len(mergeEventAnchors(p, results)) != 2 {
		t.Fatal("body prefix normalized")
	}
	p.Native = &NativeMetadata{}
	results[0].Result.Events[0].SectionEvidence = append(results[0].Result.Events[0].SectionEvidence, EvidenceReference{ViewID: "date1", Quote: "2026-10-04", End: 10})
	results[0].Result.Events[1].SectionEvidence = append(results[0].Result.Events[1].SectionEvidence, EvidenceReference{ViewID: "date2", Quote: "2026-11-04", End: 10})
	if len(mergeEventAnchors(p, results)) != 2 {
		t.Fatal("distinct reports merged")
	}
}
