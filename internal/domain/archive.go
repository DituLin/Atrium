package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"
	"unicode/utf8"
)

// ArchiveTaskStatus is the durable task lifecycle. Only queued/retry_wait and
// expired running tasks may be claimed. waiting_input requires an explicit resolution.
type ArchiveTaskStatus string

const (
	ArchiveTaskQueued       ArchiveTaskStatus = "queued"
	ArchiveTaskRunning      ArchiveTaskStatus = "running"
	ArchiveTaskSucceeded    ArchiveTaskStatus = "succeeded"
	ArchiveTaskWaitingInput ArchiveTaskStatus = "waiting_input"
	ArchiveTaskRetryWait    ArchiveTaskStatus = "retry_wait"
	ArchiveTaskFailed       ArchiveTaskStatus = "failed"
	ArchiveTaskCancelled    ArchiveTaskStatus = "cancelled"
	MaxArchiveJSONBytes                       = 64 * 1024
	MaxArchiveLease                           = 10 * time.Minute
	MaxArchiveRetryDelay                      = 30 * 24 * time.Hour
)

// ArchiveTask holds immutable intake identity and the current worker checkpoint.
// JSON fields are bounded objects, not an executable schema or file-access authority.
// Service adapters must validate task-specific schemas and source permissions.
// LeaseToken is worker authority and must never be sent to ordinary API clients.
type ArchiveTask struct {
	PrincipalID   string `json:"principal_id,omitempty"`
	PolicyVersion int64  `json:"policy_version,omitempty"`
	// DelegationErrorCode is computed in authorized client reads, never persisted.
	DelegationErrorCode string            `json:"delegation_error_code,omitempty"`
	ID                  string            `json:"id"`
	HouseholdID         string            `json:"household_id"`
	Kind                string            `json:"kind"`
	RequestKey          string            `json:"request_key"`
	InputJSON           json.RawMessage   `json:"input_json"`
	Status              ArchiveTaskStatus `json:"status"`
	Stage               string            `json:"stage"`
	Attempts            int               `json:"attempts"`
	AttemptID           string            `json:"attempt_id,omitempty"`
	Version             int64             `json:"version"`
	StateVersion        int64             `json:"state_version"`
	StateJSON           json.RawMessage   `json:"state_json"`
	ResultJSON          json.RawMessage   `json:"result_json"`
	ClarificationJSON   json.RawMessage   `json:"clarification_json"`
	AvailableAt         time.Time         `json:"available_at"`
	LeaseToken          string            `json:"-"`
	LeaseExpiresAt      *time.Time        `json:"lease_expires_at,omitempty"`
	LastErrorCode       string            `json:"last_error_code,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// ArchiveTaskUpdate replaces the stage, state, result and error fields together.
// ExpectedStateVersion must match the worker's source checkpoint. It starts at
// one and advances on payload replacement or clarification, independently of
// event Version. Claims, renewals and cancellation preserve StateVersion.
// Nil state/result values mean empty objects, not partial merges. A worker can
// reuse persisted completed-stage references after reclaim without reparsing.
type ArchiveTaskUpdate struct {
	ExpectedStateVersion int64
	Stage                string
	StateJSON            json.RawMessage
	ResultJSON           json.RawMessage
	LastErrorCode        string
}

// ArchiveTaskEvent is an immutable transition snapshot. AttemptID is an audit
// identity, distinct from the secret lease token. Version orders all events;
// StateVersion fences checkpoint writes and survives lease renewals. Input and
// clarification snapshots preserve the checkpoint provenance.
type ArchiveTaskEvent struct {
	PrincipalID       string            `json:"principal_id,omitempty"`
	PolicyVersion     int64             `json:"policy_version,omitempty"`
	ActorPrincipalID  string            `json:"actor_principal_id,omitempty"`
	HouseholdID       string            `json:"household_id"`
	TaskID            string            `json:"task_id"`
	Version           int64             `json:"version"`
	StateVersion      int64             `json:"state_version"`
	Kind              string            `json:"kind"`
	Status            ArchiveTaskStatus `json:"status"`
	Stage             string            `json:"stage"`
	Attempts          int               `json:"attempts"`
	AttemptID         string            `json:"attempt_id,omitempty"`
	InputJSON         json.RawMessage   `json:"input_json"`
	StateJSON         json.RawMessage   `json:"state_json"`
	ResultJSON        json.RawMessage   `json:"result_json"`
	ClarificationJSON json.RawMessage   `json:"clarification_json"`
	LastErrorCode     string            `json:"last_error_code,omitempty"`
	AvailableAt       time.Time         `json:"available_at"`
	LeaseExpiresAt    *time.Time        `json:"lease_expires_at,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
}

var archiveIdentifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)

func ValidateArchiveIdentifier(value string) error {
	if !archiveIdentifier.MatchString(value) {
		return Errorf(CodeInvalidRequest, "invalid archive identifier")
	}
	return nil
}

// NormalizeArchiveObject defines request equality: keys are sorted recursively,
// whitespace/string escaping are normalized, array order and number spellings
// are retained (1 and 1.0 differ). Duplicate keys, invalid UTF-8 and nesting over
// 32 levels are rejected. It does not authorize paths, URLs or actions in data.
func NormalizeArchiveObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > MaxArchiveJSONBytes || !utf8.Valid(raw) {
		return nil, Errorf(CodeInvalidRequest, "archive JSON must be a bounded UTF-8 object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	value, err := readArchiveJSON(dec, 0)
	if err != nil {
		return nil, Errorf(CodeInvalidRequest, "invalid archive JSON: %v", err)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, Errorf(CodeInvalidRequest, "archive JSON must be an object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, Errorf(CodeInvalidRequest, "archive JSON contains trailing data")
	}
	normalized, err := json.Marshal(value)
	if err != nil || len(normalized) > MaxArchiveJSONBytes {
		return nil, Errorf(CodeInvalidRequest, "archive JSON exceeds size limit")
	}
	return normalized, nil
}

func readArchiveJSON(dec *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, fmt.Errorf("nesting exceeds 32 levels")
	}
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("invalid key")
			}
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("duplicate key")
			}
			object[name], err = readArchiveJSON(dec, depth+1)
			if err != nil {
				return nil, err
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		array := []any{}
		for dec.More() {
			value, err := readArchiveJSON(dec, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter")
	}
}

// ArchiveDocumentDate stores exactly the precision found in the source. A year
// or month is never padded into an invented date; unknown has an empty value.
// Datetime requires RFC3339 with an explicit timezone and at most nine fractional
// digits. More precise source values must not be silently truncated to nanoseconds.
type ArchiveDocumentDate struct {
	Precision string `json:"precision"`
	Value     string `json:"value,omitempty"`
}

// Go's RFC3339 parser also accepts nonstandard offsets, comma fractions and
// arbitrarily long fractions. Bound the input and enforce the wire shape before
// using time.Parse to validate the calendar date.
var archiveDatetimeShape = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func (d ArchiveDocumentDate) Validate() error {
	const maxDateBytes = len("2006-01-02T15:04:05.123456789+07:00")
	if len(d.Value) > maxDateBytes {
		return Errorf(CodeInvalidRequest, "document date exceeds size limit")
	}
	if d.Precision == "unknown" && d.Value == "" {
		return nil
	}
	layout := ""
	switch d.Precision {
	case "year":
		layout = "2006"
	case "month":
		layout = "2006-01"
	case "date":
		layout = "2006-01-02"
	case "datetime":
		if !archiveDatetimeShape.MatchString(d.Value) {
			return Errorf(CodeInvalidRequest, "document datetime must have a bounded RFC3339 shape")
		}
		layout = time.RFC3339Nano
	default:
		return Errorf(CodeInvalidRequest, "invalid document date precision")
	}
	parsed, err := time.Parse(layout, d.Value)
	if err != nil || parsed.Year() < 1 || (d.Precision != "datetime" && parsed.Format(layout) != d.Value) {
		return Errorf(CodeInvalidRequest, "document date does not match its precision")
	}
	return nil
}
