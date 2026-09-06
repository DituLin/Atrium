// Package version exposes build information injected at link time.
package version

import "runtime/debug"

var (
	// Version is the semantic version, set with -ldflags.
	Version = "0.1.0"
	// Commit is the short git SHA, set with -ldflags.
	Commit = ""
	// BuildDate is an RFC 3339 timestamp, set with -ldflags.
	BuildDate = ""
)

func init() {
	if Commit != "" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			Commit = s.Value[:7]
		}
	}
}

// String renders "version+commit" (commit omitted when unknown).
func String() string {
	if Commit == "" {
		return Version
	}
	return Version + "+" + Commit
}
