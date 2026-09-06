package domain

// SourceStatus is the authorization state of a data source.
type SourceStatus string

// Source statuses.
const (
	SourceActive  SourceStatus = "active"
	SourceRevoked SourceStatus = "revoked"
)

// Valid reports whether the value is a known source status.
func (s SourceStatus) Valid() bool { return s == SourceActive || s == SourceRevoked }

// Health is the observed reachability of a data source.
type Health string

// Health states (technical design §6.1).
const (
	HealthOnline   Health = "online"
	HealthOffline  Health = "offline"
	HealthDegraded Health = "degraded"
	HealthUnknown  Health = "unknown"
)

// Valid reports whether the value is a known health state.
func (h Health) Valid() bool {
	switch h {
	case HealthOnline, HealthOffline, HealthDegraded, HealthUnknown:
		return true
	}
	return false
}

// PhotoStatus is the lifecycle state of an indexed photo.
type PhotoStatus string

// Photo statuses.
const (
	PhotoPending     PhotoStatus = "pending"
	PhotoReady       PhotoStatus = "ready"
	PhotoRemoved     PhotoStatus = "removed"
	PhotoExcluded    PhotoStatus = "excluded"
	PhotoUnsupported PhotoStatus = "unsupported"
)

// Valid reports whether the value is a known photo status.
func (p PhotoStatus) Valid() bool {
	switch p {
	case PhotoPending, PhotoReady, PhotoRemoved, PhotoExcluded, PhotoUnsupported:
		return true
	}
	return false
}

// MetaStatus is the metadata extraction state.
type MetaStatus string

// Metadata statuses.
const (
	MetaPending MetaStatus = "pending"
	MetaReady   MetaStatus = "ready"
	MetaFailed  MetaStatus = "failed"
)

// Valid reports whether the value is a known metadata status.
func (m MetaStatus) Valid() bool {
	switch m {
	case MetaPending, MetaReady, MetaFailed:
		return true
	}
	return false
}

// PreviewStatus is the derived-image state of a photo.
type PreviewStatus string

// Preview statuses.
const (
	PreviewPending     PreviewStatus = "pending"
	PreviewProcessing  PreviewStatus = "processing"
	PreviewReady       PreviewStatus = "ready"
	PreviewFailed      PreviewStatus = "failed"
	PreviewEvicted     PreviewStatus = "evicted"
	PreviewUnavailable PreviewStatus = "unavailable"
)

// Valid reports whether the value is a known preview status.
func (p PreviewStatus) Valid() bool {
	switch p {
	case PreviewPending, PreviewProcessing, PreviewReady, PreviewFailed, PreviewEvicted, PreviewUnavailable:
		return true
	}
	return false
}

// CapturedConfidence describes how captured_at was derived.
type CapturedConfidence string

// Captured-at confidence levels.
const (
	CapturedExact    CapturedConfidence = "exact"
	CapturedInferred CapturedConfidence = "inferred"
	CapturedUnknown  CapturedConfidence = "unknown"
)

// Valid reports whether the value is a known confidence level.
func (c CapturedConfidence) Valid() bool {
	switch c {
	case CapturedExact, CapturedInferred, CapturedUnknown:
		return true
	}
	return false
}

// Variant is a derived image size.
type Variant string

// Image variants.
const (
	VariantPreview Variant = "preview"
	VariantThumb   Variant = "thumb"
)

// Valid reports whether the value is a known variant.
func (v Variant) Valid() bool { return v == VariantPreview || v == VariantThumb }

// ScreenStatus is the registration state of a screen.
type ScreenStatus string

// Screen statuses.
const (
	ScreenActive  ScreenStatus = "active"
	ScreenRevoked ScreenStatus = "revoked"
)

// Valid reports whether the value is a known screen status.
func (s ScreenStatus) Valid() bool { return s == ScreenActive || s == ScreenRevoked }

// PairingStatus is the state of a pairing attempt.
type PairingStatus string

// Pairing statuses.
const (
	PairingPending  PairingStatus = "pending"
	PairingApproved PairingStatus = "approved"
	PairingClaimed  PairingStatus = "claimed"
	PairingExpired  PairingStatus = "expired"
	PairingRejected PairingStatus = "rejected"
)

// Valid reports whether the value is a known pairing status.
func (p PairingStatus) Valid() bool {
	switch p {
	case PairingPending, PairingApproved, PairingClaimed, PairingExpired, PairingRejected:
		return true
	}
	return false
}

// CommandKind is a screen control verb.
type CommandKind string

// Command kinds (FR-14).
const (
	CommandNavigate CommandKind = "navigate"
	CommandShow     CommandKind = "show"
	CommandRefresh  CommandKind = "refresh"
)

// Valid reports whether the value is a known command kind.
func (c CommandKind) Valid() bool {
	switch c {
	case CommandNavigate, CommandShow, CommandRefresh:
		return true
	}
	return false
}

// CommandStatus is the lifecycle state of an issued command.
type CommandStatus string

// Command statuses (FR-15).
const (
	CommandAccepted CommandStatus = "accepted"
	CommandApplied  CommandStatus = "applied"
	CommandFailed   CommandStatus = "failed"
	CommandExpired  CommandStatus = "expired"
	CommandUnknown  CommandStatus = "unknown"
)

// Valid reports whether the value is a known command status.
func (c CommandStatus) Valid() bool {
	switch c {
	case CommandAccepted, CommandApplied, CommandFailed, CommandExpired, CommandUnknown:
		return true
	}
	return false
}

// Terminal reports whether no further transition is possible.
func (c CommandStatus) Terminal() bool { return c != CommandAccepted }

// RouteName is a whitelisted client route.
type RouteName string

// Route names (technical design §7.1).
const (
	RouteDashboard RouteName = "dashboard"
	RoutePhotos    RouteName = "photos"
	RoutePhoto     RouteName = "photo"
	RoutePair      RouteName = "pair"
	RouteConnect   RouteName = "connect"
)

// Valid reports whether the value is a known route.
func (r RouteName) Valid() bool {
	switch r {
	case RouteDashboard, RoutePhotos, RoutePhoto, RoutePair, RouteConnect:
		return true
	}
	return false
}

// NavigableRoute reports whether a navigate command may target the route.
func (r RouteName) NavigableRoute() bool { return r == RouteDashboard || r == RoutePhotos }

// Collection is a photo query set.
type Collection string

// Collections (technical design §6.4).
const (
	CollectionRecent        Collection = "recent"
	CollectionCapturedToday Collection = "captured_today"
	CollectionRandom        Collection = "random"
	CollectionAll           Collection = "all"
)

// Valid reports whether the value is a known collection.
func (c Collection) Valid() bool {
	switch c {
	case CollectionRecent, CollectionCapturedToday, CollectionRandom, CollectionAll:
		return true
	}
	return false
}

// WidgetType is a closed whitelist of dashboard widgets.
type WidgetType string

// Widget types (technical design §6.7).
const (
	WidgetClock   WidgetType = "clock"
	WidgetPhoto   WidgetType = "photo"
	WidgetNAS     WidgetType = "nas"
	WidgetWeather WidgetType = "weather"
	WidgetNotice  WidgetType = "notice"
)

// Valid reports whether the value is a known widget type.
func (w WidgetType) Valid() bool {
	switch w {
	case WidgetClock, WidgetPhoto, WidgetNAS, WidgetWeather, WidgetNotice:
		return true
	}
	return false
}

// JobKind identifies a background job type.
type JobKind string

// Job kinds.
const (
	JobExtractMeta  JobKind = "extract_meta"
	JobBuildPreview JobKind = "build_preview"
	JobRecomputeDay JobKind = "recompute_day"
	JobSourceProbe  JobKind = "source_probe"
)

// Valid reports whether the value is a known job kind.
func (j JobKind) Valid() bool {
	switch j {
	case JobExtractMeta, JobBuildPreview, JobRecomputeDay, JobSourceProbe:
		return true
	}
	return false
}

// JobStatus is the state of a queued job.
type JobStatus string

// Job statuses.
const (
	JobQueued  JobStatus = "queued"
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// Valid reports whether the value is a known job status.
func (j JobStatus) Valid() bool {
	switch j {
	case JobQueued, JobRunning, JobDone, JobFailed:
		return true
	}
	return false
}

// ScanMode is how a scan run was triggered.
type ScanMode string

// Scan modes.
const (
	ScanScheduled ScanMode = "scheduled"
	ScanManual    ScanMode = "manual"
	ScanFull      ScanMode = "full"
)

// Valid reports whether the value is a known scan mode.
func (s ScanMode) Valid() bool {
	switch s {
	case ScanScheduled, ScanManual, ScanFull:
		return true
	}
	return false
}

// ScanStatus is the state of a scan run.
type ScanStatus string

// Scan run statuses.
const (
	ScanRunning   ScanStatus = "running"
	ScanCompleted ScanStatus = "completed"
	ScanFailed    ScanStatus = "failed"
	ScanAborted   ScanStatus = "aborted"
)

// Valid reports whether the value is a known scan status.
func (s ScanStatus) Valid() bool {
	switch s {
	case ScanRunning, ScanCompleted, ScanFailed, ScanAborted:
		return true
	}
	return false
}

// IndexState summarises indexing activity for the home snapshot.
type IndexState string

// Index states.
const (
	IndexIdle           IndexState = "idle"
	IndexScanning       IndexState = "scanning"
	IndexBaselineImport IndexState = "baseline_import"
)

// MatchKind is how an exclusion pattern is matched.
type MatchKind string

// Exclusion match kinds.
const (
	MatchPath   MatchKind = "path"
	MatchPrefix MatchKind = "prefix"
)

// Valid reports whether the value is a known match kind.
func (m MatchKind) Valid() bool { return m == MatchPath || m == MatchPrefix }

// Topic is an in-process change notification subject.
type Topic string

// Change topics (technical design §9).
const (
	TopicHome   Topic = "home"
	TopicPhotos Topic = "photos"
	TopicNAS    Topic = "nas"
	TopicScreen Topic = "screen"
)

// Valid reports whether the value is a known topic.
func (t Topic) Valid() bool {
	switch t {
	case TopicHome, TopicPhotos, TopicNAS, TopicScreen:
		return true
	}
	return false
}
