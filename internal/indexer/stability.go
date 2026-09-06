package indexer

import (
	"sync"
	"time"
)

// Observation is one sighting of a candidate file.
type Observation struct {
	Size  int64
	Mtime int64
	// At is when the current run of identical sightings was last extended.
	At time.Time
	// Count is how many spaced-out identical sightings have been recorded.
	Count int
}

// StabilitySet holds candidates that have not yet been observed unchanged for
// long enough to index. It exists because a file appearing on an SMB share may
// still be being written: size and mtime must hold still across several checks
// before Atrium reads it (PRD §5.2). The set survives across scans, so a file
// that is still growing at the end of one scan simply carries over.
type StabilitySet struct {
	mu      sync.Mutex
	entries map[string]*Observation
}

// NewStabilitySet creates an empty set.
func NewStabilitySet() *StabilitySet {
	return &StabilitySet{entries: map[string]*Observation{}}
}

// Rules configures the stability gate.
type Rules struct {
	// Checks is how many spaced sightings are required (>= 1).
	Checks int
	// Interval is the minimum gap between two counted sightings.
	Interval time.Duration
}

// Observe records a sighting and reports whether the file is now stable.
func (s *StabilitySet) Observe(rel string, size, mtime int64, now time.Time, rules Rules) bool {
	if rules.Checks <= 1 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[rel]
	if !ok {
		s.entries[rel] = &Observation{Size: size, Mtime: mtime, At: now, Count: 1}
		return false
	}
	if entry.Size != size || entry.Mtime != mtime {
		// The file changed under us: the countdown starts again.
		entry.Size, entry.Mtime, entry.At, entry.Count = size, mtime, now, 1
		return false
	}
	if now.Sub(entry.At) < rules.Interval {
		// Too soon to count as an independent check.
		return false
	}
	entry.At = now
	entry.Count++
	if entry.Count >= rules.Checks {
		delete(s.entries, rel)
		return true
	}
	return false
}

// Pending returns the paths still waiting for stability, in no particular
// order. The scan re-stats them at the end of the walk.
func (s *StabilitySet) Pending() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.entries))
	for rel := range s.entries {
		out = append(out, rel)
	}
	return out
}

// Len returns the number of pending candidates.
func (s *StabilitySet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Forget drops a candidate, used when its path disappears or is excluded.
func (s *StabilitySet) Forget(rel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, rel)
}

// Reset clears the set; a full scan starts from scratch.
func (s *StabilitySet) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]*Observation{}
}
