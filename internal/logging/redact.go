// Package logging builds the slog handlers used by the server and the CLI.
package logging

import (
	"log/slog"
	"strings"
)

// Placeholder replaces the value of a sensitive attribute.
const Placeholder = "<redacted>"

// sensitiveKeys are dropped or masked at info level and above. Tokens and NAS
// paths must never reach a log file (design §6.11, §15).
var sensitiveKeys = map[string]struct{}{
	"token":         {},
	"authorization": {},
	"cookie":        {},
	"set-cookie":    {},
	"root_path":     {},
	"rel_path":      {},
	"mount_from":    {},
}

// IsSensitive reports whether an attribute key must be redacted.
func IsSensitive(key string) bool {
	_, ok := sensitiveKeys[strings.ToLower(key)]
	return ok
}

// Redact returns a slog.ReplaceAttr function that masks sensitive attributes
// unless the handler runs at debug level, where rel_path is allowed (design
// §6.11 keeps debug off by default).
func Redact(level slog.Level) func([]string, slog.Attr) slog.Attr {
	debug := level <= slog.LevelDebug
	return func(_ []string, a slog.Attr) slog.Attr {
		key := strings.ToLower(a.Key)
		if !IsSensitive(key) {
			return a
		}
		if debug && key == "rel_path" {
			return a
		}
		return slog.String(a.Key, Placeholder)
	}
}

// RedactValue masks a string when its key is sensitive; helpers that build
// attributes by hand use it.
func RedactValue(key, value string) string {
	if IsSensitive(key) {
		return Placeholder
	}
	return value
}
