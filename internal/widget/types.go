// Package widget composes the dashboard snapshot served by GET /api/v1/home.
package widget

import "time"

// Snapshot is the payload of GET /api/v1/home (design §6.7). Optional widgets
// are pointers so a disabled widget is absent rather than null-valued.
// The JSON form is the widget envelope produced in wire.go.
type Snapshot struct {
	SchemaVersion int
	HomeName      string
	ServerTime    string
	// Version is the change-bus version at composition time; `data.changed`
	// carries the same counter so a client can tell whether it is current.
	Version int64
	Clock   Clock
	Photo   Photo
	NAS     NAS
	Weather *Weather
	Notice  *Notice
}

// Clock is the time widget; it never goes stale.
type Clock struct {
	Timezone           string  `json:"timezone"`
	ServerTime         string  `json:"server_time"`
	UTCOffsetSeconds   int     `json:"utc_offset_seconds"`
	NextOffsetChangeAt *string `json:"next_offset_change_at,omitempty"`
}

// Photo summarises the library for the slideshow.
type Photo struct {
	SlideshowIntervalSeconds int         `json:"slideshow_interval_seconds"`
	Totals                   PhotoTotals `json:"totals"`
	NewToday                 int64       `json:"new_today"`
	CapturedToday            int64       `json:"captured_today"`
	UnknownCaptured          int64       `json:"unknown_captured"`
	Baseline                 Baseline    `json:"baseline"`
	Index                    Index       `json:"index"`
}

// PhotoTotals counts the library by state.
type PhotoTotals struct {
	Ready          int64 `json:"ready"`
	PendingPreview int64 `json:"pending_preview"`
	Unsupported    int64 `json:"unsupported"`
}

// Baseline reports the first-import state.
type Baseline struct {
	// Status is importing, done or none.
	Status      string  `json:"status"`
	CompletedAt *string `json:"completed_at,omitempty"`
}

// Index reports scan activity.
type Index struct {
	State      string        `json:"state"`
	Progress   IndexProgress `json:"progress"`
	LastScanAt *string       `json:"last_scan_at,omitempty"`
}

// IndexProgress carries the counters of the current or last scan.
type IndexProgress struct {
	Seen           int64 `json:"seen"`
	Indexed        int64 `json:"indexed"`
	PendingPreview int64 `json:"pending_preview"`
}

// NAS lists source health as screens may see it.
type NAS struct {
	Sources []NASSource `json:"sources"`
}

// NASSource is one authorized share. Paths are never included.
type NASSource struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Health          string  `json:"health"`
	LastCheckAt     *string `json:"last_check_at,omitempty"`
	LastSuccessAt   *string `json:"last_success_at,omitempty"`
	ShareFreeBytes  *int64  `json:"share_free_bytes,omitempty"`
	ShareTotalBytes *int64  `json:"share_total_bytes,omitempty"`
	// HealthDetail is only populated for the admin scope.
	HealthDetail string `json:"health_detail,omitempty"`
	// StuckOps counts abandoned syscalls; admin scope only.
	StuckOps int64 `json:"stuck_ops,omitempty"`
	// IdentityConfirmed is false while the mount behind the root does not
	// match the bound identity; admin scope only.
	IdentityConfirmed *bool `json:"identity_confirmed,omitempty"`
}

// Weather is the optional P1 widget.
type Weather struct {
	Provider      string  `json:"provider"`
	LocationLabel string  `json:"location_label"`
	TemperatureC  float64 `json:"temperature_c"`
	ConditionCode int     `json:"condition_code"`
	ConditionText string  `json:"condition_text"`
	FetchedAt     string  `json:"fetched_at"`
	Stale         bool    `json:"stale"`
}

// Notice is a static operator message.
type Notice struct {
	Text      string `json:"text"`
	UpdatedAt string `json:"updated_at"`
}

// formatTime renders an API timestamp with its offset.
func formatTime(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(time.RFC3339)
}

// formatTimePtr renders an optional API timestamp.
func formatTimePtr(t *time.Time, loc *time.Location) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := formatTime(*t, loc)
	return &s
}
