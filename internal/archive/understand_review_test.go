package archive

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestUnderstandingReviewDateCrossBinding(t *testing.T) {
	p, _ := PlanChunks(understandingParse("2026-10-01 2026-10-04"))
	c := p.Chunks[0]
	id := c.Owned[0].ViewID
	e := validExtraction(c)
	e.Fields = []ExtractedField{{ID: "f", Name: "report_date", Raw: "2026-10-01", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: id, Start: 0, End: 10, Quote: "2026-10-01"}, {ViewID: id, Start: 11, End: 21, Quote: "2026-10-04"}}, Normalizations: []Normalization{{Kind: "date", Value: "2026-10-04"}}}}
	e.Coverage[0].Disposition = "extracted"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("accepted normalized 2026-10-04 for raw 2026-10-01")
	}
}
func TestUnderstandingReviewOpenLabels(t *testing.T) {
	p, _ := PlanChunks(understandingParse("家庭保险"))
	c := p.Chunks[0]
	var e map[string]any
	json.Unmarshal(extractionBytes(validExtraction(c)), &e)
	e["labels"] = []string{"insurance", "household"}
	b, _ := json.Marshal(e)
	if _, err := ValidateChunk(p, c, b); err != nil {
		t.Fatalf("bounded open labels unsupported: %v", err)
	}
}
func TestUnderstandingReviewSameParentAnchorDifferentViews(t *testing.T) {
	p, err := PlanChunks(understandingParse(strings.Repeat("x", 50000)))
	if err != nil {
		t.Fatal(err)
	}
	var own, ctx EvidenceView
	ci := -1
	for i := 1; i < len(p.Chunks) && ci < 0; i++ {
		for _, v := range p.Chunks[i].Context {
			for _, o := range p.Chunks[0].Owned {
				if v.ComponentID == o.ComponentID && v.Start >= o.Start && v.End <= o.End && v.ViewID != o.ViewID {
					own = o
					ctx = v
					ci = i
					break
				}
			}
		}
	}
	if ci < 0 {
		t.Fatal("no shared context range")
	}
	results := []ChunkUnderstanding{}
	for i, c := range p.Chunks {
		e := validExtraction(c)
		if i == 0 || i == ci {
			v := c.Owned[0]
			e.Fields = []ExtractedField{{ID: "f", Name: "text", Raw: "x", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Normalizations: []Normalization{}, Evidence: []EvidenceReference{{ViewID: v.ViewID, Start: v.Start, End: v.Start + 1, Quote: "x"}}}}
			e.Coverage[0].Disposition = "extracted"
			ref := EvidenceReference{ViewID: ctx.ViewID, Start: ctx.Start, End: ctx.End, Quote: ctx.Text}
			if i == 0 {
				ref.ViewID = own.ViewID
			}
			e.Events = []EventProposal{{ID: "e", Type: "report", FieldRefs: []string{"f"}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{ref}}}
		}
		r, err := ValidateChunk(p, c, extractionBytes(e))
		if err != nil {
			t.Fatalf("chunk %d: %v", i, err)
		}
		results = append(results, r)
	}
	m, err := MergeChunks(p, results)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Events) != 1 {
		t.Fatalf("same parent/range %d:%d became %d events, review=%v issues=%v; owned=%s context=%s", ctx.Start, ctx.End, len(m.Events), m.RequiresReview, m.LinkIssues, own.ViewID, ctx.ViewID)
	}
}

func TestUnderstandingReviewDerivedDateRawBinding(t *testing.T) {
	parsed := understandingParse("birth")
	display1, display2 := "1988-03-12", "2026-10-04"
	parsed.Segments[0].Cell = &CellEvidence{State: "value", Type: "n", ValuePresent: true, RawValue: "32214", DisplayValue: &display1, NumberFormatCode: "yyyy-mm-dd"}
	s := parsed.Segments[0]
	s.ID = "report-date"
	s.Cell = &CellEvidence{State: "value", Type: "n", ValuePresent: true, RawValue: "46399", DisplayValue: &display2, NumberFormatCode: "yyyy-mm-dd"}
	parsed.Segments = append(parsed.Segments, s)
	p, err := PlanChunks(parsed)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Chunks[0]
	e := validExtraction(c)
	refs := []EvidenceReference{}
	for _, v := range c.Owned {
		if v.Derived != nil {
			refs = append(refs, EvidenceReference{ViewID: v.ViewID, DerivedValueRef: v.Derived.Ref})
			for i := range e.Coverage {
				if e.Coverage[i].ViewID == v.ViewID {
					e.Coverage[i].Disposition = "extracted"
				}
			}
		}
	}
	e.Fields = []ExtractedField{{ID: "dob", Name: "birth_date", Raw: display1, SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: refs, Normalizations: []Normalization{{Kind: "date", Value: display2}}}}
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("derived report date rebound to birth raw")
	}
	e.Fields[0].Normalizations[0].Value = display1
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err != nil {
		t.Fatal("valid parser-derived date", err)
	}
}

func TestUnderstandingReviewPartialDateRawBinding(t *testing.T) {
	p, _ := PlanChunks(understandingParse("03-12 10-04"))
	c := p.Chunks[0]
	e := validExtraction(c)
	id := c.Owned[0].ViewID
	e.Fields = []ExtractedField{{ID: "dob", Name: "birth_date", Raw: "03-12", SubjectRefs: []string{}, Dimensions: []LiteralDimension{}, Evidence: []EvidenceReference{{ViewID: id, Start: 0, End: 5, Quote: "03-12"}, {ViewID: id, Start: 6, End: 11, Quote: "10-04"}}, Normalizations: []Normalization{{Kind: "date_partial", Value: "10-04"}}}}
	e.Coverage[0].Disposition = "extracted"
	if _, err := ValidateChunk(p, c, extractionBytes(e)); err == nil {
		t.Fatal("partial dates crossed")
	}
}

func TestUnderstandingReviewAnchorProvenance(t *testing.T) {
	base := EvidenceView{ViewID: "owned", ParentSegmentID: "parent", ComponentID: "parent/body", ComponentKind: "body", Origin: "ocr", AlternativeID: "alternative-0", ParseDigest: "parse-a", ParserVersion: "parser-a", Start: 0, End: 10, Text: "xxxxxxxxxx"}
	for _, tc := range []struct {
		name       string
		change     func(*EvidenceView)
		start, end int
		events     int
		overlap    bool
	}{
		{"view_alias", func(v *EvidenceView) { v.ViewID = "context"; v.Start = 2; v.Text = "xxxxxxxx" }, 2, 8, 1, false},
		{"overlap_view_alias", func(v *EvidenceView) { v.ViewID = "context"; v.Start = 2; v.Text = "xxxxxxxx" }, 3, 9, 2, true},
		{"different_range", func(v *EvidenceView) { v.ViewID = "context" }, 8, 10, 2, false},
		{"alternative", func(v *EvidenceView) { v.AlternativeID = "alternative-1" }, 2, 8, 2, false},
		{"parse", func(v *EvidenceView) { v.ParseDigest = "parse-b" }, 2, 8, 2, false},
		{"origin", func(v *EvidenceView) { v.Origin = "native" }, 2, 8, 2, false},
		{"parent", func(v *EvidenceView) { v.ParentSegmentID = "parent-b"; v.ComponentID = "parent-b/body" }, 2, 8, 2, false},
		{"component", func(v *EvidenceView) { v.ComponentKind = "cell_text" }, 2, 8, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := base
			tc.change(&v)
			p := UnderstandingPlan{Chunks: []UnderstandingChunk{{Owned: []EvidenceView{base}}, {Owned: []EvidenceView{v}}}}
			event := func(view EvidenceView, start, end int) EventProposal {
				return EventProposal{ID: "e", Type: "report", SectionEvidence: []EvidenceReference{{ViewID: view.ViewID, Start: start, End: end, Quote: strings.Repeat("x", end-start)}}}
			}
			cs := []ChunkUnderstanding{{Index: 0, Result: Extraction{Events: []EventProposal{event(base, 2, 8)}}}, {Index: 1, Result: Extraction{Events: []EventProposal{event(v, tc.start, tc.end)}}}}
			m := mergeEventAnchors(p, cs)
			if len(m) != tc.events {
				t.Fatalf("events=%d", len(m))
			}
			if overlappingReportAnchors(m) != tc.overlap {
				t.Fatal("wrong overlap status")
			}
			if len(m) == 1 && len(m[0].Proposals) != 2 {
				t.Fatal("lost proposals")
			}
		})
	}
}

func TestUnderstandingReviewLabelsBounds(t *testing.T) {
	p, _ := PlanChunks(understandingParse("家庭保险"))
	c := p.Chunks[0]
	for _, tc := range []struct {
		name   string
		labels any
		ok     bool
	}{{"open", []string{"insurance", "household", "未预注册新标签"}, true}, {"empty", []string{}, true}, {"duplicate", []string{"insurance", "insurance"}, false}, {"blank", []string{" "}, false}, {"null", nil, false}, {"scalar", "insurance", false}, {"long", []string{strings.Repeat("x", 129)}, false}, {"many", strings.Fields(strings.Repeat("label ", 17)), false}, {"control", []string{"a\nb"}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var e map[string]any
			json.Unmarshal(extractionBytes(validExtraction(c)), &e)
			e["labels"] = tc.labels
			b, _ := json.Marshal(e)
			_, err := ValidateChunk(p, c, b)
			if (err == nil) != tc.ok {
				t.Fatalf("labels acceptance=%v error=%v", err == nil, err)
			}
		})
	}
}

func TestUnderstandingReviewLabelProvenanceAndOldVersion(t *testing.T) {
	p, err := PlanChunks(understandingParse(strings.Repeat("x", 12000)))
	if err != nil || len(p.Chunks) < 2 {
		t.Fatal("fixture", err)
	}
	results := []ChunkUnderstanding{}
	for i, c := range p.Chunks {
		e := validExtraction(c)
		e.Labels = []string{"household"}
		if i == 1 {
			e.Labels = append(e.Labels, "insurance")
		}
		r, err := ValidateChunk(p, c, extractionBytes(e))
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}
	m, err := MergeChunks(p, results)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Labels) != 2 || m.Labels[0].Label != "household" || len(m.Labels[0].ChunkIndexes) != len(p.Chunks) || m.Labels[1].Label != "insurance" || len(m.Labels[1].ChunkIndexes) != 1 || m.Labels[1].ChunkIndexes[0] != 1 {
		t.Fatalf("lost label provenance %+v", m.Labels)
	}
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	m2, err := MergeChunks(p, results)
	if err != nil || m.Digest != m2.Digest {
		t.Fatal("nondeterministic merge", err)
	}
	var legacy map[string]any
	json.Unmarshal(extractionBytes(validExtraction(p.Chunks[0])), &legacy)
	delete(legacy, "labels")
	b, _ := json.Marshal(legacy)
	if _, err := ValidateChunk(p, p.Chunks[0], b); err == nil {
		t.Fatal("v1 response silently upgraded")
	}
}

func TestUnderstandingReviewIndependentBillSection(t *testing.T) {
	p, _ := PlanChunks(understandingParse("体检报告 145.6 g/L\n独立账单INV-123 20 CNY"))
	c := p.Chunks[0]
	v := c.Owned[0]
	e := validExtraction(c)
	ref := func(a, b int) EvidenceReference {
		return EvidenceReference{ViewID: v.ViewID, Start: a, End: b, Quote: string([]rune(v.Text)[a:b])}
	}
	e.Events = []EventProposal{{ID: "report", Type: "report", FieldRefs: []string{}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{ref(0, 4)}}, {ID: "bill", Type: "invoice", FieldRefs: []string{}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{ref(15, 26)}}}
	e.Coverage[0].Disposition = "extracted"
	r, err := ValidateChunk(p, c, extractionBytes(e))
	if err != nil {
		t.Fatal(err)
	}
	m, err := MergeChunks(p, []ChunkUnderstanding{r})
	if err != nil || len(m.Events) != 2 {
		t.Fatal("independent bill merged into report", err)
	}
}

func TestUnderstandingReviewOverlapBounded(t *testing.T) {
	events := make([]MergedEvent, 5000)
	for i := range events {
		events[i] = MergedEvent{Type: "report", CanonicalEvidence: []CanonicalEvidence{{ParseDigest: "p", ComponentID: "body", Start: i * 2, End: i*2 + 1}}}
	}
	done := make(chan bool, 1)
	go func() { done <- overlappingReportAnchors(events) }()
	select {
	case overlap := <-done:
		if overlap {
			t.Fatal("disjoint events overlap")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("5000 bounded events exceeded overlap scan deadline")
	}
	same := []MergedEvent{{Type: "report", CanonicalEvidence: []CanonicalEvidence{{ComponentID: "body", Start: 0, End: 10}, {ComponentID: "body", Start: 2, End: 8}}}, {Type: "report", CanonicalEvidence: []CanonicalEvidence{{ComponentID: "body", Start: 10, End: 12}}}}
	if overlappingReportAnchors(same) {
		t.Fatal("internal overlap or touching ranges misclassified")
	}
	same[1].CanonicalEvidence[0].Start = 9
	if !overlappingReportAnchors(same) {
		t.Fatal("cross-event overlap lost")
	}
}

func TestUnderstandingReviewNativeSectionSubset(t *testing.T) {
	for _, tc := range []string{"unique", "same_title_twice", "different_title", "two_dates", "date_alternative", "date_review", "birth_date", "bare_iso", "outside_header", "different_page", "extra_birth_evidence"} {
		t.Run(tc, func(t *testing.T) {
			native := func(id, text string, y float64) Segment {
				return Segment{ID: id, Text: text, Locator: Locator{Page: &PageLocation{Page: 1, Rect: Rect{0.05, y, 0.5, 0.03}}}, Visual: &VisualEvidence{State: "ocr_only", Alternatives: []VisualAlternative{{Origin: "ocr", Text: text}}}}
			}
			p0 := understandingParse("")
			p0.ParserVersion = NativeParserVersion
			p0.Native = &NativeMetadata{}
			p0.Segments = []Segment{native("title", "健康报告 HEALTH REPORT", 0.05), native("date", "报告日期：2026-10-04", 0.2), native("birth", "出生日期：1988-03-12", 0.15)}
			switch tc {
			case "same_title_twice":
				p0.Segments = append(p0.Segments, native("other-title", "健康报告 HEALTH REPORT", 0.5))
			case "different_title":
				p0.Segments = append(p0.Segments, native("other-title", "教育报告 SCHOOL REPORT", 0.5))
			case "two_dates":
				p0.Segments = append(p0.Segments, native("other-date", "报告日期：2026-11-04", 0.6))
			case "date_alternative":
				p0.Segments[1].Visual.Alternatives = append(p0.Segments[1].Visual.Alternatives, VisualAlternative{Origin: "native", Text: "报告日期：2026-11-04"})
			case "date_review":
				p0.Segments[1].Visual.RequiresReview = true
			case "birth_date":
				p0.Segments[1] = native("date", "出生日期：2026-10-04", 0.2)
			case "bare_iso":
				p0.Segments[1] = native("date", "2026-10-04", 0.2)
			case "outside_header":
				p0.Segments[1].Locator.Page.Rect[1] = 0.6
			case "different_page":
				p0.Segments[1].Locator.Page.Page = 2
			}
			p, err := PlanChunks(p0)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Chunks) != 1 {
				t.Fatal("fixture split")
			}
			c := p.Chunks[0]
			views := map[string]EvidenceView{}
			for _, v := range c.Owned {
				if _, ok := views[v.ParentSegmentID]; !ok {
					views[v.ParentSegmentID] = v
				}
			}
			ref := func(v EvidenceView) EvidenceReference {
				return EvidenceReference{ViewID: v.ViewID, Start: v.Start, End: v.End, Quote: v.Text}
			}
			title, date := ref(views["title"]), ref(views["date"])
			e := validExtraction(c)
			e.Events = []EventProposal{{ID: "a", Type: "report", FieldRefs: []string{}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{title}}, {ID: "b", Type: "report", FieldRefs: []string{}, SubjectRefs: []string{}, SectionEvidence: []EvidenceReference{title, date}}}
			if tc == "extra_birth_evidence" {
				e.Events[1].SectionEvidence = append(e.Events[1].SectionEvidence, ref(views["birth"]))
			}
			used := map[string]bool{}
			for _, ev := range e.Events {
				for _, r := range ev.SectionEvidence {
					used[r.ViewID] = true
				}
			}
			for i, u := range e.Coverage {
				if used[u.ViewID] {
					e.Coverage[i].Disposition = "extracted"
				}
			}
			r, err := ValidateChunk(p, c, extractionBytes(e))
			if err != nil {
				t.Fatal(err)
			}
			m, err := MergeChunks(p, []ChunkUnderstanding{r})
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if tc == "unique" {
				want = 1
			}
			if len(m.Events) != want {
				t.Fatalf("events=%d want=%d", len(m.Events), want)
			}
			if len(m.Chunks[0].Result.Events[0].SectionEvidence) != 1 || len(m.Chunks[0].Result.Events[1].SectionEvidence) < 2 {
				t.Fatal("original proposal evidence lost")
			}
			if want == 2 && !m.RequiresReview {
				t.Fatal("overlapping report subset silently accepted")
			}
		})
	}
}
