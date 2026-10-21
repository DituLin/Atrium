package archive

// Overview is one authorized household-wide document snapshot. Counts are not
// affected by the current screen's entity, domain, or result-page filters.
type Overview struct {
	Generation     string                `json:"generation"`
	Total          int                   `json:"total"`
	Confirmed      int                   `json:"confirmed"`
	Pending        int                   `json:"pending"`
	Domains        []OverviewDomain      `json:"domains"`
	Entities       []OverviewEntity      `json:"entities"`
	Ownership      OverviewOwnership     `json:"ownership"`
	Coverage       SearchCoverage        `json:"coverage"`
	GrowthProfiles []PublicGrowthProfile `json:"growth_profiles,omitempty"`
}

// PublicGrowthProfile is explicit operator-confirmed reference information.
// It never derives a birthday or sex from archive search dates or names. The
// HTTP projection includes it only for an entity in a complete public snapshot.
type PublicGrowthProfile struct {
	EntityID          string `json:"entity_id"`
	BirthDate         string `json:"birth_date"`
	Sex               string `json:"sex"`
	HeightMeasurement string `json:"height_measurement,omitempty"`
}

// OverviewDomain counts each document once in its current public domain.
type OverviewDomain struct {
	Domain string `json:"domain"`
	Count  int    `json:"count"`
}

// OverviewEntity names only an already publicly visible entity, never a name.
// A document contributes at most once to each entity's count.
type OverviewEntity struct {
	ID    string `json:"id"`
	Count int    `json:"count"`
}

// OverviewOwnership contains mutually exclusive non-person attribution counts.
type OverviewOwnership struct {
	Household  int `json:"household"`
	Unassigned int `json:"unassigned"`
	Waiting    int `json:"waiting"`
}
