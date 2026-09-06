package diag

import "github.com/DituLin/Atritum/internal/domain"

// Document is the diagnostics payload of design §6.11. Every field is a
// counter, a state name or an identifier: no path, no credential, no photo
// content ever appears here, because this document is what an operator pastes
// into a bug report.
type Document struct {
	Version       string           `json:"version"`
	UptimeSeconds int64            `json:"uptime_seconds"`
	Insecure      bool             `json:"insecure"`
	DB            DBSection        `json:"db"`
	Sources       []SourceSection  `json:"sources"`
	Photos        map[string]int64 `json:"photos"`
	Jobs          JobsSection      `json:"jobs"`
	Cache         CacheSection     `json:"cache"`
	Screens       []ScreenSection  `json:"screens"`
	Commands      CommandsSection  `json:"commands"`
	Widgets       map[string]any   `json:"widgets"`
	Pairings      map[string]int64 `json:"pairings"`
	RecentErrors  []ErrorEntry     `json:"recent_errors"`
}

// DBSection describes the database file. The path is deliberately absent.
type DBSection struct {
	PathRedacted bool  `json:"path_redacted"`
	SizeBytes    int64 `json:"size_bytes"`
	WALBytes     int64 `json:"wal_bytes"`
	Migrations   int   `json:"migrations"`
}

// SourceSection is one authorized root's operational state.
type SourceSection struct {
	ID            string       `json:"id"`
	Health        string       `json:"health"`
	HealthDetail  string       `json:"health_detail,omitempty"`
	IdentityBound bool         `json:"identity_bound"`
	LastSuccessAt *string      `json:"last_success_at,omitempty"`
	LastScan      *ScanSection `json:"last_scan,omitempty"`
	StuckOps      int64        `json:"stuck_ops"`
}

// ScanSection summarises the most recent scan run of a source.
type ScanSection struct {
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	FilesSeen  int64  `json:"files_seen"`
	Errors     int64  `json:"errors"`
}

// JobsSection is the background queue depth.
type JobsSection struct {
	Queued       int64   `json:"queued"`
	Running      int64   `json:"running"`
	Failed       int64   `json:"failed"`
	Done         int64   `json:"done"`
	OldestQueued *string `json:"oldest_queued_at"`
}

// CacheSection is the derived-image budget state.
type CacheSection struct {
	Bytes         int64   `json:"bytes"`
	BudgetBytes   int64   `json:"budget_bytes"`
	FreeDiskBytes int64   `json:"free_disk_bytes"`
	PausedReason  *string `json:"paused_reason"`
}

// ScreenSection is one paired display's presence and command backlog.
type ScreenSection struct {
	ID              string             `json:"id"`
	Online          bool               `json:"online"`
	Registered      bool               `json:"registered"`
	LastSeenAt      *string            `json:"last_seen_at,omitempty"`
	Route           *domain.RouteState `json:"route,omitempty"`
	AppliedSequence int64              `json:"applied_sequence"`
	OpenCommands    int64              `json:"open_commands"`
}

// CommandsSection reports the recent command outcomes.
type CommandsSection struct {
	Last24h map[string]int64 `json:"last_24h"`
}
