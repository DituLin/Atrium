// Package iot defines provider-independent, read-only device observations.
package iot

import (
	"context"
	"time"
)

// Kind identifies a supported normalized device capability.
type Kind string

// Supported capability kinds.
const (
	Power          Kind = "power"
	Humidity       Kind = "humidity"
	Temperature    Kind = "temperature"
	OperatingState Kind = "operating_state"
	Battery        Kind = "battery"
	FilterLife     Kind = "filter_life"
)

// Valid reports whether the capability kind is supported.
func (k Kind) Valid() bool {
	switch k {
	case Power, Humidity, Temperature, OperatingState, Battery, FilterLife:
		return true
	}
	return false
}

// ValidOperatingState accepts the bounded public states; providers require mappings.
func ValidOperatingState(s string) bool {
	switch s {
	case "off", "idle", "active", "paused", "error":
		return true
	}
	return false
}

// Availability distinguishes valid values from unknown and unavailable sources.
type Availability string

// Supported entity availability states.
const (
	Available   Availability = "available"
	Unknown     Availability = "unknown"
	Unavailable Availability = "unavailable"
)

// Reason is a bounded public explanation that excludes provider errors.
type Reason string

// Public observation failure reasons.
const (
	ReasonUnknown      Reason = "source_unknown"
	ReasonUnavailable  Reason = "source_unavailable"
	ReasonMissing      Reason = "missing_value"
	ReasonInvalid      Reason = "invalid_value"
	ReasonDisconnected Reason = "disconnected"
)

// ConnectionStatus describes the adapter connection independently of entity health.
type ConnectionStatus string

// Supported adapter connection states.
const (
	Disconnected ConnectionStatus = "disconnected"
	Connecting   ConnectionStatus = "connecting"
	Connected    ConnectionStatus = "connected"
)

// Snapshot contains public observations without provider identifiers, credentials or URLs.
type Snapshot struct {
	Instances []Instance `json:"instances"`
}

// Instance contains a source connection and its configured public devices.
type Instance struct {
	Provider string `json:"provider"`
	// LastSuccessAt records the most recent completed provider synchronization.
	LastSuccessAt *time.Time       `json:"last_success_at,omitempty"`
	ID            string           `json:"id"`
	Generation    uint64           `json:"generation"`
	Status        ConnectionStatus `json:"status"`
	Devices       []Device         `json:"devices"`
}

// Device is a stable public identity with room and capability metadata.
type Device struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	RoomID       string       `json:"room_id"`
	RoomName     string       `json:"room_name"`
	Capabilities []Capability `json:"capabilities"`
}

// Capability combines a supported kind with its latest observation.
type Capability struct {
	Name        string      `json:"name"`
	ID          string      `json:"id"`
	Kind        Kind        `json:"kind"`
	Observation Observation `json:"observation"`
}

// Observation holds a normalized value and independently tracked observation times.
type Observation struct {
	InstanceID   string       `json:"instance_id"`
	DeviceID     string       `json:"device_id"`
	CapabilityID string       `json:"capability_id"`
	Generation   uint64       `json:"generation"`
	Value        any          `json:"value"` // bool, float64, bounded operating-state string, or nil
	Unit         string       `json:"unit,omitempty"`
	Availability Availability `json:"availability"`
	Reason       Reason       `json:"reason,omitempty"`
	// SourceUpdatedAt is provider last_updated; never inferred from receipt time.
	SourceUpdatedAt *time.Time `json:"source_updated_at,omitempty"`
	// SampledAt is set only when a provider explicitly supplies a measurement time.
	SampledAt  *time.Time `json:"sampled_at,omitempty"`
	ReceivedAt time.Time  `json:"received_at"`
	// ConfirmedAt records successful synchronization, independent of value changes.
	ConfirmedAt time.Time `json:"confirmed_at"`
}

// Update belongs to a connection generation. Replace marks a full synchronization;
// otherwise observations are deltas. Sinks must reject updates from older generations.
type Update struct {
	InstanceID   string
	Generation   uint64
	Status       ConnectionStatus
	Replace      bool
	Observations []Observation
}

// SnapshotReader reads the current authorized public observations.
type SnapshotReader interface {
	Read(context.Context) (Snapshot, error)
}

// Sink atomically accepts updates from an adapter connection.
type Sink interface {
	Publish(context.Context, Update) error
}

// Adapter runs until cancellation or unrecoverable failure. Implementations reconnect
// internally, publish connection transitions, and never send device commands.
type Adapter interface {
	Run(context.Context, Sink) error
}
