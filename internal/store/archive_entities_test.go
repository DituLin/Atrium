package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func entityDB(t *testing.T) *DB {
	t.Helper()
	d, e := Open(context.Background(), filepath.Join(t.TempDir(), "entities.db"))
	require.NoError(t, e)
	t.Cleanup(func() { d.Close() })
	_, e = d.Migrate(context.Background())
	require.NoError(t, e)
	_, e = d.sql.Exec(`INSERT INTO households(id) VALUES('h'),('other'); UPDATE local_household SET household_id='h' WHERE singleton=1; INSERT INTO family_members(id,name,updated_at) VALUES('abu','阿布','2026-01-01T00:00:00Z')`)
	require.NoError(t, e)
	return d
}
func entityDoc(t *testing.T, d *DB, id string, visible bool) {
	t.Helper()
	_, e := d.sql.Exec(`INSERT OR IGNORE INTO archive_blobs(household_id,sha256,size_bytes,mime,storage_version,created_at) VALUES('h',?,1,'text/plain',1,'now')`, string(makeHex()))
	require.NoError(t, e)
	_, e = d.sql.Exec(`INSERT INTO archive_documents(id,household_id,blob_sha256,input_kind,tv_visible,created_at) VALUES(?,'h',?,'text',?,'now')`, id, string(makeHex()), visible)
	require.NoError(t, e)
}
func makeHex() []byte {
	b := make([]byte, 64)
	for i := range b {
		b[i] = 'a'
	}
	return b
}
func entityMention(ref, name, dob string) archive.ResolutionMention {
	m := archive.ResolutionMention{Ref: ref, Name: name, Kind: "person", Role: "patient", Subject: true, Evidence: []string{"name"}}
	if dob != "" {
		m.Birth = archive.IdentityDate{Precision: "date", Value: dob, Evidence: []string{"birth"}}
	}
	return m
}
func entitySnap(t *testing.T, d *DB) archive.CandidateSnapshot {
	t.Helper()
	var s archive.CandidateSnapshot
	e := d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		var e error
		s, e = archiveEntitySnapshotIn(context.Background(), tx, "h", nil)
		return e
	})
	require.NoError(t, e)
	return s
}
func entityPlan(t *testing.T, d *DB, doc string, ms ...archive.ResolutionMention) (archive.ResolutionInput, archive.ResolutionPlan) {
	t.Helper()
	in := archive.ResolutionInput{HouseholdID: "h", DocumentID: doc, Mentions: ms}
	p, e := archive.ResolveEntities(in, entitySnap(t, d))
	require.NoError(t, e)
	return in, p
}
func applyEntity(t *testing.T, d *DB, in archive.ResolutionInput, p archive.ResolutionPlan) map[string]string {
	t.Helper()
	var ids map[string]string
	e := d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		var e error
		ids, e = archiveApplyResolutionIn(context.Background(), tx, in, p)
		return e
	})
	require.NoError(t, e)
	return ids
}
func TestEntityAtomicPlanAndRevisionFence(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	in, p := entityPlan(t, d, "d", entityMention("a", "王宁", "2000-01-02"))
	abort := errors.New("abort facts checkpoint")
	e := d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		_, e := archiveApplyResolutionIn(context.Background(), tx, in, p)
		require.NoError(t, e)
		_, e = tx.Exec(`CREATE TABLE atomic_checkpoint(id TEXT)`)
		require.NoError(t, e)
		return abort
	})
	require.ErrorIs(t, e, abort)
	require.Empty(t, entitySnap(t, d).Candidates)
	ids := applyEntity(t, d, in, p)
	require.NotEmpty(t, ids["a"])
	var members int
	require.NoError(t, d.sql.QueryRow(`SELECT count(*) FROM family_members`).Scan(&members))
	require.Equal(t, 1, members)
	e = d.InWriteTx(context.Background(), func(tx *sql.Tx) error { _, e := archiveApplyResolutionIn(context.Background(), tx, in, p); return e })
	require.ErrorIs(t, e, domain.ErrConflict)
	fresh := entitySnap(t, d)
	require.Greater(t, fresh.Revision, p.Revision)
	require.Len(t, fresh.Candidates, 1)
	in2, p2 := entityPlan(t, d, "d", entityMention("b", "王宁", "2000-01-02"))
	require.Equal(t, "link_existing", p2.Decisions[0].Action)
	require.Equal(t, ids["a"], applyEntity(t, d, in2, p2)["b"])
}
func TestEntityMergeUndoKeepsMatchedHistoryAndPrivateProjection(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "public", true)
	entityDoc(t, d, "secret", false)
	entityDoc(t, d, "later", true)
	in, p := entityPlan(t, d, "public", entityMention("a", "Public", "2000-01-02"))
	a := applyEntity(t, d, in, p)["a"]
	in, p = entityPlan(t, d, "secret", entityMention("b", "SECRET NAME", "2000-01-02"))
	b := applyEntity(t, d, in, p)["b"]
	var mergeID string
	e := d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		var e error
		mergeID, e = archiveMergeEntitiesIn(context.Background(), tx, "h", b, a, entitySnapVersion(t, tx, "h", b), entitySnapVersion(t, tx, "h", a), "organizer", "same person")
		return e
	})
	require.NoError(t, e)
	in, p = entityPlan(t, d, "later", entityMention("c", "Public", "2000-01-02"))
	applyEntity(t, d, in, p)
	var public []ArchiveEntityProjection
	e = d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		var e error
		public, e = archiveEntityPublicIn(context.Background(), tx, "h")
		return e
	})
	require.NoError(t, e)
	require.Len(t, public, 1)
	require.Equal(t, []string{"Public"}, public[0].Names)
	var matched string
	require.NoError(t, d.sql.QueryRow(`SELECT matched_entity_id FROM archive_entity_links WHERE mention_ref='b'`).Scan(&matched))
	require.Equal(t, b, matched)
	e = d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		return archiveUndoEntityMergeIn(context.Background(), tx, "h", mergeID, 1, "organizer", "not same")
	})
	require.NoError(t, e)
	var status string
	require.NoError(t, d.sql.QueryRow(`SELECT status FROM archive_entity_links WHERE mention_ref='c'`).Scan(&status))
	require.Equal(t, "needs_resolution", status)
	require.NoError(t, d.sql.QueryRow(`SELECT status FROM archive_entity_links WHERE mention_ref='b'`).Scan(&status))
	require.Equal(t, "linked", status)
	require.Len(t, entitySnap(t, d).Candidates, 2)
}
func entitySnapVersion(t *testing.T, tx *sql.Tx, h, id string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, tx.QueryRow(`SELECT version FROM archive_entities WHERE household_id=? AND id=?`, h, id).Scan(&n))
	return n
}
func TestEntityLegacyMappingStableAndScoped(t *testing.T) {
	d := entityDB(t)
	var a, b string
	for i := 0; i < 2; i++ {
		e := d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
			var e error
			b, e = archiveLegacyEntityIn(context.Background(), tx, "h", "abu")
			return e
		})
		require.NoError(t, e)
		if i == 0 {
			a = b
			_, e = d.sql.Exec(`UPDATE family_members SET name='小布' WHERE id='abu'`)
			require.NoError(t, e)
		}
	}
	require.Equal(t, a, b)
	s := entitySnap(t, d)
	require.Len(t, s.Candidates, 1)
	require.Len(t, s.Bindings, 1)
	require.Equal(t, "legacy_member", s.Bindings[0].Scope)
	require.Equal(t, a, s.Bindings[0].EntityID)
}
func TestEntityProtectedIdentityBoundedAndExact(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	in, p := entityPlan(t, d, "d", entityMention("a", "王宁", ""))
	id := applyEntity(t, d, in, p)["a"]
	e := d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		return archiveProtectedIdentityPutIn(context.Background(), tx, "h", id, ProtectedIdentityFact{DocumentID: "d", Kind: "national_id", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "001234", Evidence: []string{"id"}})
	})
	require.NoError(t, e)
	for _, tt := range []struct {
		value, kind, issuer        string
		complete, supported, match bool
	}{{"001234", "national_id", "SG", true, true, true}, {"1234", "national_id", "SG", true, true, false}, {"001234", "unknown", "SG", false, false, false}, {"001234", "national_id", "", false, false, false}} {
		e = d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
			r, e := archiveCompareIdentityIn(context.Background(), tx, "h", IdentityProbe{Kind: tt.kind, Issuer: tt.issuer, RuleVersion: IdentityExactVersion, Value: tt.value, Evidence: []string{"incoming"}})
			require.Equal(t, tt.complete, r.Complete)
			require.Equal(t, tt.supported, r.Supported)
			require.Equal(t, tt.match, len(r.EntityIDs) == 1)
			return e
		})
		require.NoError(t, e)
	}
	s := entitySnap(t, d)
	require.Len(t, s.Candidates, 1)
	require.Equal(t, "王宁", s.Candidates[0].Names[0].Value)
}
func TestEntityAuthorization(t *testing.T) {
	d := entityDB(t)
	ctx := context.Background()
	now := time.Now()
	p := &domain.IntegrationPrincipal{ID: "ingest", Label: "ingest", Enabled: true, PolicyVersion: 1, Policy: domain.IntegrationPolicy{Permissions: []string{"archive.ingest", "archive.organize"}, HouseholdIDs: []string{"h"}}, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	c := &domain.IntegrationCredential{ID: "cred", PrincipalID: p.ID, TokenHash: "synthetic", ExpiresAt: p.ExpiresAt, CreatedAt: now}
	require.NoError(t, d.Integrations().Create(ctx, p, c))
	actor := domain.IntegrationActor{PrincipalID: p.ID, CredentialID: c.ID}
	_, e := d.ArchiveEntities().Candidates(ctx, actor, "h")
	require.Error(t, e)
	_, e = d.ArchiveEntities().Candidates(ctx, actor, "other")
	require.ErrorIs(t, e, domain.ErrNotFound)
	_, e = d.ArchiveEntities().Merge(ctx, actor, "h", "a", "b", 1, 1, "reason")
	require.Error(t, e)
}

func TestEntityConcurrentEmptySnapshots(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "first", true)
	entityDoc(t, d, "second", true)
	i1, p1 := entityPlan(t, d, "first", entityMention("a", "Same", "2000-01-02"))
	i2, p2 := entityPlan(t, d, "second", entityMention("a", "Same", "2000-01-02"))
	errs := make(chan error, 2)
	start := make(chan struct{})
	for _, x := range []struct {
		i archive.ResolutionInput
		p archive.ResolutionPlan
	}{{i1, p1}, {i2, p2}} {
		go func(in archive.ResolutionInput, p archive.ResolutionPlan) {
			<-start
			errs <- d.InWriteTx(context.Background(), func(tx *sql.Tx) error { _, e := archiveApplyResolutionIn(context.Background(), tx, in, p); return e })
		}(x.i, x.p)
	}
	close(start)
	successes, conflicts := 0, 0
	for n := 0; n < 2; n++ {
		e := <-errs
		if e == nil {
			successes++
		} else {
			require.ErrorIs(t, e, domain.ErrConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	require.Len(t, entitySnap(t, d).Candidates, 1)
}
func TestEntityRejectsMergeCrossHomeTypeAndCycles(t *testing.T) {
	d := entityDB(t)
	ctx := context.Background()
	var a, b, c, house, foreign string
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		var e error
		a, e = archiveEntityCreateIn(ctx, tx, "h", "person")
		require.NoError(t, e)
		b, e = archiveEntityCreateIn(ctx, tx, "h", "person")
		require.NoError(t, e)
		c, e = archiveEntityCreateIn(ctx, tx, "h", "person")
		require.NoError(t, e)
		house, e = archiveEntityCreateIn(ctx, tx, "h", "property")
		require.NoError(t, e)
		foreign, e = archiveEntityCreateIn(ctx, tx, "other", "person")
		return e
	}))
	merge := func(x, y string) error {
		return d.InWriteTx(ctx, func(tx *sql.Tx) error {
			_, xv, e := archiveEntityVersionIn(ctx, tx, "h", x)
			if e != nil {
				return e
			}
			_, yv, e := archiveEntityVersionIn(ctx, tx, "h", y)
			if e != nil {
				return e
			}
			_, e = archiveMergeEntitiesIn(ctx, tx, "h", x, y, xv, yv, "actor", "why")
			return e
		})
	}
	require.ErrorIs(t, merge(a, foreign), domain.ErrNotFound)
	require.ErrorIs(t, merge(a, house), domain.ErrConflict)
	require.NoError(t, merge(a, b))
	require.NoError(t, merge(b, c))
	require.ErrorIs(t, merge(c, a), domain.ErrConflict)
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		id, e := archiveCanonicalIn(ctx, tx, "h", a)
		require.Equal(t, c, id)
		return e
	}))
}
func TestEntityLegacyRejectsOtherHousehold(t *testing.T) {
	d := entityDB(t)
	e := d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		_, e := archiveLegacyEntityIn(context.Background(), tx, "other", "abu")
		return e
	})
	require.ErrorIs(t, e, domain.ErrNotFound)
}
func TestEntityBindingAndAliasRevisionAndScope(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	entityDoc(t, d, "correction", true)
	entityDoc(t, d, "future", true)
	in, p := entityPlan(t, d, "d", entityMention("a", "王宁", "2000-01-02"))
	id := applyEntity(t, d, in, p)["a"]
	before := entitySnap(t, d)
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		return archiveBindingPutIn(context.Background(), tx, "h", "document_mention", "correction#b", id, "actor", []string{"confirmation"}, 0)
	}))
	require.Greater(t, entitySnap(t, d).Revision, before.Revision)
	m := entityMention("b", "小宁", "2000-01-02")
	m.BindingScope = "document_mention"
	m.BindingKey = "correction#b"
	in, p = entityPlan(t, d, "correction", m)
	require.Equal(t, "link_existing", p.Decisions[0].Action)
	applyEntity(t, d, in, p)
	_, p = entityPlan(t, d, "future", entityMention("c", "小宁", "2000-01-02"))
	require.Equal(t, "needs_resolution", p.Decisions[0].Action, "document correction cannot create a permanent alias rule")
	before = entitySnap(t, d)
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		return archiveAttributePutIn(context.Background(), tx, "h", id, "display_alias", "Other", "", "d", "", []string{"alias"}, true, false)
	}))
	require.Greater(t, entitySnap(t, d).Revision, before.Revision)
}
func TestEntityStrongComparisonLimitsPrivacyAndIsolation(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	in, p := entityPlan(t, d, "d", entityMention("a", "Name", ""))
	id := applyEntity(t, d, in, p)["a"]
	ctx := context.Background()
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		for n := 0; n < maxIdentityFacts+1; n++ {
			if e := archiveProtectedIdentityPutIn(ctx, tx, "h", id, ProtectedIdentityFact{DocumentID: "d", Kind: "passport", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "001234", Evidence: []string{"identity-ref"}}); e != nil {
				return e
			}
		}
		r, e := archiveCompareIdentityIn(ctx, tx, "h", IdentityProbe{Kind: "passport", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "001234"})
		if e != nil {
			return e
		}
		require.False(t, r.Complete)
		require.Empty(t, r.EntityIDs)
		other, e := archiveCompareIdentityIn(ctx, tx, "other", IdentityProbe{Kind: "passport", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "001234"})
		require.True(t, other.Complete)
		require.Empty(t, other.EntityIDs)
		return e
	}))
	s := entitySnap(t, d)
	data, e := json.Marshal(s)
	require.NoError(t, e)
	require.NotContains(t, string(data), "001234")
	data, e = json.Marshal(IdentityProbe{Value: "001234"})
	require.NoError(t, e)
	require.NotContains(t, string(data), "001234")
	var visible bool
	require.NoError(t, d.sql.QueryRow(`SELECT tv_visible FROM archive_documents WHERE id='d'`).Scan(&visible))
	require.True(t, visible, "identity detection does not override TV default")
}
func TestEntityPublicProjectionTracksPrivateClosure(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	in, p := entityPlan(t, d, "d", entityMention("a", "Visible", "2000-01-02"))
	applyEntity(t, d, in, p)
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		out, e := archiveEntityPublicIn(context.Background(), tx, "h")
		require.Len(t, out, 1)
		if e != nil {
			return e
		}
		_, e = tx.Exec(`UPDATE archive_documents SET tv_visible=0 WHERE household_id='h' AND id='d'`)
		if e != nil {
			return e
		}
		out, e = archiveEntityPublicIn(context.Background(), tx, "h")
		require.Empty(t, out)
		return e
	}))
}

func TestEntityBatchAliasNeverPromotedByRepetition(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	entityDoc(t, d, "later", true)
	ms := []archive.ResolutionMention{entityMention("a", "Primary", "2000-01-02"), entityMention("b", "Alias", "2000-01-02"), entityMention("c", "Alias", "2000-01-02")}
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
		in := archive.ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: ms}
		p, e := archive.ResolveEntities(in, s)
		if e != nil {
			return e
		}
		_, e = archiveApplyResolutionIn(context.Background(), tx, in, p, probes)
		return e
	}))
	_, p := entityPlan(t, d, "later", entityMention("x", "Alias", "2000-01-02"))
	require.Equal(t, "needs_resolution", p.Decisions[0].Action)
}
func TestEntityBirthAggregationRetainsConflictsAndCompatiblePrecision(t *testing.T) {
	for _, dates := range [][]archive.IdentityDate{{{Precision: "date", Value: "1988-03-12"}, {Precision: "date", Value: "1991-03-12"}, {Precision: "date", Value: "1988-03-12"}}, {{Precision: "year", Value: "1988"}, {Precision: "date", Value: "1988-03-12"}}, {{Precision: "date", Value: "1988-03-12"}, {Precision: "year", Value: "1988"}}} {
		d := entityDB(t)
		entityDoc(t, d, "d", true)
		in, p := entityPlan(t, d, "d", entityMention("a", "Name", ""))
		id := applyEntity(t, d, in, p)["a"]
		require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
			for _, date := range dates {
				if e := archiveAttributePutIn(context.Background(), tx, "h", id, "birth", date.Value, date.Precision, "d", "", []string{"birth"}, true, false); e != nil {
					return e
				}
			}
			return nil
		}))
		s := entitySnap(t, d)
		require.Equal(t, len(dates) == 3, s.Candidates[0].Birth.Conflict)
		if len(dates) == 2 {
			require.Equal(t, "1988-03-12", s.Candidates[0].Birth.Value)
		}
	}
}

func TestEntityReadWritePermissionsAndRevocation(t *testing.T) {
	d := entityDB(t)
	ctx := context.Background()
	now := time.Now()
	p := &domain.IntegrationPrincipal{ID: "reader", Label: "reader", Enabled: true, PolicyVersion: 1, Policy: domain.IntegrationPolicy{Permissions: []string{"archive.read"}, HouseholdIDs: []string{"h"}}, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	c := &domain.IntegrationCredential{ID: "reader-cred", PrincipalID: p.ID, TokenHash: "synthetic-reader", ExpiresAt: p.ExpiresAt, CreatedAt: now}
	require.NoError(t, d.Integrations().Create(ctx, p, c))
	actor := domain.IntegrationActor{PrincipalID: p.ID, CredentialID: c.ID}
	_, e := d.ArchiveEntities().Candidates(ctx, actor, "h")
	require.NoError(t, e)
	_, e = d.ArchiveEntities().Public(ctx, actor, "h")
	require.NoError(t, e)
	_, e = d.ArchiveEntities().Merge(ctx, actor, "h", "a", "b", 1, 1, "reason")
	require.Error(t, e)
	p.Policy.Permissions = append(p.Policy.Permissions, "archive.organize")
	require.NoError(t, d.Integrations().UpdatePolicy(ctx, p.ID, p.Policy, now))
	var a, b string
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		var e error
		a, e = archiveEntityCreateIn(ctx, tx, "h", "person")
		if e != nil {
			return e
		}
		b, e = archiveEntityCreateIn(ctx, tx, "h", "person")
		return e
	}))
	id, e := d.ArchiveEntities().Merge(ctx, actor, "h", a, b, 1, 1, "reason")
	require.NoError(t, e)
	require.NoError(t, d.ArchiveEntities().UndoMerge(ctx, actor, "h", id, 1, "undo"))
	require.NoError(t, d.Integrations().Revoke(ctx, p.ID, now))
	_, e = d.ArchiveEntities().Candidates(ctx, actor, "h")
	require.Error(t, e)
	_, e = d.ArchiveEntities().Merge(ctx, actor, "h", a, b, 3, 3, "reason")
	require.Error(t, e)
}
func TestEntityPlanDependencyTamperingRejected(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	in, p := entityPlan(t, d, "d", entityMention("a", "Name", ""))
	p.Decisions[0].EntityID = "model-global-id"
	e := d.InWriteTx(context.Background(), func(tx *sql.Tx) error { _, e := archiveApplyResolutionIn(context.Background(), tx, in, p); return e })
	require.ErrorIs(t, e, domain.ErrConflict)
	require.Empty(t, entitySnap(t, d).Candidates)
}
func TestEntityPublicBirthNeverLeaksPrivateSources(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "public", true)
	entityDoc(t, d, "private", false)
	in, p := entityPlan(t, d, "public", entityMention("a", "Public", ""))
	a := applyEntity(t, d, in, p)["a"]
	in, p = entityPlan(t, d, "private", entityMention("b", "Secret", "1991-03-12"))
	b := applyEntity(t, d, in, p)["b"]
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		_, e := archiveMergeEntitiesIn(context.Background(), tx, "h", b, a, entitySnapVersion(t, tx, "h", b), entitySnapVersion(t, tx, "h", a), "actor", "reason")
		if e != nil {
			return e
		}
		out, e := archiveEntityPublicIn(context.Background(), tx, "h")
		require.Len(t, out, 1)
		require.Equal(t, []string{"Public"}, out[0].Names)
		require.Empty(t, out[0].Birth.Value)
		return e
	}))
}

func TestEntityIncompleteSnapshotCannotCreate(t *testing.T) {
	d := entityDB(t)
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		for i := 0; i <= maxEntityCandidates; i++ {
			if _, e := archiveEntityCreateIn(context.Background(), tx, "h", "person"); e != nil {
				return e
			}
		}
		return nil
	}))
	s := entitySnap(t, d)
	require.False(t, s.Complete)
	p, e := archive.ResolveEntities(archive.ResolutionInput{HouseholdID: "h", DocumentID: "d", Mentions: []archive.ResolutionMention{entityMention("a", "Name", "")}}, s)
	require.NoError(t, e)
	require.Equal(t, "needs_resolution", p.Decisions[0].Action)
}

func TestEntityLinkReadsCanonicalWithoutChangingMatchedID(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	in, p := entityPlan(t, d, "d", entityMention("a", "One", ""), entityMention("b", "Two", ""))
	ids := applyEntity(t, d, in, p)
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		_, e := archiveMergeEntitiesIn(context.Background(), tx, "h", ids["a"], ids["b"], entitySnapVersion(t, tx, "h", ids["a"]), entitySnapVersion(t, tx, "h", ids["b"]), "actor", "reason")
		if e != nil {
			return e
		}
		links, e := archiveEntityLinksIn(context.Background(), tx, "h", "d")
		require.Len(t, links, 2)
		for _, l := range links {
			require.Equal(t, ids[l.MentionRef], l.MatchedEntityID)
			require.Equal(t, ids["b"], l.CanonicalEntityID)
		}
		return e
	}))
}

func TestEntityUndoQuarantinesMentionIdentityEvidence(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "base", true)
	entityDoc(t, d, "later", true)
	in, p := entityPlan(t, d, "base", entityMention("a", "One", ""), entityMention("b", "Two", ""))
	ids := applyEntity(t, d, in, p)
	ctx := context.Background()
	var merge string
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		var e error
		merge, e = archiveMergeEntitiesIn(ctx, tx, "h", ids["b"], ids["a"], entitySnapVersion(t, tx, "h", ids["b"]), entitySnapVersion(t, tx, "h", ids["a"]), "actor", "same")
		if e != nil {
			return e
		}
		return archiveBindingPutIn(ctx, tx, "h", "document_mention", "later#uncertain", ids["a"], "actor", []string{"confirmed"}, 0)
	}))
	uncertain := entityMention("uncertain", "Alias", "1991-03-12")
	uncertain.BindingScope = "document_mention"
	uncertain.BindingKey = "later#uncertain"
	other := entityMention("other", "Independent", "2001-02-03")
	in, p = entityPlan(t, d, "later", uncertain, other)
	applyEntity(t, d, in, p)
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		if e := archiveProtectedIdentityPutIn(ctx, tx, "h", ids["a"], ProtectedIdentityFact{DocumentID: "later", Kind: "national_id", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "001234", Evidence: []string{"id"}}); e != nil {
			return e
		}
		return archiveUndoEntityMergeIn(ctx, tx, "h", merge, 1, "actor", "undo")
	}))
	s := entitySnap(t, d)
	require.Len(t, s.Bindings, 1)
	require.True(t, s.Bindings[0].Conflict, "undo must invalidate a scoped binding made under merged identity")
	for _, c := range s.Candidates {
		if c.ID == ids["a"] {
			require.Empty(t, c.Birth.Value)
			for _, n := range c.Names {
				require.NotEqual(t, "Alias", n.Value)
			}
		}
	}
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		r, e := archiveCompareIdentityIn(ctx, tx, "h", IdentityProbe{Kind: "national_id", Issuer: "SG", RuleVersion: IdentityExactVersion, Value: "001234"})
		require.Empty(t, r.EntityIDs)
		if e != nil {
			return e
		}
		public, e := archiveEntityPublicIn(ctx, tx, "h")
		independent := false
		for _, v := range public {
			require.NotContains(t, v.Aliases, "Alias")
			if v.ID == ids["a"] {
				require.Empty(t, v.Birth.Value)
			}
			if len(v.Names) > 0 && v.Names[0] == "Independent" {
				independent = true
			}
		}
		require.True(t, independent, "other mention in same document remains visible")
		return e
	}))
}
