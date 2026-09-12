// Package integration defines the bounded public contract between Core and
// home-mcp. It contains no credentials, database access, or media URLs.
package integration

import "time"

// SchemaVersion identifies the integration wire contract independently of Core releases.
const SchemaVersion = "1"

// Response carries a fresh observation and a bounded, permission-filtered result.
type Response[T any] struct {
	SchemaVersion string    `json:"schema_version"`
	ObservedAt    time.Time `json:"observed_at"`
	Availability  string    `json:"availability"`
	Data          T         `json:"data"`
}

// Photo exposes metadata without file paths, media URLs or image bytes.
type Photo struct {
	ID                 string     `json:"id"`
	SourceID           string     `json:"source_id"`
	CapturedAt         *time.Time `json:"captured_at"`
	CapturedConfidence string     `json:"captured_confidence"`
	FirstSeenAt        time.Time  `json:"first_seen_at"`
	IsBaseline         bool       `json:"is_baseline"`
	PreviewStatus      string     `json:"preview_status"`
}

// PhotoList is one page in an authorized collection.
type PhotoList struct {
	Items           []Photo `json:"items"`
	NextCursor      *string `json:"next_cursor"`
	Collection      string  `json:"collection"`
	Day             string  `json:"day,omitempty"`
	UnknownCaptured int64   `json:"unknown_captured"`
	BaselineOnly    bool    `json:"baseline_only"`
}

// Route redacts a displayed photo ID when the caller cannot read its source.
type Route struct {
	Name           string `json:"name"`
	Collection     string `json:"collection,omitempty"`
	PhotoID        string `json:"photo_id,omitempty"`
	ContentVisible bool   `json:"content_visible"`
}

// Screen describes an application session, not the physical panel power state.
type Screen struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Registered      bool       `json:"registered"`
	Online          bool       `json:"online"`
	LastSeenAt      *time.Time `json:"last_seen_at"`
	CurrentRoute    *Route     `json:"current_route"`
	AppliedSequence int64      `json:"applied_sequence"`
}

// Source exposes NAS health without connection or mount details.
type Source struct {
	ID            string     `json:"id"`
	Health        string     `json:"health"`
	LastCheckAt   *time.Time `json:"last_check_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastScanAt    *time.Time `json:"last_scan_at"`
	ReadyPhotos   int64      `json:"ready_photos"`
}

// PhotoCounts aggregates only the caller's authorized active sources.
type PhotoCounts struct {
	Ready           int64 `json:"ready"`
	PendingPreview  int64 `json:"pending_preview"`
	Unsupported     int64 `json:"unsupported"`
	UnknownCaptured int64 `json:"unknown_captured"`
	Baseline        int64 `json:"baseline"`
	CapturedToday   int64 `json:"captured_today"`
	NewToday        int64 `json:"new_today"`
}

// Home includes only sections individually permitted to the caller.
type Home struct {
	Core         string       `json:"core"`
	Timezone     string       `json:"timezone"`
	Day          string       `json:"day"`
	Capabilities []string     `json:"capabilities"`
	Photos       *PhotoCounts `json:"photos,omitempty"`
	NAS          *[]Source    `json:"nas,omitempty"`
	Screens      *[]Screen    `json:"screens,omitempty"`
}

// CommandPayload carries only whitelisted screen actions.
type CommandPayload struct {
	Route      string `json:"route,omitempty"`
	Collection string `json:"collection,omitempty"`
	PhotoID    string `json:"photo_id,omitempty"`
}

// CommandResult is the acknowledged result, with any unauthorized content redacted.
type CommandResult struct {
	Route      *Route `json:"route,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
}

// Command is the durable Core result; accepted is not an execution confirmation.
type Command struct {
	ID        string         `json:"id"`
	ScreenID  string         `json:"screen_id"`
	Sequence  int64          `json:"sequence"`
	Kind      string         `json:"kind"`
	Payload   CommandPayload `json:"payload"`
	IssuedAt  time.Time      `json:"issued_at"`
	ExpiresAt time.Time      `json:"expires_at"`
	Status    string         `json:"status"`
	ErrorCode string         `json:"error_code,omitempty"`
	Result    *CommandResult `json:"result,omitempty"`
}
