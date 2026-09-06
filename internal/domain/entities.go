// Package domain holds the entities, enums, invariants and error codes shared
// by every other package. It performs no I/O and imports nothing internal.
package domain

import "time"

// Source is an authorized read-only photo root.
type Source struct {
	ID                 string
	Name               string
	RootPath           string
	Status             SourceStatus
	RevokedAt          *time.Time
	RevokeReason       string
	IdentityBound      *Identity
	IdentityBoundAt    *time.Time
	Health             Health
	HealthDetail       string
	LastCheckAt        *time.Time
	LastSuccessAt      *time.Time
	ScanGeneration     int64
	LastScanStartedAt  *time.Time
	LastScanCompletedA *time.Time
	BaselineCompleted  *time.Time
	ShareTotalBytes    *int64
	ShareFreeBytes     *int64
	ShareStatsAt       *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Identity is the bound mount fingerprint of a source root.
type Identity struct {
	FSType         string `json:"fstype"`
	MountFromHash  string `json:"mount_from_hash"`
	Marker         bool   `json:"marker"`
	IsNetworkMount bool   `json:"is_network_mount"`
}

// Equal compares the identity fields that must remain stable.
func (i Identity) Equal(o Identity) bool {
	return i.FSType == o.FSType && i.MountFromHash == o.MountFromHash && i.Marker == o.Marker
}

// Photo is one indexed file.
type Photo struct {
	ID                    string
	SourceID              string
	RelPath               string
	Ext                   string
	SizeBytes             int64
	MtimeUnix             int64
	Fingerprint           string
	Status                PhotoStatus
	Width                 int
	Height                int
	Orientation           int
	CapturedAt            *time.Time
	CapturedOffsetSeconds *int
	CapturedConfidence    CapturedConfidence
	CapturedDay           string
	FirstSeenAt           time.Time
	LastSeenAt            time.Time
	LastSeenGeneration    int64
	MissingGenerations    int
	IsBaseline            bool
	MetaStatus            MetaStatus
	MetaError             string
	PreviewStatus         PreviewStatus
	PreviewError          string
	PreviewAttempts       int
	PreviewNextRetryAt    *time.Time
	RemovedAt             *time.Time
	ExcludedAt            *time.Time
	ExcludeReason         string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// PreviewFile is a cached derived image.
type PreviewFile struct {
	PhotoID      string
	Variant      Variant
	RelPath      string
	Bytes        int64
	Width        int
	Height       int
	Fingerprint  string
	CreatedAt    time.Time
	LastAccessAt time.Time
}

// Exclusion is an operator-authored authorization rule.
type Exclusion struct {
	ID        string
	SourceID  string
	MatchKind MatchKind
	Pattern   string
	Reason    string
	CreatedAt time.Time
}

// Job is a persistent unit of background work.
type Job struct {
	ID        string
	Kind      JobKind
	PhotoID   string
	SourceID  string
	Status    JobStatus
	Priority  int
	Attempts  int
	NextRunAt time.Time
	LockedBy  string
	LockedAt  *time.Time
	LastError string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ScanRun records one pass over a source.
type ScanRun struct {
	ID               string
	SourceID         string
	Mode             ScanMode
	Status           ScanStatus
	StartedAt        time.Time
	FinishedAt       *time.Time
	FilesSeen        int64
	FilesNew         int64
	FilesChanged     int64
	FilesMissing     int64
	FilesRemoved     int64
	FilesUnsupported int64
	Errors           int64
	Note             string
}

// Screen is a paired display device.
type Screen struct {
	ID              string
	Name            string
	TokenHash       string
	Status          ScreenStatus
	CreatedAt       time.Time
	ApprovedAt      time.Time
	RevokedAt       *time.Time
	LastSeenAt      *time.Time
	LastIP          string
	ClientVersion   string
	CurrentRoute    *RouteState
	LastSequence    int64
	AppliedSequence int64
}

// RouteState is the client-reported location.
type RouteState struct {
	Name       RouteName `json:"name"`
	Collection string    `json:"collection,omitempty"`
	PhotoID    string    `json:"photo_id,omitempty"`
}

// Valid reports whether the route state is well-formed.
func (r RouteState) Valid() bool { return r.Name.Valid() }

// Pairing is a pending or completed device enrolment.
type Pairing struct {
	ID               string
	Code             string
	Status           PairingStatus
	ClientHint       string
	RemoteIP         string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	ApprovedScreenID string
	ApprovedAt       *time.Time
	ClaimedAt        *time.Time
}

// Command is one issued screen instruction.
type Command struct {
	ID          string
	ScreenID    string
	Sequence    int64
	Kind        CommandKind
	Payload     CommandPayload
	IssuedBy    string
	IssuedAt    time.Time
	ExpiresAt   time.Time
	Status      CommandStatus
	DeliveredAt *time.Time
	ResolvedAt  *time.Time
	ErrorCode   string
	Result      *CommandResult
}

// CommandPayload carries kind-specific parameters.
type CommandPayload struct {
	Route      RouteName `json:"route,omitempty"`
	Collection string    `json:"collection,omitempty"`
	PhotoID    string    `json:"photo_id,omitempty"`
}

// CommandResult is the client-reported or server-observed outcome.
type CommandResult struct {
	Route         *RouteState    `json:"route,omitempty"`
	ResourceID    string         `json:"resource_id,omitempty"`
	ClientVersion string         `json:"client_version,omitempty"`
	Observed      *ObservedState `json:"observed,omitempty"`
}

// ObservedState is what the screen reported after a command became unknown.
type ObservedState struct {
	AppliedSequence int64       `json:"applied_sequence"`
	Route           *RouteState `json:"route,omitempty"`
}

// AdminToken is a stored admin credential.
type AdminToken struct {
	ID         string
	TokenHash  string
	Label      string
	CreatedAt  time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
}

// AuditEntry records an administrative action.
type AuditEntry struct {
	ID     string
	At     time.Time
	Actor  string
	Action string
	Target string
	Detail string
}
