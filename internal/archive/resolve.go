package archive

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const ResolutionRuleVersion = "entity-resolution/2"
const NameKeyVersion = "nfc-space-ascii/1-xtext-0.31.0"

// NameKey preserves punctuation, word boundaries and all non-ASCII case.
func NameKey(s string) string {
	s = strings.Join(strings.Fields(norm.NFC.String(s)), " ")
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

// References below are Core-validated immutable evidence/field references, not
// model-supplied durable entity IDs. They may point to protected source facts.
type IdentityDate struct {
	Precision, Value string
	Evidence         []string
	Conflict         bool
}
type ResolutionMention struct {
	Ref, Kind, Name, Role                                           string
	Subject, Ambiguous, OCRConflict, FullAddress, HasStrongIdentity bool
	Evidence                                                        []string
	Birth                                                           IdentityDate
	BindingScope, BindingKey                                        string // set by trusted Core, never an LLM
}
type ResolutionField struct {
	Ref         string
	SubjectRefs []string
}
type ResolutionInput struct {
	HouseholdID, DocumentID string
	Mentions                []ResolutionMention
	Fields                  []ResolutionField
}
type CandidateName struct {
	Value    string
	Alias    bool
	Evidence []string
}
type EntityCandidate struct {
	ID, PlanRef, Kind string
	Version           int64
	Names             []CandidateName
	Birth             IdentityDate
	Conflict          bool
}
type ConfirmedBinding struct {
	Scope, ScopeKey, EntityID, ConfirmedBy string
	Version                                int64
	Evidence                               []string
	Conflict                               bool
}

// StrongIdentityMatch deliberately has no raw identity value or hash. The store
// compares bounded protected facts and supplies only these match conclusions.
type StrongIdentityMatch struct {
	Supported, Complete, Conflict             bool
	EntityIDs, Evidence, PeerRefs             []string
	ConflictingEntityIDs, ConflictingPeerRefs []string
}
type CandidateSnapshot struct {
	HouseholdID string
	Revision    int64
	Complete    bool
	Candidates  []EntityCandidate
	Bindings    []ConfirmedBinding
	Strong      map[string]StrongIdentityMatch
}
type ResolutionDecision struct {
	MentionRef, Action, EntityID, PlanRef, Reason string
	Evidence                                      []string
}
type ResolutionPlan struct {
	HouseholdID, DocumentID, RuleVersion, NameVersion string
	Revision                                          int64
	Dependencies                                      map[string]int64
	Decisions                                         []ResolutionDecision
	LocalRoles                                        []ResolutionMention
	Fields                                            []ResolutionField
}

func fullBirth(d IdentityDate) bool {
	if d.Conflict || d.Precision != "date" {
		return false
	}
	t, e := time.Parse("2006-01-02", d.Value)
	return e == nil && t.Format("2006-01-02") == d.Value
}

// Known partial date components can contradict identity, but never prove it.
func validIdentityDate(d IdentityDate) bool {
	if d.Precision == "" || d.Precision == "unknown" {
		return d.Value == ""
	}
	layout := ""
	switch d.Precision {
	case "year":
		layout = "2006"
	case "month":
		layout = "2006-01"
	case "date":
		layout = "2006-01-02"
	default:
		return false
	}
	v, e := time.Parse(layout, d.Value)
	return e == nil && v.Format(layout) == d.Value
}
func birthConflict(a, b IdentityDate) bool {
	if a.Conflict || b.Conflict || !validIdentityDate(a) || !validIdentityDate(b) {
		return true
	}
	if a.Value == "" || b.Value == "" {
		return false
	}
	n := len(a.Value)
	if len(b.Value) < n {
		n = len(b.Value)
	}
	return a.Value[:n] != b.Value[:n]
}

// CombineIdentityDates keeps known components without inventing missing ones.
// Conflicts are monotonic and aggregation order cannot erase them.
func CombineIdentityDates(a, b IdentityDate) IdentityDate {
	conflict := birthConflict(a, b)
	out := a
	if len(b.Value) > len(a.Value) || len(b.Value) == len(a.Value) && b.Value < a.Value {
		out = b
	}
	out.Conflict = conflict
	out.Evidence = append(append([]string(nil), a.Evidence...), b.Evidence...)
	sort.Strings(out.Evidence)
	return out
}

func relativeName(s string) bool {
	switch NameKey(s) {
	case "妈妈", "媽媽", "爸爸", "母亲", "母親", "父亲", "父親", "奶奶", "爷爷", "爺爺", "外婆", "外公", "mum", "mom", "mother", "dad", "father":
		return true
	}
	return false
}
func validResolutionText(s string) bool {
	return utf8.ValidString(s) && len(s) <= 2048 && !strings.ContainsRune(s, 0)
}

// ResolveEntities is deterministic and side-effect free. Its complete snapshot
// is a Core capability; never manufacture one from filtered model context.
func ResolveEntities(in ResolutionInput, s CandidateSnapshot) (ResolutionPlan, error) {
	p := ResolutionPlan{HouseholdID: in.HouseholdID, DocumentID: in.DocumentID, RuleVersion: ResolutionRuleVersion, NameVersion: NameKeyVersion, Revision: s.Revision, Dependencies: map[string]int64{}, Fields: in.Fields}
	bad := errors.New("invalid entity resolution input")
	if in.HouseholdID == "" || s.HouseholdID != in.HouseholdID || s.Revision < 0 || len(in.Mentions) > 2048 || len(s.Candidates) > 4096 {
		return p, bad
	}
	seen := map[string]bool{}
	for _, m := range in.Mentions {
		if m.Ref == "" || seen[m.Ref] || !validResolutionText(m.Ref) || !validResolutionText(m.Name) || !validResolutionText(m.Role) {
			return p, bad
		}
		seen[m.Ref] = true
	}
	for _, f := range in.Fields {
		for _, r := range f.SubjectRefs {
			if !seen[r] {
				return p, bad
			}
		}
	}
	candidates := append([]EntityCandidate(nil), s.Candidates...)
	for _, c := range candidates {
		if c.ID == "" || c.PlanRef != "" || c.Version < 1 || p.Dependencies[c.ID] != 0 {
			return p, bad
		}
		p.Dependencies[c.ID] = c.Version
	}
	groups := resolutionIdentityGroups(in, s)
	resolvedTargets := map[string]string{}
	for _, m := range in.Mentions {
		if !m.Subject {
			p.LocalRoles = append(p.LocalRoles, m)
			continue
		}
		d := ResolutionDecision{MentionRef: m.Ref, Action: "needs_resolution", Reason: "insufficient_identity", Evidence: append([]string(nil), m.Evidence...)}
		d.Evidence = append(d.Evidence, m.Birth.Evidence...)
		finish := func() {
			if d.Action == "link_existing" {
				for i := range candidates {
					c := &candidates[i]
					if !(d.EntityID != "" && c.ID == d.EntityID || d.PlanRef != "" && c.PlanRef == d.PlanRef) {
						continue
					}
					// Every linked spelling becomes a candidate for subsequent
					// mentions, but an alias never upgrades itself to primary.
					found := false
					for _, n := range c.Names {
						if NameKey(n.Value) == NameKey(m.Name) {
							found = true
						}
					}
					if !found {
						c.Names = append(append([]CandidateName(nil), c.Names...), CandidateName{Value: m.Name, Alias: true, Evidence: append([]string(nil), m.Evidence...)})
					}
					c.Birth = CombineIdentityDates(c.Birth, m.Birth)
				}
			}
			if d.Action == "create_entity" || d.Action == "link_existing" {
				target := d.EntityID
				if target == "" {
					target = d.PlanRef
				}
				resolvedTargets[m.Ref] = target
			}
			p.Decisions = append(p.Decisions, d)
		}
		if m.OCRConflict || m.Birth.Conflict || !validIdentityDate(m.Birth) {
			d.Reason = "conflicting_evidence"
			finish()
			continue
		}
		if !s.Complete {
			d.Reason = "incomplete_snapshot"
			finish()
			continue
		}
		if g := groups[m.Ref]; g.Conflict {
			d.Reason = "identity_group_conflict"
			d.Evidence = append(d.Evidence, g.Evidence...)
			finish()
			continue
		}
		if m.Kind != "person" && m.Kind != "property" {
			d.Reason = "unsupported_identity"
			finish()
			continue
		}
		if NameKey(m.Name) == "" || len(m.Evidence) == 0 || m.Role == "" {
			finish()
			continue
		}
		if m.Kind == "property" && !m.FullAddress {
			d.Reason = "incomplete_address"
			finish()
			continue
		}
		identityConflicts := map[string]bool{}
		for _, id := range s.Strong[m.Ref].ConflictingEntityIDs {
			identityConflicts[id] = true
		}
		for _, ref := range s.Strong[m.Ref].ConflictingPeerRefs {
			if target := resolvedTargets[ref]; target != "" {
				identityConflicts[target] = true
			}
		}
		targets := map[string]bool{}
		conflict := false
		if m.HasStrongIdentity {
			r, ok := s.Strong[m.Ref]
			d.Evidence = append(d.Evidence, r.Evidence...)
			if !ok || !r.Supported || !r.Complete {
				d.Reason = "identity_comparison_incomplete"
				finish()
				continue
			}
			conflict = r.Conflict
			for _, ref := range groups[m.Ref].Refs {
				if target := resolvedTargets[ref]; target != "" {
					targets[target] = true
				}
			}
			for _, id := range groups[m.Ref].TargetIDs {
				targets[id] = true
			}
		}
		binding := false
		if m.BindingScope == "legacy_member" || m.BindingScope == "document_mention" && m.BindingKey == in.DocumentID+"#"+m.Ref {
			for _, b := range s.Bindings {
				if b.Scope == m.BindingScope && b.ScopeKey == m.BindingKey {
					targets[b.EntityID] = true
					binding = true
					conflict = conflict || b.Conflict
					d.Evidence = append(d.Evidence, b.Evidence...)
				}
			}
		}
		link := func(c EntityCandidate, reason string) {
			d.Action = "link_existing"
			d.EntityID = c.ID
			d.PlanRef = c.PlanRef
			d.Reason = reason
		}
		if len(targets) > 0 || conflict {
			if len(targets) == 1 && !conflict {
				found := false
				for _, c := range candidates {
					if (targets[c.ID] || targets[c.PlanRef]) && c.Kind == m.Kind {
						found = true
						if !(identityConflicts[c.ID] || identityConflicts[c.PlanRef]) && !c.Conflict && !birthConflict(m.Birth, c.Birth) {
							reason := "strong_identity"
							if binding {
								reason = "confirmed_binding"
							}
							link(c, reason)
						}
					}
				}
				if !found {
					d.Reason = "identity_target_missing"
				}
			} else {
				d.Reason = "identity_conflict"
			}
			finish()
			continue
		}
		if m.Ambiguous || relativeName(m.Name) {
			d.Reason = "ambiguous_subject"
			finish()
			continue
		}
		named := []EntityCandidate{}
		eligible := []EntityCandidate{}
		for _, c := range candidates {
			if c.Kind != m.Kind {
				continue
			}
			nameMatch, proven := false, false
			for _, n := range c.Names {
				if NameKey(n.Value) == NameKey(m.Name) {
					nameMatch = true
					proven = proven || !n.Alias
				}
			}
			if nameMatch {
				named = append(named, c)
				if proven && !(identityConflicts[c.ID] || identityConflicts[c.PlanRef]) && !c.Conflict && !birthConflict(m.Birth, c.Birth) && (m.Kind == "property" || fullBirth(m.Birth) && fullBirth(c.Birth) && m.Birth.Value == c.Birth.Value) {
					eligible = append(eligible, c)
				}
			}
		}
		// A competing same-name candidate with unknown identity prevents selecting
		// the birthday match. Explicitly different full birthdays are distinguishable.
		competing := false
		for _, c := range named {
			if c.Conflict || c.Birth.Conflict || m.Kind == "person" && (!fullBirth(c.Birth) || !fullBirth(m.Birth)) || (identityConflicts[c.ID] || identityConflicts[c.PlanRef]) && (!fullBirth(c.Birth) || !fullBirth(m.Birth) || c.Birth.Value == m.Birth.Value) {
				competing = true
			}
		}
		if len(eligible) == 1 && !competing {
			link(eligible[0], "name_full_identity")
			finish()
			continue
		}
		create := len(named) == 0
		if len(named) > 0 && m.Kind == "person" && fullBirth(m.Birth) {
			create = true
			for _, c := range named {
				if c.Conflict || !fullBirth(c.Birth) || c.Birth.Value == m.Birth.Value {
					create = false
				}
			}
		}
		if create {
			d.Action = "create_entity"
			d.PlanRef = fmt.Sprintf("new:%d", len(p.Decisions))
			d.Reason = "distinct_subject"
			candidates = append(candidates, EntityCandidate{PlanRef: d.PlanRef, Kind: m.Kind, Names: []CandidateName{{Value: m.Name}}, Birth: m.Birth})
		} else {
			d.Reason = "ambiguous_candidates"
		}
		finish()
	}
	if len(p.Decisions) == 0 {
		p.Decisions = append(p.Decisions, ResolutionDecision{Action: "household_document", Reason: "no_archive_subject"})
	}
	// Canonical output is stable even when store query order changes.
	for i := range p.Decisions {
		sort.Strings(p.Decisions[i].Evidence)
	}
	return p, nil
}

// Inspect each complete peer component before emitting any create decision.
// A member with no birthday cannot mask a conflict between two other members.
// No aggregated date is assigned back to a mention or used as matching proof.
type resolutionIdentityGroup struct {
	Conflict                  bool
	Evidence, Refs, TargetIDs []string
}

func resolutionIdentityGroups(in ResolutionInput, s CandidateSnapshot) map[string]resolutionIdentityGroup {
	mentions := map[string]ResolutionMention{}
	adj := map[string][]string{}
	for _, m := range in.Mentions {
		mentions[m.Ref] = m
		if m.HasStrongIdentity {
			for _, peer := range s.Strong[m.Ref].PeerRefs {
				adj[m.Ref] = append(adj[m.Ref], peer)
				adj[peer] = append(adj[peer], m.Ref)
			}
		}
	}
	result := map[string]resolutionIdentityGroup{}
	seen := map[string]bool{}
	for _, start := range in.Mentions {
		if !start.HasStrongIdentity || seen[start.Ref] {
			continue
		}
		refs := []string{start.Ref}
		seen[start.Ref] = true
		for i := 0; i < len(refs); i++ {
			for _, ref := range adj[refs[i]] {
				if !seen[ref] {
					seen[ref] = true
					refs = append(refs, ref)
				}
			}
		}
		g := resolutionIdentityGroup{Refs: refs}
		birth := IdentityDate{}
		targets := map[string]bool{}
		negative := map[string]bool{}
		for _, ref := range refs {
			m, exists := mentions[ref]
			r, reported := s.Strong[ref]
			g.Conflict = g.Conflict || !exists || !m.HasStrongIdentity || !reported || !r.Supported || !r.Complete || r.Conflict || m.Kind != start.Kind || m.OCRConflict
			birth = CombineIdentityDates(birth, m.Birth)
			g.Evidence = append(g.Evidence, m.Evidence...)
			g.Evidence = append(g.Evidence, m.Birth.Evidence...)
			g.Evidence = append(g.Evidence, r.Evidence...)
			for _, id := range r.EntityIDs {
				targets[id] = true
			}
			for _, id := range r.ConflictingEntityIDs {
				negative[id] = true
			}
			if m.BindingScope == "legacy_member" || m.BindingScope == "document_mention" && m.BindingKey == in.DocumentID+"#"+m.Ref {
				for _, b := range s.Bindings {
					if b.Scope == m.BindingScope && b.ScopeKey == m.BindingKey {
						targets[b.EntityID] = true
						g.Conflict = g.Conflict || b.Conflict
						g.Evidence = append(g.Evidence, b.Evidence...)
					}
				}
			}
		}
		g.Conflict = g.Conflict || birth.Conflict || len(targets) > 1
		for id := range targets {
			g.TargetIDs = append(g.TargetIDs, id)
			found := false
			for _, c := range s.Candidates {
				if c.ID == id {
					found = true
					g.Conflict = g.Conflict || c.Kind != start.Kind || c.Conflict || birthConflict(birth, c.Birth)
				}
			}
			g.Conflict = g.Conflict || !found || negative[id]
		}
		sort.Strings(g.TargetIDs)
		for _, ref := range refs {
			result[ref] = g
		}
	}
	return result
}
