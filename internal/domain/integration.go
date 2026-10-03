package domain

import (
	"slices"
	"strings"
	"time"
)

// IntegrationPolicy is an explicit allowlist. Empty lists grant no access.
type IntegrationPolicy struct {
	Permissions  []string `json:"permissions"`
	ScreenIDs    []string `json:"screen_ids"`
	SourceIDs    []string `json:"source_ids"`
	MemberIDs    []string `json:"member_ids,omitempty"`
	HouseholdIDs []string `json:"household_ids,omitempty"`
}

// Allows reports explicit permission membership.
func (p IntegrationPolicy) Allows(permission string) bool {
	return slices.Contains(p.Permissions, permission)
}

// AllowsScreen reports explicit screen membership.
func (p IntegrationPolicy) AllowsScreen(id string) bool { return slices.Contains(p.ScreenIDs, id) }

// AllowsSource reports explicit source membership.
func (p IntegrationPolicy) AllowsSource(id string) bool { return slices.Contains(p.SourceIDs, id) }

func (p IntegrationPolicy) AllowsMember(id string) bool { return slices.Contains(p.MemberIDs, id) }

func (p IntegrationPolicy) AllowsHousehold(id string) bool {
	return slices.Contains(p.HouseholdIDs, id)
}

// Validate rejects ambiguous, duplicate, wildcard and oversized entries.
func (p IntegrationPolicy) Validate() error {
	valid := []string{"home.read", "nas.read", "photos.read", "screens.read", "screens.control", "commands.read", "family.read", "family.write", "archive.read", "archive.ingest", "archive.organize", "archive.analyze"}
	for _, list := range [][]string{p.Permissions, p.ScreenIDs, p.SourceIDs, p.MemberIDs, p.HouseholdIDs} {
		if len(list) > 128 {
			return Errorf(CodeInvalidRequest, "policy allowlist exceeds 128 entries")
		}
		seen := map[string]bool{}
		for _, v := range list {
			if v == "" || len(v) > 128 || strings.TrimSpace(v) != v || strings.ContainsAny(v, "*?\n\r\t") || seen[v] {
				return Errorf(CodeInvalidRequest, "invalid or duplicate policy entry")
			}
			seen[v] = true
		}
	}
	for _, v := range p.Permissions {
		if !slices.Contains(valid, v) {
			return Errorf(CodeInvalidRequest, "unknown integration permission")
		}
	}
	return nil
}

// IntegrationPrincipal is stable across credential rotations.
type IntegrationPrincipal struct {
	ID            string            `json:"id"`
	Label         string            `json:"label"`
	Enabled       bool              `json:"enabled"`
	RevokedAt     *time.Time        `json:"revoked_at,omitempty"`
	ExpiresAt     time.Time         `json:"expires_at"`
	Policy        IntegrationPolicy `json:"policy"`
	PolicyVersion int64             `json:"policy_version"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// Active checks expiry and revocation at the supplied time.
func (p IntegrationPrincipal) Active(now time.Time) bool {
	return p.Enabled && p.RevokedAt == nil && now.Before(p.ExpiresAt)
}

// IntegrationCredential stores only the irreversible credential hash.
type IntegrationCredential struct {
	ID          string     `json:"id"`
	PrincipalID string     `json:"principal_id"`
	TokenHash   string     `json:"-"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

// Active checks expiry and revocation at the supplied time.
func (c IntegrationCredential) Active(now time.Time) bool {
	return c.RevokedAt == nil && now.Before(c.ExpiresAt)
}

// IntegrationActor is a server-authenticated reference, never a request payload.
// Repositories reload both rows; no caller-supplied policy is trusted.
type IntegrationActor struct {
	PrincipalID  string
	CredentialID string
}

// AllowsArchive keeps archive permissions independent of legacy member grants.
// Ingest delegates automatic parsing, classification and entity/fact extraction.
// Organize permits manual correction; analysis also needs archive.read.
func (p IntegrationPolicy) AllowsArchive(permission, household string) bool {
	if !p.AllowsHousehold(household) {
		return false
	}
	switch permission {
	case "archive.read", "archive.ingest", "archive.organize":
		return p.Allows(permission)
	case "archive.analyze":
		return p.Allows(permission) && p.Allows("archive.read")
	default:
		return false
	}
}
