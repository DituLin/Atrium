// Package diag assembles the operator's single view of the system
// (technical design §6.11) and keeps the ring buffer of recent errors that
// feeds it.
package diag

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// RingSize is the number of recent errors kept (design §6.11).
const RingSize = 100

// ErrorEntry is one recorded failure. It carries codes and IDs only: never a
// path, a token or a message that might embed one (design §15).
type ErrorEntry struct {
	At        time.Time `json:"at"`
	Component string    `json:"component"`
	Code      string    `json:"code"`
	PhotoID   string    `json:"photo_id,omitempty"`
	SourceID  string    `json:"source_id,omitempty"`
	ScreenID  string    `json:"screen_id,omitempty"`
}

// Errors is a bounded ring of recent failures that doubles as an slog.Handler.
// Wiring it into the logger is what lets every component feed diagnostics
// without importing this package or holding a reference to it.
type Errors struct {
	mu    sync.Mutex
	ring  []ErrorEntry
	next  int
	full  bool
	level slog.Level
	// attrs carries the component set by logger.With, since a record's own
	// attributes may not repeat it.
	attrs []slog.Attr
	// parent is set on handlers derived by WithAttrs so every entry lands in
	// the one ring the aggregator reads.
	parent *Errors
}

// NewErrors builds an empty ring that records warnings and errors.
func NewErrors() *Errors {
	return &Errors{ring: make([]ErrorEntry, RingSize), level: slog.LevelWarn}
}

// Enabled reports whether a level is recorded.
func (e *Errors) Enabled(_ context.Context, level slog.Level) bool { return level >= e.level }

// Handle extracts the diagnostic fields of one log record.
func (e *Errors) Handle(_ context.Context, r slog.Record) error {
	entry := ErrorEntry{At: r.Time, Code: "error"}
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	apply := func(a slog.Attr) {
		switch a.Key {
		case "component":
			entry.Component = a.Value.String()
		case "code", "event":
			// `code` is the specific failure; `event` is the fallback so an
			// error logged without one is still identifiable.
			if a.Key == "code" || entry.Code == "error" {
				entry.Code = a.Value.String()
			}
		case "photo_id":
			entry.PhotoID = a.Value.String()
		case "source_id":
			entry.SourceID = a.Value.String()
		case "screen_id":
			entry.ScreenID = a.Value.String()
		}
	}
	for _, a := range e.attrs {
		apply(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		apply(a)
		return true
	})
	if entry.Component == "" {
		entry.Component = "app"
	}
	e.record(entry)
	return nil
}

// WithAttrs returns a handler that remembers the pre-bound attributes.
func (e *Errors) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &Errors{level: e.level, parent: e.root()}
	next.attrs = append(append([]slog.Attr{}, e.attrs...), attrs...)
	return next
}

// WithGroup returns the handler unchanged: diagnostics read flat keys.
func (e *Errors) WithGroup(string) slog.Handler { return e }

// root walks to the handler that owns the ring.
func (e *Errors) root() *Errors {
	target := e
	for target.parent != nil {
		target = target.parent
	}
	return target
}

func (e *Errors) record(entry ErrorEntry) {
	target := e.root()
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.ring == nil {
		target.ring = make([]ErrorEntry, RingSize)
	}
	target.ring[target.next] = entry
	target.next = (target.next + 1) % RingSize
	if target.next == 0 {
		target.full = true
	}
}

// Recent returns the recorded errors, newest last, at most RingSize of them.
func (e *Errors) Recent() []ErrorEntry {
	target := e.root()
	target.mu.Lock()
	defer target.mu.Unlock()
	if !target.full {
		out := make([]ErrorEntry, target.next)
		copy(out, target.ring[:target.next])
		return out
	}
	out := make([]ErrorEntry, 0, RingSize)
	out = append(out, target.ring[target.next:]...)
	out = append(out, target.ring[:target.next]...)
	return out
}
