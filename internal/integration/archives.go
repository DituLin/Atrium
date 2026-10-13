package integration

import "github.com/DituLin/Atrium/internal/archive"

// ArchiveQueryVersion versions coverage and provenance independently of the
// existing integration transport envelope.
const ArchiveQueryVersion = "archive-query/1"

// ArchiveSearch is a versioned page of authorized archive matches with coverage
// and continuation metadata.
type ArchiveSearch struct {
	SchemaVersion string `json:"schema_version"`
	archive.SearchPage
}

// ArchiveEntities is a versioned page of authorized people or properties with
// coverage and continuation metadata.
type ArchiveEntities struct {
	SchemaVersion string `json:"schema_version"`
	archive.EntityPage
}

// ArchiveData is the typed versioned payload for lifecycle operations.
type ArchiveData[T any] struct {
	SchemaVersion string `json:"schema_version"`
	Result        T      `json:"result"`
}

// ArchiveQuestion exposes a clarification's permitted actions and targets,
// together with the document and task revisions needed to answer it safely.
type ArchiveQuestion struct {
	SubjectName        string            `json:"subject_name,omitempty"`
	SubjectRole        string            `json:"subject_role,omitempty"`
	TargetLabels       map[string]string `json:"target_labels,omitempty"`
	TargetsTruncated   bool              `json:"targets_truncated,omitempty"`
	EntityKind         string            `json:"entity_kind,omitempty"`
	ID                 string            `json:"question_id"`
	Version            int64             `json:"question_version"`
	WaitingTaskVersion int64             `json:"waiting_task_version"`
	ExtractionID       string            `json:"extraction_id"`
	DocumentID         string            `json:"document_id"`
	DocumentRevision   int64             `json:"document_revision"`
	ResolutionRevision int64             `json:"resolution_revision"`
	MentionRef         string            `json:"mention_ref,omitempty"`
	Actions            []string          `json:"actions"`
	Targets            map[string]int64  `json:"targets"`
	Reason             string            `json:"reason"`
}

// ArchiveRevisionResult reports the document revision committed by a correction.
type ArchiveRevisionResult struct {
	DocumentRevision int64 `json:"document_revision"`
}
