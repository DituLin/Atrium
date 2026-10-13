package store

import (
	"context"
	"database/sql"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEntityReviewDifferentNationalIDs(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "one", true)
	entityDoc(t, d, "two", true)
	in, p := entityPlan(t, d, "one", entityMention("a", "Same Name", "2000-01-02"))
	id := applyEntity(t, d, in, p)["a"]
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		return archiveProtectedIdentityPutIn(context.Background(), tx, "h", id, ProtectedIdentityFact{DocumentID: "one", Kind: "national_id", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "001234", Evidence: []string{"original-id"}})
	}))
	m := entityMention("b", "Same Name", "2000-01-02")
	m.HasStrongIdentity = true
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		s, e := archiveEntitySnapshotIn(context.Background(), tx, "h", map[string]IdentityProbe{"b": {Kind: "national_id", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "009999", Evidence: []string{"different-id"}}})
		if e != nil {
			return e
		}
		p, e := archive.ResolveEntities(archive.ResolutionInput{HouseholdID: "h", DocumentID: "two", Mentions: []archive.ResolutionMention{m}}, s)
		if e != nil {
			return e
		}
		t.Logf("different national IDs: action=%s reason=%s", p.Decisions[0].Action, p.Decisions[0].Reason)
		require.Equal(t, "needs_resolution", p.Decisions[0].Action)
		return nil
	}))
}
func TestEntityReviewStrongGroupConflict(t *testing.T) {
	d := entityDB(t)
	ms := []archive.ResolutionMention{entityMention("a", "A", ""), entityMention("b", "B", "1988-03-12"), entityMention("c", "C", "1991-03-12")}
	probes := map[string]IdentityProbe{}
	for i := range ms {
		ms[i].HasStrongIdentity = true
		probes[ms[i].Ref] = IdentityProbe{Kind: "national_id", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "001234", Evidence: []string{"id"}}
	}
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		s, e := archiveEntitySnapshotIn(context.Background(), tx, "h", probes)
		if e != nil {
			return e
		}
		p, e := archive.ResolveEntities(archive.ResolutionInput{HouseholdID: "h", DocumentID: "group", Mentions: ms}, s)
		if e != nil {
			return e
		}
		for _, x := range p.Decisions {
			t.Logf("mention=%s action=%s reason=%s", x.MentionRef, x.Action, x.Reason)
		}
		for _, x := range p.Decisions {
			require.Equal(t, "needs_resolution", x.Action)
		}
		return nil
	}))
}

func TestEntityReviewSameBatchAlias(t *testing.T) {
	ms := []archive.ResolutionMention{entityMention("a", "Primary", "2000-01-02"), entityMention("b", "Alias", "2000-01-02"), entityMention("c", "Alias", "2000-01-02")}
	ms[0].HasStrongIdentity = true
	ms[1].HasStrongIdentity = true
	s := archive.CandidateSnapshot{HouseholdID: "h", Complete: true, Strong: map[string]archive.StrongIdentityMatch{"a": {Supported: true, Complete: true, PeerRefs: []string{"b"}}, "b": {Supported: true, Complete: true, PeerRefs: []string{"a"}}}}
	p, e := archive.ResolveEntities(archive.ResolutionInput{HouseholdID: "h", DocumentID: "doc", Mentions: ms}, s)
	require.NoError(t, e)
	for _, x := range p.Decisions {
		t.Logf("mention=%s action=%s reason=%s", x.MentionRef, x.Action, x.Reason)
	}
	require.Equal(t, "needs_resolution", p.Decisions[2].Action)
}

func TestEntityReviewNegativeIdentityScopeAndNewEntity(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "old", true)
	in, p := entityPlan(t, d, "old", entityMention("a", "Existing", "2000-01-02"))
	id := applyEntity(t, d, in, p)["a"]
	ctx := context.Background()
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		return archiveProtectedIdentityPutIn(ctx, tx, "h", id, ProtectedIdentityFact{DocumentID: "old", Kind: "national_id", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "001234", Evidence: []string{"old-id"}})
	}))
	for _, tt := range []struct{ name, issuer, birth, want string }{{"New Subject", "SG", "2000-01-02", "create_entity"}, {"Existing", "OTHER", "2000-01-02", "link_existing"}, {"Existing", "SG", "2001-01-02", "create_entity"}} {
		m := entityMention("x", tt.name, tt.birth)
		m.HasStrongIdentity = true
		require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
			s, e := archiveEntitySnapshotIn(ctx, tx, "h", map[string]IdentityProbe{"x": {Kind: "national_id", Issuer: tt.issuer, RuleVersion: IdentityExactVersion, Value: "009999", Evidence: []string{"new-id"}}})
			if e != nil {
				return e
			}
			p, e := archive.ResolveEntities(archive.ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: []archive.ResolutionMention{m}}, s)
			require.Equal(t, tt.want, p.Decisions[0].Action)
			return e
		}))
	}
}
func TestEntityReviewDifferentBatchStableIDs(t *testing.T) {
	d := entityDB(t)
	ms := []archive.ResolutionMention{entityMention("a", "Same", "2000-01-02"), entityMention("b", "Same", "2000-01-02")}
	probes := map[string]IdentityProbe{}
	for i := range ms {
		ms[i].HasStrongIdentity = true
		probes[ms[i].Ref] = IdentityProbe{Kind: "national_id", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: []string{"001234", "009999"}[i], Evidence: []string{"id"}}
	}
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		s, e := archiveEntitySnapshotIn(context.Background(), tx, "h", probes)
		if e != nil {
			return e
		}
		p, e := archive.ResolveEntities(archive.ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: ms}, s)
		require.Equal(t, "needs_resolution", p.Decisions[1].Action)
		return e
	}))
}
