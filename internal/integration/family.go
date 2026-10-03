package integration

import "github.com/DituLin/Atrium/internal/domain"

type FamilyMembers struct {
	Members []domain.FamilyMember `json:"members"`
}
type FamilyRecords = domain.FamilyRecordPage
type FamilyRecordResult struct {
	Record    domain.FamilyRecord `json:"record"`
	Replayed  bool                `json:"replayed"`
	NASStatus string              `json:"nas_status"`
}
