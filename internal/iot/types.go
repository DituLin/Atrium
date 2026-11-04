// Package iot defines provider-independent, read-only device observations.
package iot

import (
	"context"
	"time"
)

type Kind string

const (
	Power          Kind = "power"
	Humidity       Kind = "humidity"
	Temperature    Kind = "temperature"
	OperatingState Kind = "operating_state"
	Battery        Kind = "battery"
	FilterLife     Kind = "filter_life"
)

func (k Kind) Valid() bool {
	switch k {
	case Power, Humidity, Temperature, OperatingState, Battery, FilterLife:
		return true
	}
	return false
}

// Operating states are deliberately bounded; provider-specific states require mappings.
func ValidOperatingState(s string) bool {
	switch s {
	case "off", "idle", "active", "paused", "error":
		return true
	}
	return false
}

type Availability string

const (
	Available   Availability = "available"
	Unknown     Availability = "unknown"
	Unavailable Availability = "unavailable"
)

type Reason string

const (
	ReasonUnknown      Reason = "source_unknown"
	ReasonUnavailable  Reason = "source_unavailable"
	ReasonMissing      Reason = "missing_value"
	ReasonInvalid      Reason = "invalid_value"
	ReasonDisconnected Reason = "disconnected"
)

type ConnectionStatus string

const (
	Disconnected ConnectionStatus = "disconnected"
	Connecting   ConnectionStatus = "connecting"
	Connected    ConnectionStatus = "connected"
)

// Public types intentionally have no provider identifiers, credentials, URLs or raw errors.
type Snapshot struct {
	Instances []Instance `json:"instances"`
}
type Instance struct {
	Provider string `json:"provider"`
	// LastSuccessAt records the most recent completed provider synchronization.
	LastSuccessAt *time.Time       `json:"last_success_at,omitempty"`
	ID            string           `json:"id"`
	Generation    uint64           `json:"generation"`
	Status        ConnectionStatus `json:"status"`
	Devices       []Device         `json:"devices"`
}
type Device struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	RoomID       string       `json:"room_id"`
	RoomName     string       `json:"room_name"`
	Capabilities []Capability `json:"capabilities"`
}
type Capability struct {
	Name        string      `json:"name"`
	ID          string      `json:"id"`
	Kind        Kind        `json:"kind"`
	Observation Observation `json:"observation"`
}
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
type SnapshotReader interface {
	Read(context.Context) (Snapshot, error)
}
type Sink interface {
	Publish(context.Context, Update) error
}

// Run blocks until cancellation or unrecoverable failure. Implementations reconnect
// internally, publish connection transitions, and never send device commands.
type Adapter interface {
	Run(context.Context, Sink) error
}
