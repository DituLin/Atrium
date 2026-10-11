package homemcp

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"testing"
	"time"
)

func TestArchiveSearchMCPBusinessCriteriaAndCoverage(t *testing.T) {
	calls := 0
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/integrations/archives/search" || r.URL.Query().Get("query") != "体检" || r.URL.Query().Get("domain") != "health" {
			t.Errorf("wrong route %s", r.URL)
		}
		json.NewEncoder(w).Encode(map[string]any{"schema_version": "1", "observed_at": time.Now().UTC(), "availability": "available", "data": map[string]any{"items": []any{}, "coverage": map[string]any{"complete": false, "reason": "indexing"}, "index_status": "indexing"}})
	})
	cs := session(t, cfg)
	out, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_search_archives", Arguments: map[string]any{"query": "体检", "domain": "health"}})
	if e != nil || out.IsError {
		t.Fatalf("search unavailable: %v %v", out, e)
	}
	raw, _ := json.Marshal(out.StructuredContent)
	var env struct {
		Data struct {
			Schema   string `json:"schema_version"`
			Coverage struct {
				Complete bool   `json:"complete"`
				Reason   string `json:"reason"`
			} `json:"coverage"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &env) != nil || env.Data.Schema != "archive-query/1" || env.Data.Coverage.Complete || env.Data.Coverage.Reason != "indexing" {
		t.Fatalf("lost versioned coverage: %s", raw)
	}
	for _, args := range []map[string]any{{"query": "体检", "principal_id": "other"}, {"limit": 26}, {"date_from": "yesterday"}} {
		out, e = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_search_archives", Arguments: args})
		if e == nil && !out.IsError {
			t.Fatal("invalid criteria accepted")
		}
	}
	if calls != 1 {
		t.Fatalf("invalid criteria reached core: %d", calls)
	}
}

func TestArchiveAnswerCarriesAllVersionFences(t *testing.T) {
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/integrations/archives/tasks/task1/answer" {
			t.Errorf("route %s", r.URL)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["resolution_key"] != "host-key" || body["document_revision"] != float64(3) || body["waiting_task_version"] != float64(4) || body["question_version"] != float64(2) || body["extraction_id"] != "extract1" {
			t.Errorf("fences missing %v", body)
		}
		json.NewEncoder(w).Encode(map[string]any{"schema_version": "1", "observed_at": time.Now().UTC(), "availability": "available", "data": map[string]any{"id": "task1", "status": "queued", "stage": "processing", "original_status": "saved", "nas_status": "pending", "created_at": time.Now().UTC(), "updated_at": time.Now().UTC()}})
	})
	cs := session(t, cfg)
	out, e := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "home_answer_archive_question", Arguments: map[string]any{"task_id": "task1", "question_id": "q1", "question_version": 2, "waiting_task_version": 4, "extraction_id": "extract1", "document_revision": 3, "resolution_key": "host-key", "action": "create_subject", "name": "妈妈"}})
	if e != nil || out.IsError {
		t.Fatalf("typed answer unavailable %v %v", out, e)
	}
}

func TestArchiveMissingCoverageNeverBecomesFalseEvidence(t *testing.T) {
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"schema_version": "1", "observed_at": time.Now().UTC(), "availability": "available", "data": map[string]any{"items": []any{}, "index_status": "ready"}})
	})
	out, e := session(t, cfg).CallTool(context.Background(), &mcp.CallToolParams{Name: "home_search_archives", Arguments: map[string]any{}})
	if e != nil {
		t.Fatal(e)
	}
	if !out.IsError {
		t.Fatal("missing coverage accepted")
	}
}
func TestArchiveRevisionRequiresExplicitValue(t *testing.T) {
	cfg := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("missing value reached Core") })
	out, e := session(t, cfg).CallTool(context.Background(), &mcp.CallToolParams{Name: "home_revise_archive", Arguments: map[string]any{"document_id": "doc1", "document_revision": 2, "kind": "value", "reason": "correction", "observation_id": "obs1"}})
	if e == nil && !out.IsError {
		t.Fatal("implicit empty correction accepted")
	}
}
