package domain

// Household is a stable archive scope independent of display names or members.
type Household struct {
	ID string `json:"id"`
}

// ArchiveGrant adds exactly these permissions and households to an existing
// policy, guarded by a required policy generation. It never grants implicitly.
type ArchiveGrant struct {
	Permissions           []string `json:"permissions"`
	HouseholdIDs          []string `json:"household_ids"`
	ExpectedPolicyVersion int64    `json:"expected_policy_version"`
}

// Validate requires a policy version and nonempty household and permission lists,
// accepting only archive permissions and valid explicit allowlist entries.
func (g ArchiveGrant) Validate() error {
	if g.ExpectedPolicyVersion < 1 || len(g.Permissions) == 0 || len(g.HouseholdIDs) == 0 {
		return Errorf(CodeInvalidRequest, "archive grant requires permissions, households and expected policy version")
	}
	if err := (IntegrationPolicy{Permissions: g.Permissions, HouseholdIDs: g.HouseholdIDs}).Validate(); err != nil {
		return err
	}
	for _, permission := range g.Permissions {
		switch permission {
		case "archive.read", "archive.ingest", "archive.organize", "archive.analyze":
		default:
			return Errorf(CodeInvalidRequest, "grant accepts only archive permissions")
		}
	}
	return nil
}
