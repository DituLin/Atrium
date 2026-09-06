// Package indexer discovers files under an authorized source root and keeps
// the photo index in step with them: a serial, non-overlapping scan per
// source, a stability gate before a file is indexed, and a removal rule that
// refuses to act on anything but a completed scan (technical design §6.2,
// tasks B-203 and B-204).
package indexer

import (
	"context"
	"strings"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
)

// ExclusionSet answers "is this path authorized?" for one source. Rules live
// in the database rather than in memory, so they survive a full rescan and an
// index rebuild (design §5, B-213).
type ExclusionSet struct {
	paths    map[string]bool
	prefixes []string
}

// LoadExclusions reads the rules for one source.
func LoadExclusions(ctx context.Context, db *store.DB, sourceID string) (*ExclusionSet, error) {
	rows, err := db.Exclusions().List(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return NewExclusionSet(rows), nil
}

// NewExclusionSet builds a matcher from rules.
func NewExclusionSet(rows []domain.Exclusion) *ExclusionSet {
	set := &ExclusionSet{paths: map[string]bool{}}
	for _, r := range rows {
		pattern := strings.Trim(r.Pattern, "/")
		if pattern == "" {
			continue
		}
		if r.MatchKind == domain.MatchPrefix {
			set.prefixes = append(set.prefixes, pattern)
			continue
		}
		set.paths[pattern] = true
	}
	return set
}

// Excluded reports whether a relative path is covered by a rule.
func (s *ExclusionSet) Excluded(rel string) bool {
	if s == nil {
		return false
	}
	if s.paths[rel] {
		return true
	}
	for _, p := range s.prefixes {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// ExcludedDir reports whether a directory and everything under it is excluded.
// It is the same test as Excluded, named separately because the walker uses it
// to prune whole subtrees rather than to reject one file.
func (s *ExclusionSet) ExcludedDir(rel string) bool { return s.Excluded(rel) }

// Empty reports whether the set has no rules.
func (s *ExclusionSet) Empty() bool { return s == nil || (len(s.paths) == 0 && len(s.prefixes) == 0) }
