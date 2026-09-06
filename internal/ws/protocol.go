// Package ws implements the screen realtime channel of technical design §9:
// the authenticated WebSocket upgrade, one session per screen, the bounded
// send queue and the message envelope both directions share.
package ws

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// SchemaVersion is the envelope version every message carries.
const SchemaVersion = 1

// MaxMessageBytes is the largest frame the server accepts (design §9). A
// larger one is a protocol error, not a resource to buffer.
const MaxMessageBytes = 64 << 10

// Message types, server to client.
const (
	TypeSessionReady      = "session.ready"
	TypeScreenCommand     = "screen.command"
	TypeDataChanged       = "data.changed"
	TypeHeartbeatAck      = "heartbeat.ack"
	TypeSessionRevoked    = "session.revoked"
	TypeSessionSuperseded = "session.superseded"
)

// Message types, client to server.
const (
	TypeHeartbeat  = "heartbeat"
	TypeState      = "state"
	TypeCommandAck = "command.ack"
)

// Envelope wraps every message in both directions.
type Envelope struct {
	SchemaVersion int             `json:"schema_version"`
	Type          string          `json:"type"`
	ID            string          `json:"id"`
	SentAt        string          `json:"sent_at"`
	Payload       json.RawMessage `json:"payload"`
}

// ScreenRef identifies the screen a session belongs to.
type ScreenRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SessionReady is the first message after a successful handshake.
type SessionReady struct {
	Screen         ScreenRef       `json:"screen"`
	ServerTime     string          `json:"server_time"`
	HomeVersion    int64           `json:"home_version"`
	PendingCommand *CommandMessage `json:"pending_command,omitempty"`
}

// CommandMessage is one screen instruction.
type CommandMessage struct {
	CommandID string                `json:"command_id"`
	Sequence  int64                 `json:"sequence"`
	Kind      domain.CommandKind    `json:"kind"`
	Payload   domain.CommandPayload `json:"payload"`
	IssuedAt  string                `json:"issued_at"`
	ExpiresAt string                `json:"expires_at"`
}

// NewCommandMessage renders a stored command for the wire.
func NewCommandMessage(cmd *domain.Command) *CommandMessage {
	return &CommandMessage{
		CommandID: cmd.ID,
		Sequence:  cmd.Sequence,
		Kind:      cmd.Kind,
		Payload:   cmd.Payload,
		IssuedAt:  cmd.IssuedAt.UTC().Format(time.RFC3339),
		ExpiresAt: cmd.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

// DataChanged notifies a session that server-side data moved.
type DataChanged struct {
	Topics  []string `json:"topics"`
	Version int64    `json:"version"`
}

// HeartbeatAck answers a client heartbeat.
type HeartbeatAck struct {
	ServerTime string `json:"server_time"`
}

// SessionRevoked precedes close 4002.
type SessionRevoked struct {
	Reason string `json:"reason"`
}

// Heartbeat is the client's 15 s presence report.
type Heartbeat struct {
	Route           *domain.RouteState `json:"route"`
	AppliedSequence int64              `json:"applied_sequence"`
	ClientVersion   string             `json:"client_version"`
}

// StateReport is sent when the client changes route on its own.
type StateReport struct {
	Route *domain.RouteState `json:"route"`
}

// CommandAck is the client's decision about one command.
type CommandAck struct {
	CommandID  string               `json:"command_id"`
	Status     domain.CommandStatus `json:"status"`
	Route      *domain.RouteState   `json:"route"`
	ResourceID string               `json:"resource_id,omitempty"`
	ErrorCode  string               `json:"error_code,omitempty"`
}

// encode renders a payload inside an envelope.
func encode(msgType string, now time.Time, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ws: encode %s payload: %w", msgType, err)
	}
	return json.Marshal(Envelope{
		SchemaVersion: SchemaVersion,
		Type:          msgType,
		ID:            "msg_" + domain.NewID(),
		SentAt:        now.UTC().Format(time.RFC3339),
		Payload:       body,
	})
}
