package archive

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// SearchFilter contains business criteria only. Identity and projection are
// supplied by the authenticated host, never by the model's query arguments.
type SearchFilter struct {
	Query        string `json:"query,omitempty"`
	EntityID     string `json:"entity_id,omitempty"`
	Domain       string `json:"domain,omitempty"`
	DocumentType string `json:"document_type,omitempty"`
	DateFrom     string `json:"date_from,omitempty"`
	DateTo       string `json:"date_to,omitempty"`
	Ownership    string `json:"ownership,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
}

func (f SearchFilter) Normalize() (SearchFilter, error) {
	f.Query = strings.TrimSpace(f.Query)
	if f.Limit == 0 {
		f.Limit = 10
	}
	if f.Limit < 1 || f.Limit > 25 || len(f.Query) > 256 || !utf8.ValidString(f.Query) || strings.ContainsRune(f.Query, 0) || len(f.Cursor) > 4096 || len(f.EntityID) > 128 || len(f.DocumentType) > 128 {
		return f, fmt.Errorf("invalid search bounds")
	}
	switch f.Domain {
	case "", "health", "education", "finance", "property", "other", "unknown":
	default:
		return f, fmt.Errorf("invalid domain")
	}
	switch f.Ownership {
	case "", "confirmed", "household", "waiting", "unassigned":
	default:
		return f, fmt.Errorf("invalid ownership")
	}
	for _, v := range []string{f.DateFrom, f.DateTo} {
		if v != "" {
			if _, e := time.Parse("2006-01-02", v); e != nil {
				return f, fmt.Errorf("invalid date")
			}
		}
	}
	if f.DateFrom != "" && f.DateTo != "" && f.DateFrom > f.DateTo {
		return f, fmt.Errorf("invalid date interval")
	}
	return f, nil
}

// LiteralFTS encloses the entire input in one quoted phrase; operators, quotes,
// punctuation and prefixes never acquire user-controlled FTS grammar.
func LiteralFTS(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

type SearchEvidence struct {
	FieldName            string          `json:"field_name,omitempty"`
	SourceNormalizations []Normalization `json:"source_normalizations,omitempty"`
	ComponentID          string          `json:"component_id,omitempty"`
	ComponentKind        string          `json:"component_kind,omitempty"`
	AlternativeID        string          `json:"alternative_id,omitempty"`
	AssertedValue        string          `json:"asserted_value,omitempty"`
	AssertionID          string          `json:"assertion_id,omitempty"`
	SourceKind           string          `json:"source_kind,omitempty"`
	LegacyRecordID       string          `json:"legacy_record_id,omitempty"`
	DocumentID           string          `json:"document_id"`
	PublicationID        string          `json:"publication_id"`
	PublicationRevision  int64           `json:"publication_revision"`
	ObservationID        string          `json:"observation_id,omitempty"`
	ObservationRevision  int64           `json:"observation_revision,omitempty"`
	SourceExtractionID   string          `json:"source_extraction_id"`
	SegmentID            string          `json:"segment_id"`
	Locator              Locator         `json:"locator"`
	Start                int             `json:"start"`
	End                  int             `json:"end"`
	Excerpt              string          `json:"excerpt"`
	Confirmed            bool            `json:"confirmed"`
}
type SearchItem struct {
	Processing       string             `json:"processing,omitempty"`
	TaskSummary      *ScreenTaskSummary `json:"task_summary,omitempty"`
	OriginalSaved    bool               `json:"original_saved,omitempty"`
	TaskStatus       string             `json:"task_status,omitempty"`
	ReviewRequired   bool               `json:"review_required"`
	DocumentID       string             `json:"document_id"`
	PublicationID    string             `json:"publication_id,omitempty"`
	DocumentRevision int64              `json:"document_revision"`
	Domain           string             `json:"domain"`
	DocumentType     string             `json:"document_type"`
	Ownership        string             `json:"ownership"`
	ParseStatus      string             `json:"parse_status"`
	EntityIDs        []string           `json:"entity_ids"`
	Dates            []string           `json:"dates"`
	Evidence         []SearchEvidence   `json:"evidence"`
	MetadataOnly     bool               `json:"metadata_only"`
	Truncated        bool               `json:"truncated"`
}
type SearchCoverage struct {
	Complete bool   `json:"complete"`
	Reason   string `json:"reason,omitempty"`
}
type SearchPage struct {
	QueryRef    string         `json:"query_ref,omitempty"`
	Generation  string         `json:"generation,omitempty"`
	Items       []SearchItem   `json:"items"`
	NextCursor  string         `json:"next_cursor,omitempty"`
	Coverage    SearchCoverage `json:"coverage"`
	IndexStatus string         `json:"index_status"`
}

type SearchEntity struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Names   []string `json:"names"`
	Aliases []string `json:"aliases"`
}
type EntityPage struct {
	Items      []SearchEntity `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Coverage   SearchCoverage `json:"coverage"`
}
