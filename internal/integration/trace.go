package integration

import "github.com/oklog/ulid/v2"

// Trace fields are correlation only. Neither metadata nor HTTP headers grant
// permission; Core always derives the principal from the service credential.
const (
	TurnRefMeta   = "io.atrium/turn_ref"
	CallRefMeta   = "io.atrium/call_ref"
	TurnRefHeader = "X-Atrium-Turn-Ref"
	CallRefHeader = "X-Atrium-Call-Ref"
)

// ValidTraceRef accepts only canonical opaque ULIDs, never arbitrary user text.
func ValidTraceRef(value string) bool {
	if len(value) != 26 {
		return false
	}
	id, err := ulid.ParseStrict(value)
	return err == nil && id.String() == value
}
