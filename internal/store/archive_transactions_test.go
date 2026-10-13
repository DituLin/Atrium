package store

import (
	"context"
	"database/sql"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestArchiveTransactionIdentityRequiresPublishedProof(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "a", true)
	entityDoc(t, d, "b", true)
	fields := []archive.PublicationField{{Name: "transaction_id", Raw: "0012", Evidence: []archive.CanonicalEvidence{{Quote: "0012"}}}, {Name: "transaction_type", Raw: "payment", Evidence: []archive.CanonicalEvidence{{Quote: "payment"}}}, {Name: "issuer", Raw: "Bank A", Evidence: []archive.CanonicalEvidence{{Quote: "Bank A"}}}}
	require.NoError(t, d.InWriteTx(context.Background(), func(tx *sql.Tx) error {
		for _, id := range []string{"a", "b"} {
			_, e := tx.Exec(`INSERT INTO archive_subject_events VALUES('h',?,?,?,'physical-parent/1','[]','payment')`, id, id, id)
			if e != nil {
				return e
			}
		}
		first, e := archiveTransactionEventIn(context.Background(), tx, "h", "a", "", fields)
		require.NoError(t, e)
		second, e := archiveTransactionEventIn(context.Background(), tx, "h", "b", "", fields)
		require.NoError(t, e)
		require.NotEqual(t, first, second, "unpublished lookup rows are not current source proof")
		third, e := archiveTransactionEventIn(context.Background(), tx, "h", "b", "", fields[:2])
		require.NoError(t, e)
		require.Equal(t, "b", third)
		return nil
	}))
}
func TestArchiveNativeSlotCorrectionReusesObservationIdentity(t *testing.T) {
	d := entityDB(t)
	entityDoc(t, d, "d", true)
	ctx := context.Background()
	a := archive.PhysicalAnchor{Locator: archive.Locator{Kind: "page", Page: &archive.PageLocation{Page: 1, Rect: archive.Rect{.1, .2, .5, .04}}}, Component: "body", Start: 0, End: 2, Geometry: &archive.PageGeometry{Page: 1, Width: 600, Height: 800}}
	require.NoError(t, d.InWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO archive_tasks(id,household_id,kind,request_key,input_json,status,stage,version,state_version,state_json,result_json,clarification_json,available_at,created_at,updated_at) VALUES('task','h','document.extract','key','{}','queued','intake',1,1,'{}','{}','{}','now','now','now'); INSERT INTO archive_extractions(household_id,id,task_id,document_id,fingerprint,document_revision,resolution_revision) VALUES('h','x','task','d','fp',1,0); INSERT INTO archive_subject_events VALUES('h','event','d','anchor','physical-parent/1','[]','report');INSERT INTO archive_observations VALUES('h','obs','event','original-slot')`)
		if e != nil {
			return e
		}
		_, e = tx.Exec(`INSERT INTO archive_observation_revisions VALUES('h','obs',1,'x',?,'source')`, entityJSON(struct{ Field archive.PublicationField }{archive.PublicationField{Anchors: []archive.PhysicalAnchor{a}, Raw: "70"}}))
		if e != nil {
			return e
		}
		a.End = 3
		fields := []archive.PublicationField{{SlotKey: "new-slot", Raw: "100", Anchors: []archive.PhysicalAnchor{a}}}
		if e = archiveReconcileObservationSlotsIn(ctx, tx, "h", "event", fields); e != nil {
			return e
		}
		require.Equal(t, "original-slot", fields[0].SlotKey)
		require.False(t, fields[0].RequiresReview)
		split := append(fields, fields[0])
		if e = archiveReconcileObservationSlotsIn(ctx, tx, "h", "event", split); e != nil {
			return e
		}
		require.True(t, split[0].RequiresReview)
		require.True(t, split[1].RequiresReview)
		return nil
	}))
}

func TestArchiveTransactionReplacementRejectsPendingField(t *testing.T) {
	fields := []archive.PublicationField{{Name: "amount", Raw: "80", Normalizations: []archive.Normalization{{Kind: "decimal", Value: "80"}}, Dimensions: []archive.PublicationDimension{{Kind: "currency", Raw: "SGD"}}}}
	require.True(t, archiveTransactionReplacementReady(fields, nil))
	fields[0].RequiresReview = true
	require.False(t, archiveTransactionReplacementReady(fields, nil), "unconfirmed source cannot authorize destructive canonical migration")
}
