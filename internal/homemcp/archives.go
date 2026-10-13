package homemcp

import (
	"context"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
	"reflect"
	"strings"
)

func isArchiveTool(name string) bool {
	switch name {
	case "home_analyze_archives", "home_search_archives", "home_get_archive_evidence", "home_search_archive_entities", "home_get_archive_task", "home_get_archive_question", "home_answer_archive_question", "home_revise_archive":
		return true
	}
	return false
}
func archiveField(k string) map[string]any {
	switch k {
	case "operation":
		return map[string]any{"type": "string", "enum": []string{"count", "sum", "latest", "series", "ratio"}}
	case "count":
		return map[string]any{"type": "string", "enum": []string{"documents", "events", "observations"}}
	case "metric":
		return map[string]any{"type": "string", "minLength": 1, "maxLength": 128}
	case "question_version", "waiting_task_version", "document_revision":
		return map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991}
	case "action":
		return map[string]any{"type": "string", "enum": []string{"keep_current", "select_subject", "create_subject"}}
	case "kind":
		return map[string]any{"type": "string", "enum": []string{"value", "reassign", "private", "withdraw"}}
	case "reason":
		return map[string]any{"type": "string", "minLength": 1, "maxLength": 1024}
	case "value":
		return map[string]any{"type": "string", "maxLength": 16384}
	case "name":
		return map[string]any{"type": "string", "minLength": 1, "maxLength": 256}

	case "query":
		return map[string]any{"type": "string", "maxLength": 256}
	case "document_type":
		return map[string]any{"type": "string", "maxLength": 128}
	case "document_id":
		return map[string]any{"type": "string", "pattern": "^(legacy:)?[A-Za-z0-9][A-Za-z0-9_-]{0,127}$"}
	case "domain":
		return map[string]any{"type": "string", "enum": []string{"health", "education", "finance", "property", "other", "unknown"}}
	case "ownership":
		return map[string]any{"type": "string", "enum": []string{"confirmed", "household", "waiting", "unassigned"}}
	case "date_from", "date_to":
		return map[string]any{"type": "string", "format": "date", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"}
	case "limit":
		return map[string]any{"type": "integer", "minimum": 1, "maximum": 25, "default": 10}
	case "cursor":
		return map[string]any{"type": "string", "maxLength": 4096}
	default:
		return field(k)
	}
}
func (a *adapter) archiveCall(ctx context.Context, d toolDef, in args) *mcp.CallToolResult {
	if d.name == "home_analyze_archives" {
		return a.archiveAnalysis(ctx, in)
	}
	if d.name == "home_get_archive_task" || d.name == "home_get_archive_question" || d.mutation {
		return a.archiveLifecycle(ctx, d, in)
	}
	f, e := (archive.SearchFilter{Query: in.Query, EntityID: in.EntityID, Domain: in.Domain, DocumentType: in.DocumentType, DateFrom: in.DateFrom, DateTo: in.DateTo, Ownership: in.Ownership, Limit: in.Limit, Cursor: in.Cursor}).Normalize()
	if e != nil {
		return failure("invalid_arguments", "Invalid archive criteria.", "")
	}
	q := url.Values{"limit": {jsonNumber(f.Limit)}}
	for k, v := range map[string]string{"query": f.Query, "entity_id": f.EntityID, "domain": f.Domain, "document_type": f.DocumentType, "date_from": f.DateFrom, "date_to": f.DateTo, "ownership": f.Ownership, "cursor": f.Cursor} {
		if v != "" {
			q.Set(k, v)
		}
	}
	path := "archives/search"
	if d.name == "home_search_archive_entities" {
		path = "archives/entities"
	}
	if d.name == "home_get_archive_evidence" {
		path = "archives/documents/" + url.PathEscape(in.DocumentID) + "/evidence"
	}
	b, status, e := a.request(ctx, "GET", path+"?"+q.Encode(), nil)
	if e != nil {
		return transportFailure(e, "")
	}
	if status < 200 || status >= 300 {
		return backendError(b, status)
	}
	// Decode into an explicit DTO; backend-private additions cannot reach a model.
	if strings.Contains(d.name, "entities") {
		var env integration.Response[integration.ArchiveEntities]
		if !archiveBackendFields[integration.Response[archive.EntityPage]](b) {
			return failure("invalid_response", "Incomplete archive response.", "")
		}
		if json.Unmarshal(b, &env) != nil || env.SchemaVersion != "1" || env.ObservedAt.IsZero() || env.Availability != "available" {
			return failure("invalid_response", "Invalid archive response.", "")
		}
		env.Data.SchemaVersion = integration.ArchiveQueryVersion
		return result(env, false)
	}
	var env integration.Response[integration.ArchiveSearch]
	if !archiveBackendFields[integration.Response[archive.SearchPage]](b) {
		return failure("invalid_response", "Incomplete archive response.", "")
	}
	if json.Unmarshal(b, &env) != nil || env.SchemaVersion != "1" || env.ObservedAt.IsZero() || env.Availability != "available" {
		return failure("invalid_response", "Invalid archive response.", "")
	}
	env.Data.SchemaVersion = integration.ArchiveQueryVersion
	return result(env, false)
}

func (a *adapter) archiveLifecycle(ctx context.Context, d toolDef, in args) *mcp.CallToolResult {
	method, path := "GET", "archives/tasks/"+url.PathEscape(in.TaskID)
	var body any
	switch d.name {
	case "home_get_archive_question":
		path += "/question"
	case "home_answer_archive_question":
		method = "POST"
		path += "/answer"
		if (in.Action == "keep_current" && (in.Target != "" || in.Name != "")) || (in.Action == "select_subject" && (in.Target == "" || in.Name != "")) || (in.Action == "create_subject" && (strings.TrimSpace(in.Name) == "" || in.Target != "")) {
			return failure("invalid_arguments", "Invalid clarification action.", "")
		}
		body = map[string]any{"question_id": in.QuestionID, "question_version": in.QuestionVersion, "waiting_task_version": in.WaitingTaskVersion, "extraction_id": in.ExtractionID, "document_revision": in.DocumentRevision, "resolution_key": in.ResolutionKey, "action": in.Action, "target": in.Target, "name": in.Name}
	case "home_revise_archive":
		method = "POST"
		path = "archives/documents/" + url.PathEscape(in.DocumentID) + "/revisions"
		if strings.TrimSpace(in.Reason) == "" || ((in.Kind == "value" || in.Kind == "reassign") && in.ObservationID == "") || (in.Kind == "reassign" && in.EntityID == "") || (in.Kind != "value" && in.Value != "") || (in.Kind != "reassign" && in.EntityID != "") || (in.Kind == "withdraw" && in.ObservationID != "") {
			return failure("invalid_arguments", "Invalid archive correction.", "")
		}
		body = map[string]any{"document_revision": in.DocumentRevision, "kind": in.Kind, "reason": in.Reason, "observation_id": in.ObservationID, "value": in.Value, "entity_id": in.EntityID}
	}
	b, status, e := a.request(ctx, method, path, body)
	if e != nil {
		if d.mutation {
			return failure("outcome_unknown", "Archive change unconfirmed; query current state before another change.", "")
		}
		return transportFailure(e, "")
	}
	if status < 200 || status >= 300 {
		return backendError(b, status)
	}
	switch d.name {
	case "home_get_archive_question":
		return archiveLifecycleResult[integration.ArchiveQuestion](b)
	case "home_revise_archive":
		return archiveLifecycleResult[integration.ArchiveRevisionResult](b)
	default:
		return archiveLifecycleResult[domain.ArchiveTaskProjection](b)
	}
}
func archiveLifecycleResult[T any](b []byte) *mcp.CallToolResult {
	var env integration.Response[T]
	if !archiveBackendFields[integration.Response[T]](b) {
		return failure("invalid_response", "Incomplete archive response.", "")
	}
	if json.Unmarshal(b, &env) != nil || env.SchemaVersion != "1" || env.ObservedAt.IsZero() || env.Availability != "available" {
		return failure("invalid_response", "Invalid archive response.", "")
	}
	return result(integration.Response[integration.ArchiveData[T]]{SchemaVersion: env.SchemaVersion, ObservedAt: env.ObservedAt, Availability: env.Availability, Data: integration.ArchiveData[T]{SchemaVersion: integration.ArchiveQueryVersion, Result: env.Data}}, false)
}

func archiveBackendFields[T any](b []byte) bool {
	var v T
	return integration.RequiredFields(b, reflect.TypeOf(v))
}

func (a *adapter) archiveAnalysis(ctx context.Context, in args) *mcp.CallToolResult {
	q := archive.AnalysisQuery{Ownership: in.Ownership, Operation: in.Operation, Count: in.Count, Metric: in.Metric, EntityID: in.EntityID, Domain: in.Domain, DocumentType: in.DocumentType, Query: in.Query, DateFrom: in.DateFrom, DateTo: in.DateTo, Timezone: "UTC"}
	if _, e := q.Normalize(); e != nil {
		return failure("invalid_arguments", "Invalid analysis criteria.", "")
	}
	values := url.Values{}
	for k, v := range map[string]string{"ownership": in.Ownership, "operation": in.Operation, "count": in.Count, "metric": in.Metric, "entity_id": in.EntityID, "domain": in.Domain, "document_type": in.DocumentType, "query": in.Query, "date_from": in.DateFrom, "date_to": in.DateTo} {
		if v != "" {
			values.Set(k, v)
		}
	}
	b, status, e := a.request(ctx, "GET", "archives/analysis?"+values.Encode(), nil)
	if e != nil {
		return transportFailure(e, "")
	}
	if status < 200 || status >= 300 {
		return backendError(b, status)
	}
	var env integration.Response[archive.AnalysisResult]
	if !archiveBackendFields[integration.Response[archive.AnalysisResult]](b) || json.Unmarshal(b, &env) != nil || env.SchemaVersion != "1" || env.ObservedAt.IsZero() || env.Availability != "available" || env.Data.SchemaVersion != archive.AnalysisVersion {
		return failure("invalid_response", "Invalid analysis response.", "")
	}
	return result(env, false)
}
