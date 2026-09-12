// Package family exposes deterministic, public family projections.
package family

import "github.com/DituLin/Atrium/internal/domain"

// Availability describes the source snapshot, independently of device health.
type Availability string

// Source availability values distinguish missing, pending, current, expired and failed reads.
const (
	NotConnected Availability = "not_connected"
	Loading      Availability = "loading"
	Available    Availability = "available"
	Stale        Availability = "stale"
	Failed       Availability = "failed"
)

// Reason is a public explanation, never a raw source error.
type Reason string

// Public reasons explain source availability without disclosing internal errors.
const (
	NotConfigured  Reason = "not_configured"
	NotProvided    Reason = "not_provided"
	NotSupported   Reason = "not_supported"
	SharingStopped Reason = "sharing_stopped"
	ReadFailed     Reason = "read_failed"
	Expired        Reason = "expired"
	NotObserved    Reason = "not_observed"
)

// ItemMeta carries content times. Observation and expiry belong to the source.
type ItemMeta struct {
	ID         string  `json:"id"`
	UpdatedAt  *string `json:"updated_at"`
	ValidFrom  *string `json:"valid_from"`
	ValidUntil *string `json:"valid_until"`
}

// SourceSnapshot is one complete read; empty items serialize as an array.
type SourceSnapshot[T any] struct {
	SourceID     string       `json:"source_id"`
	SourceLabel  string       `json:"source_label"`
	ObservedAt   *string      `json:"observed_at"`
	ExpiresAt    *string      `json:"expires_at"`
	Availability Availability `json:"availability"`
	Reason       *Reason      `json:"reason"`
	Items        []T          `json:"items"`
}

// CoreResponse records only that Core handled this HTTP response.
type CoreResponse struct {
	ItemMeta
	Responding bool `json:"responding"`
}

// NASHealth exposes the health observation and last readable-check time.
type NASHealth struct {
	ItemMeta
	Health        domain.Health `json:"health"`
	LastSuccessAt *string       `json:"last_success_at"`
}

// HouseResponse deliberately excludes source paths, capacity and admin details.
type HouseResponse struct {
	SchemaVersion int                          `json:"schema_version"`
	GeneratedAt   string                       `json:"generated_at"`
	Home          HouseHome                    `json:"home"`
	Core          SourceSnapshot[CoreResponse] `json:"core"`
	NAS           []SourceSnapshot[NASHealth]  `json:"nas"`
	Profile       SourceSnapshot[struct{}]     `json:"profile"`
	Environment   SourceSnapshot[struct{}]     `json:"environment"`
}

// HouseHome identifies the configured household and its IANA timezone.
type HouseHome struct {
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}
