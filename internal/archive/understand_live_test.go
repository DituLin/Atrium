package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in, synthetic-only integration. Never reads ordinary agent configuration
// or credentials from disk. The operator supplies one provider key in env.
func TestUnderstandingLivePublicWorker(t *testing.T) {
	if os.Getenv("ATRIUM_UNDERSTANDING_LIVE") != "1" {
		t.Skip("opt-in synthetic provider evaluation")
	}
	if path := os.Getenv("ATRIUM_SYNTHETIC_EVIDENCE"); path != "" {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal("synthetic evidence directory")
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	a := OpenClawUnderstandingAdapter{NodePath: "/opt/homebrew/bin/node", OpenClawPath: "/opt/homebrew/lib/node_modules/openclaw/openclaw.mjs", PluginPath: filepath.Join(root, "adapters/openclaw/archive-worker-plugin"), ProviderPath: os.Getenv("ATRIUM_DEEPSEEK_PLUGIN"), APIKey: os.Getenv("DEEPSEEK_API_KEY"), Timeout: 120 * time.Second}
	samples := []struct{ domain, text, role1, role2, value, date, dimension, dimensionKind string }{
		{"health", "体检报告\n患者：林明\n医生：王华\n日期：2026-10-04\n血红蛋白：145.6 g/L\n", "patient", "doctor", "145.6", "2026-10-04", "g/L", "unit"},
		{"education", "成绩报告\n学生：陈晨\n老师：李芳\n日期：2026-09-20\n数学：96分\n", "student", "teacher", "96", "2026-09-20", "分", "unit"},
		{"finance", "银行交易记录\n账户持有人：周宁\n日期：2026-10-01\n交易金额：130.50元\n收款人：吴平\n", "account_holder", "payee", "130.50", "2026-10-01", "元", "currency"},
		{"finance", "银行交易记录\n账户持有人：周宁\n日期：2026-10-01\n周宁的交易金额：130.50元\n收款人：吴平\n", "account_holder", "payee", "130.50", "2026-10-01", "元", "currency"},
		{"property", "房屋检查报告\n业主：赵青\n工程师：孙涛\n日期：2026-09-30\n建筑面积：88.75平方米\n", "owner", "engineer", "88.75", "2026-09-30", "平方米", "unit"},
	}
	for _, sample := range samples {
		sampleName := sample.domain
		if strings.Contains(sample.text, "周宁的交易金额") {
			sampleName += "_explicit_control"
		}
		t.Run(sampleName, func(t *testing.T) {
			p, err := PlanChunks(understandingParse(sample.text))
			if err != nil {
				t.Fatal(err)
			}
			response, err := a.complete(context.Background(), p.Chunks[0])
			if err != nil {
				t.Fatal(err)
			}

			if path := os.Getenv("ATRIUM_SYNTHETIC_EVIDENCE"); path != "" {
				wire, _ := json.MarshalIndent(response, "", "  ")
				if err := os.WriteFile(filepath.Join(path, sampleName+".json"), wire, 0600); err != nil {
					t.Fatal("synthetic evidence write")
				}
			}
			r, err := validatedResponse(p, p.Chunks[0], response)
			if err != nil {
				t.Fatal(err)
			}
			if r.Result.Domain != sample.domain {
				t.Fatal("domain mismatch")
			}
			roles := map[string]bool{}
			subjectRoles := map[string]string{}
			for _, s := range r.Result.Subjects {
				roles[s.Role] = true
				subjectRoles[s.ID] = s.Role
			}
			if !roles[sample.role1] || !roles[sample.role2] {
				t.Fatal("role mismatch")
			}
			found, dateFound := false, false
			for _, f := range r.Result.Fields {
				if f.Raw == sample.date {
					dateFound = true
				}
				if f.Raw == sample.value {
					unit := false
					for _, d := range f.Dimensions {
						if d.Raw == sample.dimension && d.Kind == sample.dimensionKind {
							unit = true
						}
					}
					bound := sample.domain == "property"
					for _, id := range f.SubjectRefs {
						if subjectRoles[id] == sample.role1 {
							bound = true
						}
					}
					found = unit && bound
				}
			}
			if !found || !dateFound || len(r.Result.Events) != 1 {
				t.Fatal("missing field/event")
			}
			merged, err := MergeChunks(p, []ChunkUnderstanding{r})
			if err != nil || !merged.Complete {
				t.Fatal("coverage incomplete")
			}
			if r.Receipt == nil || r.Receipt.Execution.Mode != "direct-provider" || r.Receipt.Execution.Owner.ID != "deepseek" {
				t.Fatal("missing route proof")
			}
			if path := os.Getenv("ATRIUM_SYNTHETIC_EVIDENCE"); path != "" {
				artifact := struct {
					Text    string            `json:"synthetic_text"`
					Plan    UnderstandingPlan `json:"plan"`
					Product Understanding     `json:"product"`
				}{sample.text, p, merged}
				wire, _ := json.MarshalIndent(artifact, "", "  ")
				if os.WriteFile(filepath.Join(path, sampleName+".validated.json"), wire, 0600) != nil {
					t.Fatal("synthetic artifact write")
				}
			}
			t.Logf("domain=%s subjects=%d fields=%d events=%d complete=true exact_value_date_dimension_binding=true route=%s/%s", sample.domain, len(r.Result.Subjects), len(r.Result.Fields), len(r.Result.Events), r.Receipt.Execution.Mode, r.Receipt.Execution.Owner.ID)
		})
	}
}

func TestUnderstandingNativeHandoff(t *testing.T) {
	path := os.Getenv("ATRIUM_UNDERSTANDING_NATIVE_RESULT")
	if path == "" {
		t.Skip("opt-in retained synthetic T06 native result")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed ParseResult
	if json.Unmarshal(b, &parsed) != nil || !strings.HasPrefix(parsed.ParserVersion, NativeParserVersion+"/") {
		t.Fatal("native fixture version")
	}
	p, err := PlanChunks(parsed)
	if err != nil {
		t.Fatal(err)
	}
	results := []ChunkUnderstanding{}
	hasConflict := false
	for _, c := range p.Chunks {
		for _, v := range c.Owned {
			if v.RequiresReview && v.AlternativeID != "" {
				hasConflict = true
			}
		}
		e := validExtraction(c)
		for i := range e.Coverage {
			e.Coverage[i].Disposition = "insufficient"
		}
		r, err := ValidateChunk(p, c, extractionBytes(e))
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}
	m, err := MergeChunks(p, results)
	if err != nil || !hasConflict || !m.RequiresReview || m.Complete || m.Native == nil {
		t.Fatal("native uncertainty lost", err)
	}
}
func TestUnderstandingNativeReportContext(t *testing.T) {
	path := os.Getenv("ATRIUM_UNDERSTANDING_NATIVE_REPORT")
	if path == "" {
		t.Skip("opt-in retained actual synthetic native report")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed ParseResult
	if json.Unmarshal(b, &parsed) != nil {
		t.Fatal("fixture")
	}
	p, err := PlanChunks(parsed)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	found := false
	for _, c := range p.Chunks {
		ownValue := false
		for _, v := range c.Owned {
			if seen[v.ViewID] {
				t.Fatal("duplicate ownership")
			}
			seen[v.ViewID] = true
			if v.Text == "-2.75" {
				ownValue = true
			}
		}
		if ownValue {
			found = true
			text := ""
			for _, v := range c.Evidence() {
				text += v.Text + "\n"
			}
			for _, want := range []string{"变化 Delta", "mmol/L", "姓名：林明"} {
				if !strings.Contains(text, want) {
					t.Fatalf("row/header context missing: %s", want)
				}
			}
		}
		for _, v := range c.Context {
			for _, owned := range c.Owned {
				if v.ViewID == owned.ViewID {
					t.Fatal("context owns")
				}
			}
		}
	}
	if !found {
		t.Fatal("numeric fixture missing")
	}
}
func TestUnderstandingLiveNativeReport(t *testing.T) {
	if os.Getenv("ATRIUM_UNDERSTANDING_LIVE") != "1" || os.Getenv("ATRIUM_UNDERSTANDING_NATIVE_REPORT") == "" {
		t.Skip("opt-in synthetic native/model handoff")
	}
	b, err := os.ReadFile(os.Getenv("ATRIUM_UNDERSTANDING_NATIVE_REPORT"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed ParseResult
	if json.Unmarshal(b, &parsed) != nil {
		t.Fatal("fixture")
	}
	p, err := PlanChunks(parsed)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs("../..")
	a := OpenClawUnderstandingAdapter{NodePath: "/opt/homebrew/bin/node", OpenClawPath: "/opt/homebrew/lib/node_modules/openclaw/openclaw.mjs", PluginPath: filepath.Join(root, "adapters/openclaw/archive-worker-plugin"), ProviderPath: os.Getenv("ATRIUM_DEEPSEEK_PLUGIN"), APIKey: os.Getenv("DEEPSEEK_API_KEY"), Timeout: 120 * time.Second}
	results := []ChunkUnderstanding{}
	for _, c := range p.Chunks {
		response, err := a.complete(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		if path := os.Getenv("ATRIUM_SYNTHETIC_EVIDENCE"); path != "" {
			b, _ := json.MarshalIndent(response, "", "  ")
			if os.WriteFile(filepath.Join(path, fmt.Sprintf("native-report-chunk-%d.json", c.Index)), b, 0600) != nil {
				t.Fatal("artifact")
			}
		}
		r, err := validatedResponse(p, c, response)
		if err != nil {
			t.Fatalf("chunk %d: %v", c.Index, err)
		}
		results = append(results, r)
	}
	merged, err := MergeChunks(p, results)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		for _, f := range r.Result.Fields {
			if f.Raw == "-2.75" {
				for _, d := range f.Dimensions {
					if d.Kind == "unit" && d.Raw == "mmol/L" {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("delta/label/unit relationship missing")
	}
	if len(merged.Events) != 1 {
		t.Fatalf("report anchors differ: %d events", len(merged.Events))
	}
	assertSyntheticNativeReport(t, merged)
	if path := os.Getenv("ATRIUM_SYNTHETIC_EVIDENCE"); path != "" {
		artifact := struct {
			Plan    UnderstandingPlan `json:"plan"`
			Product Understanding     `json:"product"`
		}{p, merged}
		b, _ := json.MarshalIndent(artifact, "", "  ")
		if os.WriteFile(filepath.Join(path, "native-report.validated.json"), b, 0600) != nil {
			t.Fatal("artifact")
		}
	}
	t.Logf("actual_native_segments=%d chunks=%d merged_report_events=%d delta_unit_bound=true", len(parsed.Segments), len(p.Chunks), len(merged.Events))
}
func TestUnderstandingNativeReportReplay(t *testing.T) {
	path, dir := os.Getenv("ATRIUM_UNDERSTANDING_NATIVE_REPORT"), os.Getenv("ATRIUM_SYNTHETIC_EVIDENCE")
	if path == "" || dir == "" {
		t.Skip("opt-in actual synthetic response replay")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed ParseResult
	if json.Unmarshal(b, &parsed) != nil {
		t.Fatal("fixture")
	}
	p, err := PlanChunks(parsed)
	if err != nil {
		t.Fatal(err)
	}
	results := []ChunkUnderstanding{}
	for _, c := range p.Chunks {
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("native-report-chunk-%d.json", c.Index)))
		if err != nil {
			t.Fatal(err)
		}
		var response WorkerResponse
		if strictJSON(data, &response) != nil {
			t.Fatal("response schema")
		}
		wire := c
		wire.InvocationID = response.InvocationID
		wire.InputDigest = wire.inputDigest()
		if err := validateWorkerResponse(wire, response); err != nil {
			t.Fatal(err)
		}
		r, err := validatedResponse(p, c, response)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}
	merged, err := MergeChunks(p, results)
	if err != nil || len(merged.Events) != 1 {
		t.Fatalf("report anchors not linked: %d %v", len(merged.Events), err)
	}
	assertSyntheticNativeReport(t, merged)
	artifact := struct {
		Plan    UnderstandingPlan `json:"plan"`
		Product Understanding     `json:"product"`
	}{p, merged}
	data, _ := json.MarshalIndent(artifact, "", "  ")
	if os.WriteFile(filepath.Join(dir, "native-report.validated.json"), data, 0600) != nil {
		t.Fatal("artifact")
	}
}

// This evaluator is specific to the retained synthetic image, not a general
// rule that every compound document has one event. The image has a single
// report title and a shared item/value/unit table, with no separate bill section.
func assertSyntheticNativeReport(t *testing.T, m Understanding) {
	t.Helper()
	if !m.Complete || len(m.Events) != 1 || m.Events[0].Type != "report" {
		t.Fatal("synthetic single report incomplete or split")
	}
	expected := map[string]string{"1988-03-12": "", "2026-10-04": "", "145.6": "g/L", "1234.50": "CNY", "-2.75": "mmol/L"}
	found := map[string]bool{}
	patient, doctor := false, false
	linked := map[ScopedLocalReference]bool{}
	for _, r := range m.Events[0].FieldRefs {
		linked[r] = true
	}
	for _, c := range m.Chunks {
		roles := map[string]string{}
		for _, s := range c.Result.Subjects {
			roles[s.ID] = s.Role
			patient = patient || (s.Role == "patient" && strings.Contains(s.Name, "林明"))
			doctor = doctor || (s.Role == "doctor" && strings.Contains(s.Name, "王华"))
		}
		for _, f := range c.Result.Fields {
			dim, ok := expected[f.Raw]
			if !ok {
				continue
			}
			if !linked[ScopedLocalReference{c.Index, f.ID}] {
				t.Fatal("report field not linked")
			}
			dimensionOK := dim == ""
			for _, d := range f.Dimensions {
				kind := "unit"
				if dim == "CNY" {
					kind = "currency"
				}
				dimensionOK = dimensionOK || (d.Raw == dim && d.Kind == kind)
			}
			bound := f.Raw != "1988-03-12" && f.Raw != "145.6" && f.Raw != "-2.75"
			for _, id := range f.SubjectRefs {
				bound = bound || roles[id] == "patient"
			}
			labelOK := f.Raw != "-2.75"
			for _, r := range f.Evidence {
				labelOK = labelOK || strings.Contains(r.Quote, "变化")
			}
			found[f.Raw] = dimensionOK && bound && labelOK
		}
	}
	if !patient || !doctor {
		t.Fatal("native subject role missing")
	}
	for raw := range expected {
		if !found[raw] {
			t.Fatal("native expected field/dimension/binding missing")
		}
	}
}
