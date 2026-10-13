package store

import (
	"context"
	"database/sql"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEntityQualityPropertyScopedAliasDoesNotBecomeGlobal(t *testing.T) {
	d := entityDB(t)
	for _, doc := range []string{"base", "correction", "future"} {
		entityDoc(t, d, doc, true)
	}
	property := func(ref, name string) archive.ResolutionMention {
		return archive.ResolutionMention{Ref: ref, Name: name, Kind: "property", Role: "property", Subject: true, FullAddress: true, Evidence: []string{"address"}}
	}
	in, p := entityPlan(t, d, "base", property("a", "Road 1 Unit 1"))
	id := applyEntity(t, d, in, p)["a"]
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		return archiveBindingPutIn(context.Background(), tx, "h", "document_mention", "correction#b", id, "actor", []string{"confirmation"}, 0)
	}))
	m := property("b", "Road 1 Unit 2")
	m.BindingScope = "document_mention"
	m.BindingKey = "correction#b"
	in, p = entityPlan(t, d, "correction", m)
	require.Equal(t, "link_existing", p.Decisions[0].Action)
	applyEntity(t, d, in, p)
	_, p = entityPlan(t, d, "future", property("c", "Road 1 Unit 2"))
	t.Logf("future property action=%s reason=%s", p.Decisions[0].Action, p.Decisions[0].Reason)
	require.Equal(t, "needs_resolution", p.Decisions[0].Action, "document-only confirmation must not authorize future exact-address identity")
}
func TestEntityQualityUndoInvalidatesBindingAddedToOriginalLink(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "base", true)
	in, p := entityPlan(t, d, "base", entityMention("a", "One", ""), entityMention("b", "Two", ""))
	ids := applyEntity(t, d, in, p)
	ctx := context.Background()
	var merge string
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		var e error
		merge, e = archiveMergeEntitiesIn(ctx, tx, "h", ids["b"], ids["a"], entitySnapVersion(t, tx, "h", ids["b"]), entitySnapVersion(t, tx, "h", ids["a"]), "actor", "same")
		return e
	}))
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		return archiveBindingPutIn(ctx, tx, "h", "document_mention", "base#b", ids["a"], "actor", []string{"confirmed while merged"}, 0)
	}))
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error { return archiveUndoEntityMergeIn(ctx, tx, "h", merge, 1, "actor", "undo") }))
	s := entitySnap(t, d)
	require.Len(t, s.Bindings, 1)
	m := entityMention("b", "Two", "")
	m.BindingScope = "document_mention"
	m.BindingKey = "base#b"
	p, e := archive.ResolveEntities(archive.ResolutionInput{HouseholdID: "h", DocumentID: "base", Mentions: []archive.ResolutionMention{m}}, s)
	require.NoError(t, e)
	t.Logf("after undo binding conflict=%v action=%s reason=%s targets-original=%v", s.Bindings[0].Conflict, p.Decisions[0].Action, p.Decisions[0].Reason, p.Decisions[0].EntityID == ids["b"])
	require.Equal(t, "needs_resolution", p.Decisions[0].Action, "confirmation made against merged object cannot silently reattach original source to merge target after undo")
}

func TestEntityQualityPropertyPrimaryAndCorrectionControls(t *testing.T) {
	d := entityDB(t)
	for _, doc := range []string{"base", "correction", "future"} {
		entityDoc(t, d, doc, true)
	}
	property := func(ref, address string) archive.ResolutionMention {
		return archive.ResolutionMention{Ref: ref, Name: address, Kind: "property", Role: "property", Subject: true, FullAddress: true, Evidence: []string{"address"}}
	}
	in, p := entityPlan(t, d, "base", property("a", "Road 1 Unit 1"))
	id := applyEntity(t, d, in, p)["a"]
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		return archiveBindingPutIn(context.Background(), tx, "h", "document_mention", "correction#b", id, "actor", []string{"confirmation"}, 0)
	}))
	m := property("b", "Road 1 Unit 2")
	m.BindingScope = "document_mention"
	m.BindingKey = "correction#b"
	in, p = entityPlan(t, d, "correction", m)
	require.Equal(t, id, p.Decisions[0].EntityID)
	applyEntity(t, d, in, p)
	_, p = entityPlan(t, d, "future", property("c", "Road 1 Unit 1"))
	require.Equal(t, "link_existing", p.Decisions[0].Action)
	require.Equal(t, id, p.Decisions[0].EntityID)
	_, p = entityPlan(t, d, "correction", m)
	require.Equal(t, "link_existing", p.Decisions[0].Action)
	require.Equal(t, id, p.Decisions[0].EntityID)
}
func TestEntityQualityBindingRevisionContextsPreserveOriginalAndFenceReconfirmation(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "base", true)
	in, p := entityPlan(t, d, "base", entityMention("a", "One", ""), entityMention("b", "Two", ""))
	ids := applyEntity(t, d, in, p)
	ctx := context.Background()
	var merge string
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		if e := archiveBindingPutIn(ctx, tx, "h", "document_mention", "base#a", ids["a"], "actor", []string{"original-a"}, 0); e != nil {
			return e
		}
		if e := archiveBindingPutIn(ctx, tx, "h", "document_mention", "base#b", ids["b"], "actor", []string{"original-b"}, 0); e != nil {
			return e
		}
		var e error
		merge, e = archiveMergeEntitiesIn(ctx, tx, "h", ids["b"], ids["a"], entitySnapVersion(t, tx, "h", ids["b"]), entitySnapVersion(t, tx, "h", ids["a"]), "actor", "merge")
		return e
	}))
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		return archiveBindingPutIn(ctx, tx, "h", "document_mention", "base#b", ids["a"], "actor", []string{"merged confirmation"}, 1)
	}))
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error { return archiveUndoEntityMergeIn(ctx, tx, "h", merge, 1, "actor", "undo") }))
	for _, b := range entitySnap(t, d).Bindings {
		if b.ScopeKey == "base#a" {
			require.False(t, b.Conflict)
			require.EqualValues(t, 1, b.Version)
		} else {
			require.True(t, b.Conflict)
			require.EqualValues(t, 3, b.Version)
		}
	}
	e := d.InWriteTx(ctx, func(tx *sql.Tx) error {
		return archiveBindingPutIn(ctx, tx, "h", "document_mention", "base#b", ids["b"], "actor", []string{"stale"}, 2)
	})
	require.ErrorIs(t, e, domain.ErrConflict)
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		return archiveBindingPutIn(ctx, tx, "h", "document_mention", "base#b", ids["b"], "actor", []string{"new confirmation"}, 3)
	}))
	for _, b := range entitySnap(t, d).Bindings {
		require.False(t, b.Conflict)
		if b.ScopeKey == "base#b" {
			require.Equal(t, ids["b"], b.EntityID)
			require.EqualValues(t, 4, b.Version)
		}
	}
}
func TestEntityQualityBindingNestedMergeContexts(t *testing.T) {
	for _, mode := range []string{"keep", "independent", "remaining"} {
		t.Run(mode, func(t *testing.T) {
			d := entityDB(t)
			entityDoc(t, d, "base", true)
			in, p := entityPlan(t, d, "base", entityMention("a", "One", ""), entityMention("b", "Two", ""), entityMention("c", "Three", ""), entityMention("d", "Four", ""))
			ids := applyEntity(t, d, in, p)
			ctx := context.Background()
			var first, second string
			require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
				var e error
				first, e = archiveMergeEntitiesIn(ctx, tx, "h", ids["b"], ids["a"], entitySnapVersion(t, tx, "h", ids["b"]), entitySnapVersion(t, tx, "h", ids["a"]), "actor", "first")
				if e != nil {
					return e
				}
				second, e = archiveMergeEntitiesIn(ctx, tx, "h", ids["a"], ids["c"], entitySnapVersion(t, tx, "h", ids["a"]), entitySnapVersion(t, tx, "h", ids["c"]), "actor", "second")
				if e != nil {
					return e
				}
				if e = archiveBindingPutIn(ctx, tx, "h", "document_mention", "base#b", ids["c"], "actor", []string{"nested"}, 0); e != nil {
					return e
				}
				return archiveBindingPutIn(ctx, tx, "h", "document_mention", "base#d", ids["d"], "actor", []string{"independent"}, 0)
			}))
			require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error { return archiveUndoEntityMergeIn(ctx, tx, "h", first, 1, "actor", "undo first") }))
			if mode != "keep" {
				target := ids["d"]
				if mode == "remaining" {
					target = ids["c"]
				}
				require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
					return archiveBindingPutIn(ctx, tx, "h", "document_mention", "base#b", target, "actor", []string{"new confirmation"}, 2)
				}))
			}
			require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
				return archiveUndoEntityMergeIn(ctx, tx, "h", second, 1, "actor", "undo second")
			}))
			for _, b := range entitySnap(t, d).Bindings {
				if b.ScopeKey == "base#d" {
					require.False(t, b.Conflict)
					require.EqualValues(t, 1, b.Version)
				} else {
					require.Equal(t, mode != "independent", b.Conflict)
					expected := 3
					if mode == "remaining" {
						expected = 4
					}
					require.EqualValues(t, expected, b.Version)
				}
			}
			var contexts int
			require.NoError(t, d.sql.QueryRow(`SELECT count(*) FROM archive_entity_binding_merge_context`).Scan(&contexts))
			expected := 2
			if mode == "remaining" {
				expected = 3
			}
			require.Equal(t, expected, contexts, "historical confirmation contexts remain immutable")
		})
	}
}
