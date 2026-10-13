package archive

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestUnderstandingQualityNumericSourceBinding(t *testing.T) {
	p, err := PlanChunks(understandingParse("Alice volume 70 ml"))
	if err != nil {
		t.Fatal(err)
	}
	c := p.Chunks[0]
	v := c.Owned[0]
	e := validExtraction(c)
	e.Fields = []ExtractedField{{ID: "f", Name: "volume", Raw: "Alice", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 0, End: 5, Quote: "Alice"}, {ViewID: v.ViewID, Start: 13, End: 15, Quote: "70"}}, Normalizations: []Normalization{{Kind: "decimal", Value: "70"}}}}
	e.Coverage[0].Disposition = "extracted"
	r, err := ValidateChunk(p, c, extractionBytes(e))
	if err == nil {
		m, me := MergeChunks(p, []ChunkUnderstanding{r})
		t.Fatalf("unrelated normalization accepted: raw=%q value=%q mergeErr=%v complete=%v review=%v", r.Result.Fields[0].Raw, r.Result.Fields[0].Normalizations[0].Value, me, m.Complete, m.RequiresReview)
	}
}
func TestUnderstandingQualityJSONCaseAlias(t *testing.T) {
	p, _ := PlanChunks(understandingParse("hello"))
	c := p.Chunks[0]
	e := validExtraction(c)
	b := extractionBytes(e)
	b = bytes.Replace(b, []byte(`"domain":"health"`), []byte(`"domain":"health","Domain":"finance"`), 1)
	r, err := ValidateChunk(p, c, b)
	if err == nil {
		t.Fatalf("case alias accepted, resulting domain=%s", r.Result.Domain)
	}
}
func TestUnderstandingQualityInventedEmptyValue(t *testing.T) {
	p, _ := PlanChunks(understandingParse("volume 130 ml"))
	c := p.Chunks[0]
	v := c.Owned[0]
	e := validExtraction(c)
	e.Fields = []ExtractedField{{ID: "f", Name: "volume", Raw: "", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 7, End: 7, Quote: ""}}, Normalizations: []Normalization{}}}
	e.Coverage[0].Disposition = "extracted"
	r, err := ValidateChunk(p, c, extractionBytes(e))
	if err == nil {
		m, me := MergeChunks(p, []ChunkUnderstanding{r})
		t.Fatalf("fabricated empty value accepted over nonempty source: mergeErr=%v complete=%v review=%v", me, m.Complete, m.RequiresReview)
	}
}
func TestUnderstandingQualityPositiveControls(t *testing.T) {
	for _, raw := range []string{"70", ""} {
		p, _ := PlanChunks(understandingParse(raw))
		c := p.Chunks[0]
		v := c.Owned[0]
		e := validExtraction(c)
		norms := []Normalization{}
		if raw != "" {
			norms = append(norms, Normalization{Kind: "decimal", Value: raw})
		}
		e.Fields = []ExtractedField{{ID: "f", Name: "value", Raw: raw, SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 0, End: len(raw), Quote: raw}}, Normalizations: norms}}
		e.Coverage[0].Disposition = "extracted"
		r, err := ValidateChunk(p, c, extractionBytes(e))
		if err != nil {
			t.Fatal(raw, err)
		}
		if _, err := MergeChunks(p, []ChunkUnderstanding{r}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnderstandingQualityNumericSources(t *testing.T) {
	for _, raw := range []string{"Alice", "2026-10-04", "volume", "70"} {
		t.Run(raw, func(t *testing.T) {
			text := raw + " 70"
			p, _ := PlanChunks(understandingParse(text))
			c := p.Chunks[0]
			id := c.Owned[0].ViewID
			e := validExtraction(c)
			e.Fields = []ExtractedField{{ID: "f", Name: "value", Raw: raw, SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: id, Start: 0, End: len(raw), Quote: raw}, {ViewID: id, Start: len(raw) + 1, End: len(raw) + 3, Quote: "70"}}, Normalizations: []Normalization{{Kind: "decimal", Value: "70"}}}}
			e.Coverage[0].Disposition = "extracted"
			_, err := ValidateChunk(p, c, extractionBytes(e))
			if (err == nil) != (raw == "70") {
				t.Fatalf("numeric source binding: %v", err)
			}
		})
	}
	// A numeric cache in another cell cannot supply a percentage for a text field.
	p0 := understandingParse("Alice")
	p0.Segments = append(p0.Segments, Segment{ID: "amount", Text: "70%", Cell: &CellEvidence{Type: "n", State: "value", ValuePresent: true, RawValue: "0.70", NumberFormatCode: "0%"}})
	p, _ := PlanChunks(p0)
	c := p.Chunks[0]
	e := validExtraction(c)
	refs := []EvidenceReference{}
	for _, v := range c.Owned {
		if v.Text == "Alice" || v.ComponentKind == "numeric_raw" {
			refs = append(refs, EvidenceReference{ViewID: v.ViewID, Start: 0, End: len(v.Text), Quote: v.Text})
			for i := range e.Coverage {
				if e.Coverage[i].ViewID == v.ViewID {
					e.Coverage[i].Disposition = "extracted"
				}
			}
		}
	}
	e.Fields = []ExtractedField{{ID: "f", Name: "rate", Raw: "Alice", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: refs, Normalizations: []Normalization{{Kind: "percentage", Value: "70"}}}}
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("borrowed unrelated percentage source")
	}
	e.Fields[0].Raw = "0.70"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err != nil {
		t.Fatal("valid percentage source", err)
	}
}

func TestUnderstandingQualityExactJSONKeys(t *testing.T) {
	p, _ := PlanChunks(understandingParse("70"))
	c := p.Chunks[0]
	e := validExtraction(c)
	v := c.Owned[0]
	e.Fields = []ExtractedField{{ID: "f", Name: "value", Raw: "70", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 0, End: 2, Quote: "70"}}, Normalizations: []Normalization{{Kind: "decimal", Value: "70"}}}}
	e.Coverage[0].Disposition = "extracted"
	base := extractionBytes(e)
	for _, pair := range [][2]string{{`"domain":"health"`, `"domain":"health","Domain":"health"`}, {`"raw":"70"`, `"raw":"70","Raw":"70"`}, {`"start":0`, `"start":0,"Start":0`}, {`"kind":"decimal"`, `"kind":"decimal","Kind":"decimal"`}, {`"quote":"70"`, `"quote":"70","unknown":"x"`}} {
		t.Run(pair[0], func(t *testing.T) {
			b := bytes.Replace(base, []byte(pair[0]), []byte(pair[1]), 1)
			if bytes.Equal(base, b) {
				t.Fatal("fixture")
			}
			if _, err := ValidateChunk(p, c, b); err == nil {
				t.Fatal("aliased/unknown key accepted")
			}
		})
	}
	if _, err := ValidateChunk(p, c, base); err != nil {
		t.Fatal("exact keys", err)
	}
	// Optional protocol usage and its optional members also require exact tags.
	response := WorkerResponse{Result: validExtraction(c)}
	wire, _ := json.Marshal(response)
	for _, fragment := range []string{`"Usage":{"input_tokens":7},`, `"usage":{"Input_Tokens":7},`, `"usage":{"input_tokens":7,"Input_Tokens":8},`, `"Protocol_Version":1,`} {
		t.Run(fragment, func(t *testing.T) {
			b := append([]byte("{"+fragment), wire[1:]...)
			var dst WorkerResponse
			if strictJSON(b, &dst) == nil {
				t.Fatal("protocol optional/nested alias accepted")
			}
		})
	}
	for _, fragment := range []string{"", `"usage":{},`, `"usage":{"input_tokens":7},`} {
		b := append([]byte("{"+fragment), wire[1:]...)
		var dst WorkerResponse
		if err := strictJSON(b, &dst); err != nil {
			t.Fatal("valid optional protocol", err)
		}
	}
}

func TestUnderstandingQualityEmptyReferences(t *testing.T) {
	for _, kind := range []string{"body", "numeric_raw", "subject", "dimension", "event"} {
		t.Run(kind, func(t *testing.T) {
			p0 := understandingParse("Alice 70 ml")
			if kind == "numeric_raw" {
				p0.Segments[0].Text = "70"
				p0.Segments[0].Cell = &CellEvidence{Type: "n", State: "value", RawValue: "70", ValuePresent: true}
			}
			p, _ := PlanChunks(p0)
			c := p.Chunks[0]
			v := c.Owned[0]
			e := validExtraction(c)
			empty := EvidenceReference{ViewID: v.ViewID, Start: v.End, End: v.End, Quote: ""}
			e.Coverage[0].Disposition = "extracted"
			switch kind {
			case "subject":
				e.Subjects = []LocalSubject{{ID: "s", Name: "Alice", Role: "patient", Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 0, End: 5, Quote: "Alice"}, empty}}}
			case "dimension":
				e.Fields = []ExtractedField{{ID: "f", Name: "volume", Raw: "70", SubjectRefs: []string{}, Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 6, End: 8, Quote: "70"}}, Dimensions: []LiteralDimension{{Kind: "unit", Raw: "ml", Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 9, End: 11, Quote: "ml"}, empty}}}, Normalizations: []Normalization{}}}
			case "event":
				e.Events = []EventProposal{{ID: "ev", Type: "report", FieldRefs: []string{}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{empty}}}
			default:
				e.Fields = []ExtractedField{{ID: "f", Name: "empty", Raw: "", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{empty}, Normalizations: []Normalization{}}}
			}
			if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
				t.Fatal("empty span in nonempty component accepted")
			}
		})
	}
	for _, cell := range []*CellEvidence{nil, {Type: "inlineStr", State: "empty"}, {Type: "n", State: "value", ValuePresent: true, RawValue: ""}, {Type: "n", Formula: &FormulaEvidence{Text: "SUM(A1:A2)", CachePresent: true, CachedRaw: ""}}} {
		p0 := understandingParse("")
		p0.Segments[0].Cell = cell
		p, err := PlanChunks(p0)
		if err != nil {
			t.Fatal(err)
		}
		c := p.Chunks[0]
		e := validExtraction(c)
		found := false
		for _, v := range c.Owned {
			if v.Text != "" || v.ComponentKind == "formula_text" || v.ComponentKind == "format_code" {
				continue
			}
			found = true
			e.Fields = append(e.Fields, ExtractedField{ID: v.ViewID, Name: "empty", Raw: "", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: 0, End: 0, Quote: ""}}, Normalizations: []Normalization{}})
			for i := range e.Coverage {
				if e.Coverage[i].ViewID == v.ViewID {
					e.Coverage[i].Disposition = "extracted"
				}
			}
		}
		if !found {
			t.Fatal("missing real empty component")
		}
		if _, err := ValidateChunk(p, c, extractionBytes(e)); err != nil {
			t.Fatal("true empty component rejected", err)
		}
	}
}
