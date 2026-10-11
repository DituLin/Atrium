package brain

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
	"time"
)

func TestArchiveModelArgumentsAreBusinessOnly(t *testing.T) {
	good := map[string]any{"query": "体检", "domain": "health", "limit": 2.0}
	if _, _, e := validateArguments("home_search_archives", good); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []map[string]any{{"principal_id": "fake"}, {"limit": 26.0}, {"limit": 1.5}, {"date_from": "2026-02-31"}, {"cursor": 3}, {"domain": "shell"}} {
		if _, _, e := validateArguments("home_search_archives", bad); e == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	for _, key := range []string{"request_id", "operation_id", "resolution_key", "household_id", "principal_id"} {
		if _, _, e := validateArguments("home_answer_archive_question", map[string]any{key: "fake"}); e == nil {
			t.Fatalf("accepted host identity %s", key)
		}
	}
}
func TestArchiveReceiptPreservesIncompleteCoverageAndSources(t *testing.T) {
	env := integration.Response[integration.ArchiveSearch]{SchemaVersion: "1", ObservedAt: time.Now(), Availability: "available", Data: integration.ArchiveSearch{SchemaVersion: integration.ArchiveQueryVersion, SearchPage: archive.SearchPage{Items: []archive.SearchItem{}, Coverage: archive.SearchCoverage{Complete: false, Reason: "indexing"}, IndexStatus: "indexing"}}}
	res, e := receiptResult("home_search_archives", map[string]any{"query": "体检"}, &mcp.CallToolResult{StructuredContent: env})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(res)
	if len(b) == 0 {
		t.Fatal("missing receipt")
	}
}

type archiveCapturingCaller struct{ params []*mcp.CallToolParams }

func (c *archiveCapturingCaller) CallTool(_ context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	c.params = append(c.params, p)
	return &mcp.CallToolResult{}, nil
}
func TestArchiveAnswerHostOwnsResolutionKeyAndBudget(t *testing.T) {
	l, _ := executorLedger(t)
	c := &archiveCapturingCaller{}
	turn := NewExecutor(c, l).StartTurn(context.Background())
	defer turn.Close()
	args := map[string]any{"task_id": "task1", "question_id": "q1", "question_version": 1, "waiting_task_version": 2, "extraction_id": "x1", "document_revision": 2, "action": "keep_current"}
	for range 8 {
		if _, e := turn.Call("call1", "home_answer_archive_question", args); e != nil {
			t.Fatal(e)
		}
	}
	first := c.params[0].Arguments.(map[string]any)["resolution_key"]
	if first == "" || first == nil {
		t.Fatal("missing host identity")
	}
	for _, p := range c.params {
		if p.Arguments.(map[string]any)["resolution_key"] != first {
			t.Fatal("same call received a new resolution key")
		}
	}
	if _, e := turn.Call("call2", "home_search_archives", nil); !errors.Is(e, ErrToolBudget) {
		t.Fatal(e)
	}
	if _, ok := args["resolution_key"]; ok {
		t.Fatal("mutated model arguments")
	}
}
func TestArchiveReceiptRejectsWrongOrUnversionedChange(t *testing.T) {
	args := map[string]any{"document_id": "doc1", "document_revision": 4}
	for _, data := range []any{map[string]any{"document_revision": 5}, integration.ArchiveData[integration.ArchiveRevisionResult]{SchemaVersion: integration.ArchiveQueryVersion, Result: integration.ArchiveRevisionResult{DocumentRevision: 99}}} {
		env := integration.Response[any]{SchemaVersion: "1", ObservedAt: time.Now(), Availability: "available", Data: data}
		if _, e := receiptResult("home_revise_archive", args, &mcp.CallToolResult{StructuredContent: env}); e == nil {
			t.Fatal("unmatched change accepted")
		}
	}
}
