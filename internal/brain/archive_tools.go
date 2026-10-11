package brain

import (
	"encoding/json"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/integration"
	"strings"
	"unicode/utf8"
)

func isArchiveTool(name string) bool {
	switch name {
	case "home_search_archives", "home_search_archive_entities", "home_get_archive_evidence", "home_get_archive_task", "home_get_archive_question", "home_answer_archive_question", "home_revise_archive":
		return true
	}
	return false
}
func validateArchiveArguments(name string, out map[string]any) (map[string]any, *Action, error) {
	bad := func() (map[string]any, *Action, error) { return nil, nil, ErrInvalidOutcome }
	for k, v := range out {
		switch k {
		case "limit", "document_revision", "question_version", "waiting_task_version":
			b, e := json.Marshal(v)
			var n int64
			if e != nil || json.Unmarshal(b, &n) != nil || n < 1 || n > 9007199254740991 || (k == "limit" && n > 25) {
				return bad()
			}
			out[k] = n
		default:
			s, ok := v.(string)
			if !ok || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
				return bad()
			}
			max := 128
			switch k {
			case "query", "name":
				max = 256
			case "cursor":
				max = 4096
			case "reason":
				max = 1024
			case "value":
				max = 16384
			}
			if len(s) > max {
				return bad()
			}
			switch k {
			case "entity_id", "task_id", "question_id", "extraction_id", "observation_id", "target":
				if !identifier.MatchString(s) {
					return bad()
				}
			case "document_id":
				if !identifier.MatchString(strings.TrimPrefix(s, "legacy:")) {
					return bad()
				}
			}
		}
	}
	required := []string{}
	switch name {
	case "home_get_archive_task", "home_get_archive_question":
		required = []string{"task_id"}
	case "home_get_archive_evidence":
		required = []string{"document_id"}
	case "home_answer_archive_question":
		required = []string{"task_id", "question_id", "question_version", "waiting_task_version", "extraction_id", "document_revision", "action"}
	case "home_revise_archive":
		required = []string{"document_id", "document_revision", "kind", "reason"}
	}
	for _, k := range required {
		if v, ok := out[k]; !ok || v == "" {
			return bad()
		}
	}
	if name == "home_search_archives" || name == "home_search_archive_entities" || name == "home_get_archive_evidence" {
		raw, _ := json.Marshal(out)
		var f archive.SearchFilter
		if json.Unmarshal(raw, &f) != nil {
			return bad()
		}
		if _, e := f.Normalize(); e != nil {
			return bad()
		}
	}
	text := func(k string) string { s, _ := out[k].(string); return s }
	if name == "home_answer_archive_question" {
		switch text("action") {
		case "keep_current":
			if text("target") != "" || text("name") != "" {
				return bad()
			}
		case "select_subject":
			if text("target") == "" || text("name") != "" {
				return bad()
			}
		case "create_subject":
			if strings.TrimSpace(text("name")) == "" || text("target") != "" {
				return bad()
			}
		default:
			return bad()
		}
	}
	if name == "home_revise_archive" {
		kind := text("kind")
		if strings.TrimSpace(text("reason")) == "" {
			return bad()
		}
		switch kind {
		case "value":
			if text("observation_id") == "" {
				return bad()
			}
			if _, ok := out["value"]; !ok {
				return bad()
			}
		case "reassign":
			if text("observation_id") == "" || text("entity_id") == "" {
				return bad()
			}
		case "private":
		case "withdraw":
			if text("observation_id") != "" {
				return bad()
			}
		default:
			return bad()
		}
		if kind != "value" && text("value") != "" || kind != "reassign" && text("entity_id") != "" {
			return bad()
		}
	}
	return out, nil, nil
}
func validArchiveReceipt(tool string, args map[string]any, data any) bool {
	switch d := data.(type) {
	case *integration.ArchiveSearch:
		if d.SchemaVersion != integration.ArchiveQueryVersion || len(d.Items) > 25 || d.IndexStatus == "" || (!d.Coverage.Complete && d.Coverage.Reason == "") {
			return false
		}
		for _, item := range d.Items {
			if item.DocumentID == "" || item.DocumentRevision < 1 {
				return false
			}
			if tool == "home_get_archive_evidence" && item.DocumentID != args["document_id"] {
				return false
			}
			for _, ev := range item.Evidence {
				if ev.DocumentID == "" || ev.End < ev.Start || ev.Start < 0 || ev.SegmentID == "" || ev.AssertedValue != "" && ev.AssertionID == "" {
					return false
				}
			}
		}
		return true
	case *integration.ArchiveEntities:
		return d.SchemaVersion == integration.ArchiveQueryVersion && len(d.Items) <= 25 && (d.Coverage.Complete || d.Coverage.Reason != "")
	case *integration.ArchiveData[domain.ArchiveTaskProjection]:
		t := d.Result
		if d.SchemaVersion != integration.ArchiveQueryVersion || t.ID != args["task_id"] || t.CreatedAt.IsZero() || t.UpdatedAt.IsZero() || t.OriginalStatus != "saved" || (t.NASStatus != "pending" && t.NASStatus != "synced") {
			return false
		}
		switch string(t.Status) {
		case "queued", "running", "retry_wait", "waiting_input", "succeeded", "failed", "cancelled":
			return true
		}
		return false
	case *integration.ArchiveData[integration.ArchiveQuestion]:
		q := d.Result
		return d.SchemaVersion == integration.ArchiveQueryVersion && q.ID != "" && q.Version > 0 && q.WaitingTaskVersion > 0 && q.DocumentRevision > 0 && q.DocumentID != "" && q.ExtractionID != "" && len(q.Targets) <= 64 && len(q.Actions) > 0
	case *integration.ArchiveData[integration.ArchiveRevisionResult]:
		raw, _ := json.Marshal(args["document_revision"])
		var n int64
		return d.SchemaVersion == integration.ArchiveQueryVersion && json.Unmarshal(raw, &n) == nil && d.Result.DocumentRevision == n+1
	}
	return false
}
