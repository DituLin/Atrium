package homemcp

import (
	"strings"
	"testing"
)

func TestArchiveEntityToolDoesNotAdvertiseDocumentFilters(t *testing.T) {
	entity := description(toolDef{name: "home_search_archive_entities"})
	if strings.Contains(entity, "separate domain filter") || !strings.Contains(entity, "entity_id") {
		t.Fatal("entity guidance advertises unsupported document filters")
	}
	evidence := description(toolDef{name: "home_get_archive_evidence"})
	if strings.Contains(evidence, "separate domain filter") || !strings.Contains(evidence, "document_id") {
		t.Fatal("evidence guidance must require an actual document ID")
	}
}
