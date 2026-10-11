package store_test

import (
	"context"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestArchiveQualityRegressionValueDoesNotConfirmUnresolvedOwner(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "named", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, namedPublicationBundle(t, "Alice"))
	_, e := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, e)
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
	require.NoError(t, e)
	require.Len(t, items, 1)
	_, e = r.ReviseArchive(ctx, a, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: items[0].ID, Value: "71", Reason: "correct value only"})
	require.NoError(t, e)
	_, e = d.SQL().Exec(`UPDATE archive_entity_links SET status='needs_resolution',version=version+1 WHERE document_id=?`, rec.DocumentID)
	require.NoError(t, e)
	for _, public := range []bool{false, true} {
		items, e = r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, public)
		require.NoError(t, e)
		require.Empty(t, items, "value-only assertion cannot authorize unresolved ownership")
	}
}
func TestArchiveQualityRegressionCorrectedTransactionIdentityDoesNotConflate(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	var docs []string
	for i, c := range []struct {
		id, value string
		kind      domain.ArchiveInputKind
	}{{"0012", "70", domain.ArchiveInputFile}, {"0013", "80", domain.ArchiveInputFile}, {"0012", "70", domain.ArchiveInputText}} {
		rec, f, x := publicationFixture(t, d, r, c.id+c.value, c.kind)
		if i > 0 {
			docs = append(docs, rec.DocumentID)
		}
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", c.id}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", c.value}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	for i, doc := range docs {
		items, e := r.CurrentArchiveObservations(ctx, a, "home-1", doc, false)
		require.NoError(t, e)
		value := ""
		for _, it := range items {
			var b struct{ Field archive.PublicationField }
			require.NoError(t, json.Unmarshal(it.Body, &b))
			if b.Field.Name == "amount" {
				value = b.Field.Raw
			}
		}
		require.Equal(t, []string{"80", "70"}[i], value, "distinct currently proven transaction IDs must not conflict")
	}
}
func TestArchiveQualityRegressionValueAfterActualMergeUndo(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	publish := func(key, name string) (*domain.ArchiveIntakeReceipt, []store.ArchiveCurrentObservation) {
		rec, f, x := publicationFixture(t, d, r, key, domain.ArchiveInputText)
		b := literalBundle(t, name, []publicationLiteral{{"birth_date", "2000-01-02"}, {"amount", "70"}}, "SGD")
		result := b.Understanding.Chunks[0].Result
		result.Fields[0].Normalizations = []archive.Normalization{{Kind: "date", Value: "2000-01-02"}}
		b = revalidatePublication(t, b, result)
		f = saveBundle(t, r, f, b)
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		items, e := r.CurrentArchiveObservations(ctx, a, "home-1", rec.DocumentID, false)
		require.NoError(t, e)
		require.NotEmpty(t, items)
		return rec, items
	}
	first, _ := publish("first", "Alice")
	second, _ := publish("second", "Bob")
	var source, target string
	var sv, tv int64
	require.NoError(t, d.SQL().QueryRow(`SELECT matched_entity_id FROM archive_entity_links WHERE document_id=?`, first.DocumentID).Scan(&source))
	require.NoError(t, d.SQL().QueryRow(`SELECT matched_entity_id FROM archive_entity_links WHERE document_id=?`, second.DocumentID).Scan(&target))
	require.NoError(t, d.SQL().QueryRow(`SELECT version FROM archive_entities WHERE id=?`, source).Scan(&sv))
	require.NoError(t, d.SQL().QueryRow(`SELECT version FROM archive_entities WHERE id=?`, target).Scan(&tv))
	merge, e := d.ArchiveEntities().Merge(ctx, a, "home-1", source, target, sv, tv, "same person")
	require.NoError(t, e)
	later, items := publish("later", "Alice")
	obs := ""
	for _, item := range items {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(item.Body, &b))
		if b.Field.Name == "amount" {
			obs = item.ID
		}
	}
	require.NotEmpty(t, obs)
	_, e = r.ReviseArchive(ctx, a, "home-1", later.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: obs, Value: "71", Reason: "correct value"})
	require.NoError(t, e)
	require.NoError(t, d.ArchiveEntities().UndoMerge(ctx, a, "home-1", merge, 1, "different people"))
	var status string
	require.NoError(t, d.SQL().QueryRow(`SELECT status FROM archive_entity_links WHERE document_id=?`, later.DocumentID).Scan(&status))
	require.Equal(t, "needs_resolution", status)
	for _, public := range []bool{false, true} {
		items, e = r.CurrentArchiveObservations(ctx, a, "home-1", later.DocumentID, public)
		require.NoError(t, e)
		require.Empty(t, items, "merge undo must quarantine owner even when amount was manually changed")
	}
}

func TestArchiveTransactionProofChangeWithDependenciesWaits(t *testing.T) {
	for _, dependency := range []string{"shared", "private_peer", "manual", "missing_proof"} {
		t.Run(dependency, func(t *testing.T) {
			d := testutil.NewDB(t)
			r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			ctx := context.Background()
			actor := archiveActor(t, d)
			bundle := func(id string) store.ArchivePublicationBundle {
				return literalBundle(t, "", []publicationLiteral{{"transaction_id", id}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "70"}}, "SGD")
			}
			rec, f, x := publicationFixture(t, d, r, "first", domain.ArchiveInputFile)
			f = saveBundle(t, r, f, bundle("0012"))
			_, e := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			if dependency == "shared" || dependency == "private_peer" {
				peer, pf, px := publicationFixture(t, d, r, "peer", domain.ArchiveInputText)
				pf = saveBundle(t, r, pf, bundle("0012"))
				_, e = r.PublishExtraction(ctx, pf, px.ID)
				require.NoError(t, e)
				if dependency == "private_peer" {
					_, e = r.ReviseArchive(ctx, actor, "home-1", peer.DocumentID, 2, store.ArchiveRevision{Kind: "private", Reason: "private proof"})
					require.NoError(t, e)
				}
			}
			if dependency == "manual" {
				var obs string
				require.NoError(t, d.SQL().QueryRow(`SELECT id FROM archive_observations WHERE slot_key='supported-transaction-amount/1'`).Scan(&obs))
				_, e = r.ReviseArchive(ctx, actor, "home-1", rec.DocumentID, 2, store.ArchiveRevision{Kind: "value", ObservationID: obs, Value: "71", Reason: "manual amount"})
				require.NoError(t, e)
			}
			var previous string
			require.NoError(t, d.SQL().QueryRow(`SELECT active_publication_id FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&previous))
			_, f, x = publicationFixture(t, d, r, "corrected", domain.ArchiveInputFile)
			b := bundle("0013")
			if dependency == "missing_proof" {
				b = literalBundle(t, "", []publicationLiteral{{"amount", "70"}}, "SGD")
			}
			f = saveBundle(t, r, f, b)
			done, e := r.PublishExtraction(ctx, f, x.ID)
			require.NoError(t, e)
			require.Equal(t, domain.ArchiveTaskWaitingInput, done.Status)
			var current string
			require.NoError(t, d.SQL().QueryRow(`SELECT active_publication_id FROM archive_documents WHERE id=?`, rec.DocumentID).Scan(&current))
			require.Equal(t, previous, current)
			var count int
			require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_transaction_events WHERE identifier='0013'`).Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestArchiveSharedValueChecksActualSourceOwner(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	actor := archiveActor(t, d)
	docs := []string{}
	for _, name := range []string{"Alice", "Bob"} {
		rec, f, x := publicationFixture(t, d, r, name, domain.ArchiveInputText)
		docs = append(docs, rec.DocumentID)
		f = saveBundle(t, r, f, literalBundle(t, name, []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "70"}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	var obs string
	require.NoError(t, d.SQL().QueryRow(`SELECT id FROM archive_observations WHERE slot_key='supported-transaction-amount/1'`).Scan(&obs))
	_, e := r.ReviseArchive(ctx, actor, "home-1", docs[0], 2, store.ArchiveRevision{Kind: "value", ObservationID: obs, Value: "71", Reason: "value only"})
	require.NoError(t, e)
	items, e := r.CurrentArchiveObservations(ctx, actor, "home-1", docs[1], false)
	require.NoError(t, e)
	found := false
	for _, item := range items {
		if item.ID == obs {
			found = true
			require.Equal(t, docs[0], item.SourceDocumentID)
		}
	}
	require.True(t, found, "must validate against assertion source Alice, not queried Bob document")
	_, e = d.SQL().Exec(`UPDATE archive_entity_links SET status='needs_resolution',version=version+1 WHERE document_id=?`, docs[0])
	require.NoError(t, e)
	for _, doc := range docs {
		items, e = r.CurrentArchiveObservations(ctx, actor, "home-1", doc, false)
		require.NoError(t, e)
		for _, item := range items {
			require.NotEqual(t, obs, item.ID)
		}
	}
}

func TestArchiveWithdrawnTransactionLookupCannotAuthorizeNewSharing(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	actor := archiveActor(t, d)
	publish := func(key string) (string, string) {
		rec, f, x := publicationFixture(t, d, r, key, domain.ArchiveInputText)
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", "0012"}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", "70"}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
		items, e := r.CurrentArchiveObservations(ctx, actor, "home-1", rec.DocumentID, false)
		require.NoError(t, e)
		for _, item := range items {
			var b struct{ Field archive.PublicationField }
			require.NoError(t, json.Unmarshal(item.Body, &b))
			if b.Field.Name == "amount" {
				return rec.DocumentID, item.ID
			}
		}
		t.Fatal("amount missing")
		return "", ""
	}
	doc, old := publish("first")
	_, e := r.ReviseArchive(ctx, actor, "home-1", doc, 2, store.ArchiveRevision{Kind: "withdraw", Reason: "invalid proof"})
	require.NoError(t, e)
	_, next := publish("second")
	require.NotEqual(t, old, next)
}

func TestArchiveCorrectedTransactionCanJoinExistingNewIdentity(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	ctx := context.Background()
	a := archiveActor(t, d)
	source := ""
	for i, c := range []struct {
		id, value string
		kind      domain.ArchiveInputKind
	}{{"0012", "70", domain.ArchiveInputFile}, {"0013", "80", domain.ArchiveInputText}, {"0013", "80", domain.ArchiveInputFile}} {
		rec, f, x := publicationFixture(t, d, r, c.id+c.value+string(rune('a'+i)), c.kind)
		if i == 0 {
			source = rec.DocumentID
		}
		f = saveBundle(t, r, f, literalBundle(t, "", []publicationLiteral{{"transaction_id", c.id}, {"transaction_type", "payment"}, {"issuer", "Bank A"}, {"amount", c.value}}, "SGD"))
		_, e := r.PublishExtraction(ctx, f, x.ID)
		require.NoError(t, e)
	}
	items, e := r.CurrentArchiveObservations(ctx, a, "home-1", source, false)
	require.NoError(t, e)
	amounts := []string{}
	for _, item := range items {
		var b struct{ Field archive.PublicationField }
		require.NoError(t, json.Unmarshal(item.Body, &b))
		if b.Field.Name == "amount" {
			amounts = append(amounts, b.Field.Raw)
		}
	}
	require.Equal(t, []string{"80"}, amounts)
}
