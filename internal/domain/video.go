package domain

import "time"

// VideoStatus is independent of the photo and preview lifecycle.
type VideoStatus string

// Video lifecycle states; ready means metadata is available.
const (
	VideoPending     VideoStatus = "pending"
	VideoReady       VideoStatus = "ready"
	VideoUnsupported VideoStatus = "unsupported"
	VideoRemoved     VideoStatus = "removed"
	VideoExcluded    VideoStatus = "excluded"
)

// VideoObservation describes a stable file in the existing authorized root.
type VideoObservation struct {
	SourceID   string
	RelPath    string
	SizeBytes  int64
	MtimeUnix  int64
	Generation int64
}

// VideoMetadata describes the displayed orientation, not the encoded raster.
// Empty AudioCodec means no audio stream. No paths or arbitrary probe output
// are retained here.
type VideoMetadata struct {
	Container  string `json:"container"`
	VideoCodec string `json:"video_codec"`
	AudioCodec string `json:"audio_codec"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMS int64  `json:"duration_ms"`
	Rotation   int    `json:"rotation"`
}

// Video is an internal index entity, never a screen response DTO.
type Video struct {
	ID                 string
	SourceID           string
	RelPath            string
	Ext                string
	SizeBytes          int64
	MtimeUnix          int64
	Revision           int64
	Status             VideoStatus
	Metadata           VideoMetadata
	LastSeenGeneration int64
	MissingGenerations int64
	FirstSeenAt        time.Time
	LastSeenAt         time.Time
	UpdatedAt          time.Time
}
