package archive

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func subject(ref, name, dob string) ResolutionMention {
	m := ResolutionMention{Ref: ref, Kind: "person", Name: name, Role: "patient", Subject: true, Evidence: []string{"name-evidence"}}
	if dob != "" {
		m.Birth = IdentityDate{Precision: "date", Value: dob, Evidence: []string{"dob-evidence"}}
	}
	return m
}
func candidate(id, name, dob string) EntityCandidate {
	c := EntityCandidate{ID: id, Kind: "person", Version: 1, Names: []CandidateName{{Value: name}}}
	if dob != "" {
		c.Birth = IdentityDate{Precision: "date", Value: dob}
	}
	return c
}
func snapshot(cs ...EntityCandidate) CandidateSnapshot {
	return CandidateSnapshot{HouseholdID: "h", Revision: 1, Complete: true, Candidates: cs}
}
func decision(t *testing.T, m ResolutionMention, s CandidateSnapshot) ResolutionDecision {
	t.Helper()
	p, e := ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "doc", Mentions: []ResolutionMention{m}}, s)
	require.NoError(t, e)
	require.Len(t, p.Decisions, 1)
	require.Equal(t, ResolutionRuleVersion, p.RuleVersion)
	return p.Decisions[0]
}
func TestResolutionIdentityMatrix(t *testing.T) {
	tests := []struct {
		name string
		m    ResolutionMention
		s    CandidateSnapshot
		want string
	}{
		{"empty creates", subject("a", "王宁", ""), snapshot(), "create_entity"},
		{"name only", subject("a", "王宁", ""), snapshot(candidate("one", "王宁", "2000-01-02")), "needs_resolution"},
		{"full birthday", subject("a", "王宁", "2000-01-02"), snapshot(candidate("one", "王宁", "2000-01-02")), "link_existing"},
		{"different birthday", subject("a", "王宁", "2001-01-02"), snapshot(candidate("one", "王宁", "2000-01-02")), "create_entity"},
		{"ambiguous candidates", subject("a", "王宁", "2000-01-02"), snapshot(candidate("one", "王宁", "2000-01-02"), candidate("two", "王宁", "2000-01-02")), "needs_resolution"},
		{"relative", subject("a", "妈妈", ""), snapshot(), "needs_resolution"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, decision(t, tt.m, tt.s).Action) })
	}
	m := subject("a", "王宁", "2000")
	m.Birth.Precision = "year"
	require.Equal(t, "needs_resolution", decision(t, m, snapshot(candidate("one", "王宁", "2000-01-02"))).Action)
	m = subject("a", "王宁", "2000-01-02")
	m.OCRConflict = true
	require.Equal(t, "needs_resolution", decision(t, m, snapshot()).Action)
	s := snapshot()
	s.Complete = false
	require.Equal(t, "needs_resolution", decision(t, subject("a", "王宁", ""), s).Action)
}
func TestResolutionStrongAndBindings(t *testing.T) {
	m := subject("a", "王宁", "2000-01-02")
	s := snapshot(candidate("one", "另名", "2000-01-02"))
	s.Strong = map[string]StrongIdentityMatch{"a": {Supported: true, Complete: true, EntityIDs: []string{"one"}, Evidence: []string{"id-field"}}}
	m.HasStrongIdentity = true
	require.Equal(t, "link_existing", decision(t, m, s).Action)
	m.Birth.Value = "2001-01-02"
	require.Equal(t, "needs_resolution", decision(t, m, s).Action)
	s.Strong["a"] = StrongIdentityMatch{}
	require.Equal(t, "needs_resolution", decision(t, m, s).Action)
	m = subject("a", "阿布", "")
	s = snapshot(candidate("one", "林小布", ""))
	s.Bindings = []ConfirmedBinding{{Scope: "legacy_member", ScopeKey: "abu", EntityID: "one", Version: 1, Evidence: []string{"legacy"}}}
	require.Equal(t, "create_entity", decision(t, m, s).Action)
	m.BindingScope = "legacy_member"
	m.BindingKey = "abu"
	require.Equal(t, "link_existing", decision(t, m, s).Action)
	m.BindingScope = "document_mention"
	m.BindingKey = "doc#a"
	s.Bindings[0].Scope = "document_mention"
	s.Bindings[0].ScopeKey = "other#a"
	require.Equal(t, "create_entity", decision(t, m, s).Action)
	s.Bindings[0].ScopeKey = "doc#a"
	require.Equal(t, "link_existing", decision(t, m, s).Action)
	s.Bindings = append(s.Bindings, ConfirmedBinding{Scope: "document_mention", ScopeKey: "doc#a", EntityID: "two", Version: 1})
	require.Equal(t, "needs_resolution", decision(t, m, s).Action)
}
func TestResolutionAliasesRolesAndBatch(t *testing.T) {
	m := subject("a", "小宁", "2000-01-02")
	c := candidate("one", "王宁", "2000-01-02")
	c.Names = append(c.Names, CandidateName{Value: "小宁", Alias: true})
	require.Equal(t, "needs_resolution", decision(t, m, snapshot(c)).Action)
	// Display aliases never become a permanent identity rule.
	require.Equal(t, "needs_resolution", decision(t, m, snapshot(c)).Action)
	related := subject("doctor", "李医生", "")
	related.Role = "doctor"
	related.Subject = false
	p, err := ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "doc", Mentions: []ResolutionMention{related}}, snapshot())
	require.NoError(t, err)
	require.Len(t, p.LocalRoles, 1)
	require.Equal(t, "household_document", p.Decisions[0].Action)
	a := subject("a", "王宁", "2000-01-02")
	b := subject("b", "王宁", "2000-01-02")
	p, err = ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "doc", Mentions: []ResolutionMention{a, b}}, snapshot())
	require.NoError(t, err)
	require.Equal(t, "create_entity", p.Decisions[0].Action)
	require.Equal(t, "link_existing", p.Decisions[1].Action)
	require.Equal(t, p.Decisions[0].PlanRef, p.Decisions[1].PlanRef)
	require.Empty(t, p.Decisions[1].EntityID)
	b.Birth = IdentityDate{}
	p, err = ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "doc", Mentions: []ResolutionMention{a, b}}, snapshot())
	require.NoError(t, err)
	require.Equal(t, "needs_resolution", p.Decisions[1].Action)
}
func TestResolutionPropertyAndMultipleOwners(t *testing.T) {
	a := subject("a", "王宁", "")
	a.Role = "owner"
	b := subject("b", "林宁", "")
	b.Role = "insured"
	p := ResolutionMention{Ref: "p", Kind: "property", Name: "幸福路 1号 2栋 301", Role: "property", Subject: true, FullAddress: true, Evidence: []string{"address"}}
	s := snapshot(EntityCandidate{ID: "house", Version: 1, Kind: "property", Names: []CandidateName{{Value: p.Name}}})
	out, e := ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "doc", Mentions: []ResolutionMention{a, b, p}, Fields: []ResolutionField{{Ref: "ownership", SubjectRefs: []string{"a", "b", "p"}}}}, s)
	require.NoError(t, e)
	require.Len(t, out.Decisions, 3)
	require.Equal(t, "house", out.Decisions[2].EntityID)
	require.Equal(t, []string{"a", "b", "p"}, out.Fields[0].SubjectRefs)
	p.Name = "幸福路 1号 2栋 302"
	require.Equal(t, "create_entity", decision(t, p, s).Action)
	p.Kind = "device"
	require.Equal(t, "needs_resolution", decision(t, p, snapshot()).Action)
}
func TestResolutionNameKeyAndScopeValidation(t *testing.T) {
	require.Equal(t, NameKey(" ÉRIC\t A "), NameKey(" E\u0301ric  A"))
	require.NotEqual(t, NameKey("A-B"), NameKey("AB"))
	require.NotEqual(t, NameKey("王 宁"), NameKey("王宁"))
	require.NotEqual(t, NameKey("寧"), NameKey("宁"))
	_, e := ResolveEntities(ResolutionInput{HouseholdID: "other"}, snapshot())
	require.Error(t, e)
	_, e = ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: []ResolutionMention{subject("a", "A", ""), subject("a", "B", "")}}, snapshot())
	require.Error(t, e)
}

func TestResolutionPartialBirthConflictsAndInvalidDates(t *testing.T) {
	for _, d := range []IdentityDate{{Precision: "year", Value: "1991"}, {Precision: "month", Value: "1988-04"}, {Precision: "date", Value: "1988-02-31"}, {Precision: "bogus", Value: "1988-03-12"}} {
		m := subject("a", "Name", "")
		m.Birth = d
		m.HasStrongIdentity = true
		s := snapshot(candidate("one", "Name", "1988-03-12"))
		s.Strong = map[string]StrongIdentityMatch{"a": {Supported: true, Complete: true, EntityIDs: []string{"one"}}}
		require.Equal(t, "needs_resolution", decision(t, m, s).Action)
	}
	m := subject("a", "Name", "")
	m.Birth = IdentityDate{Precision: "date", Value: "1988-02-31"}
	require.Equal(t, "needs_resolution", decision(t, m, snapshot()).Action)
}

func TestResolutionSameBatchStrongIdentity(t *testing.T) {
	a := subject("a", "First Name", "2000-01-02")
	b := subject("b", "Other Name", "2000-01-02")
	a.HasStrongIdentity = true
	b.HasStrongIdentity = true
	s := snapshot()
	s.Strong = map[string]StrongIdentityMatch{"a": {Supported: true, Complete: true, PeerRefs: []string{"b"}}, "b": {Supported: true, Complete: true, PeerRefs: []string{"a"}}}
	p, e := ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: []ResolutionMention{a, b}}, s)
	require.NoError(t, e)
	require.Equal(t, "create_entity", p.Decisions[0].Action)
	require.Equal(t, "link_existing", p.Decisions[1].Action)
	require.Equal(t, p.Decisions[0].PlanRef, p.Decisions[1].PlanRef)
	b.Birth.Value = "2001-01-02"
	p, e = ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: []ResolutionMention{a, b}}, s)
	require.NoError(t, e)
	require.Equal(t, "needs_resolution", p.Decisions[0].Action)
	require.Equal(t, "needs_resolution", p.Decisions[1].Action)
}

func TestResolutionBirthAggregationOrderAndStickyConflict(t *testing.T) {
	a := IdentityDate{Precision: "date", Value: "1988-03-12"}
	b := IdentityDate{Precision: "date", Value: "1991-03-12"}
	for _, ds := range [][]IdentityDate{{a, b, a}, {a, a, b}, {b, a, a}} {
		got := IdentityDate{}
		for _, d := range ds {
			got = CombineIdentityDates(got, d)
		}
		require.True(t, got.Conflict)
	}
	a.Conflict = true
	require.True(t, CombineIdentityDates(a, IdentityDate{Precision: "date", Value: a.Value}).Conflict)
}
func TestResolutionRelatedRoleMayHaveIndependentSubjectEvidence(t *testing.T) {
	m := subject("doctor", "Dr Example", "")
	m.Role = "doctor"
	require.Equal(t, "create_entity", decision(t, m, snapshot()).Action)
	m.Subject = false
	p, e := ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: []ResolutionMention{m}}, snapshot())
	require.NoError(t, e)
	require.Equal(t, "household_document", p.Decisions[0].Action)
}

func TestResolutionRelativeNeedsExplicitScopedReferent(t *testing.T) {
	m := subject("a", "妈妈", "")
	m.Ambiguous = true
	m.BindingScope = "document_mention"
	m.BindingKey = "doc#a"
	s := snapshot(candidate("one", "Named Person", ""))
	s.Bindings = []ConfirmedBinding{{Scope: "document_mention", ScopeKey: "doc#a", EntityID: "one", ConfirmedBy: "organizer", Version: 1, Evidence: []string{"confirmed-referent"}}}
	require.Equal(t, "link_existing", decision(t, m, s).Action)
	m.BindingKey = "other#a"
	require.Equal(t, "needs_resolution", decision(t, m, s).Action)
}

func TestResolutionReviewStrongGroupChecksAllMembersBeforeCreate(t *testing.T) {
	for _, order := range [][]int{{0, 1, 2}, {2, 0, 1}, {1, 2, 0}} {
		for _, fault := range []string{"birthday", "kind", "ocr", "identity", "incomplete"} {
			source := []ResolutionMention{subject("a", "A", ""), subject("b", "B", "1988-03-12"), subject("c", "C", "1988-03-12")}
			switch fault {
			case "birthday":
				source[2].Birth.Value = "1991-03-12"
			case "kind":
				source[2].Kind = "property"
			case "ocr":
				source[2].OCRConflict = true
			}
			s := snapshot()
			s.Strong = map[string]StrongIdentityMatch{}
			for i := range source {
				source[i].HasStrongIdentity = true
				s.Strong[source[i].Ref] = StrongIdentityMatch{Supported: true, Complete: true}
			}
			// A non-clique peer graph still denotes one complete connected group.
			a := s.Strong["a"]
			a.PeerRefs = []string{"b"}
			s.Strong["a"] = a
			b := s.Strong["b"]
			b.PeerRefs = []string{"c"}
			s.Strong["b"] = b
			c := s.Strong["c"]
			if fault == "identity" {
				c.Conflict = true
			}
			if fault == "incomplete" {
				c.Complete = false
			}
			s.Strong["c"] = c
			ms := []ResolutionMention{source[order[0]], source[order[1]], source[order[2]]}
			p, e := ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: ms}, s)
			require.NoError(t, e)
			for i, d := range p.Decisions {
				require.Equal(t, ms[i].Ref, d.MentionRef)
				require.Equal(t, "needs_resolution", d.Action, fault)
			}
		}
	}
}
func TestResolutionReviewGroupDoesNotInventMissingBirth(t *testing.T) {
	a := subject("a", "A", "")
	b := subject("b", "B", "1988-03-12")
	c := subject("c", "C", "1988-03-12")
	a.HasStrongIdentity = true
	b.HasStrongIdentity = true
	c.HasStrongIdentity = true
	s := snapshot()
	s.Strong = map[string]StrongIdentityMatch{"a": {Supported: true, Complete: true, PeerRefs: []string{"b"}}, "b": {Supported: true, Complete: true, PeerRefs: []string{"c"}}, "c": {Supported: true, Complete: true}}
	in := ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: []ResolutionMention{a, c, b}}
	p, e := ResolveEntities(in, s)
	require.NoError(t, e)
	require.Equal(t, "create_entity", p.Decisions[0].Action)
	for _, d := range p.Decisions[1:] {
		require.Equal(t, "link_existing", d.Action)
		require.Equal(t, p.Decisions[0].PlanRef, d.PlanRef)
	}
	require.Empty(t, in.Mentions[0].Birth.Value)
	require.Empty(t, in.Mentions[0].Birth.Precision)
}
func TestResolutionReviewLinkedBirthUpdatesOnlyTemporaryCandidate(t *testing.T) {
	c := candidate("one", "Primary", "")
	s := snapshot(c)
	s.Bindings = []ConfirmedBinding{{Scope: "document_mention", ScopeKey: "doc#a", EntityID: "one", Version: 1, Evidence: []string{"confirmed"}}}
	a := subject("a", "Alias", "2000-01-02")
	a.BindingScope = "document_mention"
	a.BindingKey = "doc#a"
	b := subject("b", "Primary", "2000-01-02")
	p, e := ResolveEntities(ResolutionInput{HouseholdID: "h", DocumentID: "doc", Mentions: []ResolutionMention{a, b}}, s)
	require.NoError(t, e)
	require.Equal(t, "link_existing", p.Decisions[1].Action)
	require.Empty(t, s.Candidates[0].Birth.Value)
	require.Len(t, s.Candidates[0].Names, 1)
}
