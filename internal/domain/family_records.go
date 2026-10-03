package domain

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

var familyID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

type FamilyMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type FamilyMeasurement struct {
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
}
type FamilyRecordInput struct {
	MemberID       string              `json:"member_id"`
	Category       string              `json:"category"`
	OccurredAt     time.Time           `json:"occurred_at"`
	Title          string              `json:"title"`
	Notes          string              `json:"notes"`
	Measurements   []FamilyMeasurement `json:"measurements"`
	DisplayOnTV    bool                `json:"display_on_tv"`
	SupersedesID   string              `json:"supersedes_id,omitempty"`
	RevisionReason string              `json:"revision_reason,omitempty"`
	Retracted      bool                `json:"retracted,omitempty"`
}
type FamilyRecord struct {
	FamilyRecordInput
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	ExportedAt *time.Time `json:"exported_at,omitempty"`
	// SupersededBy is a query projection, never part of the immutable archive.
	SupersededBy string `json:"superseded_by,omitempty"`
}

func (m FamilyMember) Validate() error {
	if !familyID.MatchString(m.ID) || strings.TrimSpace(m.Name) == "" || len(m.Name) > 200 {
		return Errorf(CodeInvalidRequest, "invalid family member")
	}
	return nil
}
func (r FamilyRecordInput) Validate() error {
	if r.SupersedesID == "" {
		if r.RevisionReason != "" || r.Retracted {
			return Errorf(CodeInvalidRequest, "revision requires a target record")
		}
	} else {
		if _, err := ulid.ParseStrict(r.SupersedesID); err != nil || strings.TrimSpace(r.RevisionReason) == "" || len(r.RevisionReason) > 2000 || (r.Retracted && r.DisplayOnTV) {
			return Errorf(CodeInvalidRequest, "invalid family revision")
		}
	}
	if !familyID.MatchString(r.MemberID) || r.OccurredAt.IsZero() || r.OccurredAt.Year() < 1900 || r.OccurredAt.Year() > 2200 || strings.TrimSpace(r.Title) == "" || len(r.Title) > 300 || len(r.Notes) > 16000 || len(r.Measurements) > 32 {
		return Errorf(CodeInvalidRequest, "invalid family record")
	}
	switch r.Category {
	case "growth", "health", "checkup", "meal", "note":
	default:
		return Errorf(CodeInvalidRequest, "invalid record category")
	}
	seen := map[string]bool{}
	for _, m := range r.Measurements {
		if !familyID.MatchString(m.Metric) || seen[m.Metric] || strings.TrimSpace(m.Unit) == "" || len(m.Unit) > 32 || math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
			return Errorf(CodeInvalidRequest, "invalid or duplicate measurement")
		}
		seen[m.Metric] = true
	}
	return nil
}

// FamilyRecordPage is ordered by observation time, with a stable continuation.
type FamilyRecordPage struct {
	Records    []FamilyRecord `json:"records"`
	NextCursor string         `json:"next_cursor,omitempty"`
}
