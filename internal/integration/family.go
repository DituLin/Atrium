package integration

import "github.com/DituLin/Atrium/internal/domain"

// FamilyMembers lists the record owners visible to the integration principal.
type FamilyMembers struct {
	Members []domain.FamilyMember `json:"members"`
}

// FamilyRecords carries an authorized observation page and its continuation cursor.
type FamilyRecords = domain.FamilyRecordPage

// FamilyRecordResult acknowledges a stored observation, including idempotent
// replay and the current NAS export status.
type FamilyRecordResult struct {
	Record    domain.FamilyRecord `json:"record"`
	Replayed  bool                `json:"replayed"`
	NASStatus string              `json:"nas_status"`
}
