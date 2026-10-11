package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
)

func archiveTransactionEventIn(ctx context.Context, tx *sql.Tx, h, event, publication string, fields []archive.PublicationField) (string, error) {
	proof := archive.TransactionIdentity(fields)
	if proof == nil {
		return event, nil
	}
	var oldCanonical, oldProof string
	e := tx.QueryRowContext(ctx, `SELECT canonical_event_id,proof_json FROM archive_event_equivalences WHERE household_id=? AND event_id=?`, h, event).Scan(&oldCanonical, &oldProof)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	if e == nil && !archiveTransactionProofMatches(oldProof, proof) {
		review, err := archiveTransactionChangeNeedsReviewIn(ctx, tx, h, event, fields)
		if err != nil {
			return "", err
		}
		if review {
			return "", domain.ErrConflict
		}
		// The old equivalence had this source alone. Retire its lookup, not its
		// immutable observation history, before installing the corrected proof.
		if _, e = tx.ExecContext(ctx, `DELETE FROM archive_transaction_events WHERE household_id=? AND event_id=?`, h, oldCanonical); e != nil {
			return "", e
		}
	}
	var canonical string
	e = tx.QueryRowContext(ctx, `SELECT t.event_id FROM archive_transaction_events t WHERE t.household_id=? AND t.transaction_type=? AND t.issuer=? AND t.identifier=? AND EXISTS(SELECT 1 FROM archive_event_equivalences q JOIN archive_subject_events ev ON ev.household_id=q.household_id AND ev.id=q.event_id JOIN archive_documents d ON d.household_id=ev.household_id AND d.id=ev.document_id WHERE q.household_id=t.household_id AND q.canonical_event_id=t.event_id AND json_extract(q.proof_json,'$.Type')=t.transaction_type AND json_extract(q.proof_json,'$.Issuer')=t.issuer AND json_extract(q.proof_json,'$.Identifier')=t.identifier AND d.withdrawn=0 AND EXISTS(SELECT 1 FROM archive_publication_observations p JOIN archive_observations o ON o.household_id=p.household_id AND o.id=p.observation_id WHERE p.household_id=d.household_id AND (p.publication_id=d.active_publication_id OR p.publication_id=?) AND o.event_id=q.canonical_event_id AND o.slot_key='supported-transaction-amount/1'))`, h, proof.Type, proof.Issuer, proof.Identifier, publication).Scan(&canonical)
	if errors.Is(e, sql.ErrNoRows) {
		canonical = event
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_transaction_events VALUES(?,?,?,?,?) ON CONFLICT(household_id,transaction_type,issuer,identifier) DO UPDATE SET event_id=excluded.event_id`, h, proof.Type, proof.Issuer, proof.Identifier, event)
	}
	if e != nil {
		return "", e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO archive_event_equivalences VALUES(?,?,?,?) ON CONFLICT(household_id,event_id) DO UPDATE SET canonical_event_id=excluded.canonical_event_id,proof_json=excluded.proof_json`, h, event, canonical, entityJSON(proof))
	return canonical, e
}
func archiveAmountSignature(f archive.PublicationField) string {
	value := ""
	currency := ""
	for _, n := range f.Normalizations {
		if n.Kind == "decimal" {
			value = n.Value
		}
	}
	for _, d := range f.Dimensions {
		if d.Kind == "currency" {
			currency = d.Raw
		}
	}
	if value == "" || currency == "" {
		return ""
	}
	return entityJSON([]string{value, currency})
}

// A physical event may change its transaction proof only while its old amount
// is exclusive to this document. Splitting a shared fact or migrating human
// assertions across identities requires explicit review instead of auto-merge.
func archiveTransactionChangeNeedsReviewIn(ctx context.Context, tx *sql.Tx, h, event string, fields []archive.PublicationField) (bool, error) {
	var canonical, previous, doc string
	e := tx.QueryRowContext(ctx, `SELECT q.canonical_event_id,q.proof_json,ev.document_id FROM archive_event_equivalences q JOIN archive_subject_events ev ON ev.household_id=q.household_id AND ev.id=q.event_id WHERE q.household_id=? AND q.event_id=?`, h, event).Scan(&canonical, &previous, &doc)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	proof := archive.TransactionIdentity(fields)
	if proof == nil {
		// Uncertain amount coverage carries the existing current proof without
		// asserting a replacement. A newly trusted amount without identity must
		// not be installed beside the old shared amount.
		for _, field := range fields {
			if field.Name == "amount" && !field.RequiresReview {
				return true, nil
			}
		}
		return false, nil
	}
	if archiveTransactionProofMatches(previous, proof) {
		return false, nil
	}
	if canonical != event {
		return true, nil
	}
	var shared bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM archive_documents d JOIN archive_publication_observations p ON p.household_id=d.household_id AND p.publication_id=d.active_publication_id JOIN archive_observations o ON o.household_id=p.household_id AND o.id=p.observation_id WHERE d.household_id=? AND d.id<>? AND d.withdrawn=0 AND o.event_id=? AND o.slot_key='supported-transaction-amount/1') OR EXISTS(SELECT 1 FROM archive_manual_revisions m JOIN archive_observations o ON o.household_id=m.household_id AND o.id=json_extract(m.body_json,'$.ObservationID') WHERE m.household_id=? AND o.event_id=? AND m.kind IN ('value','reassign'))`, h, doc, canonical, h, canonical).Scan(&shared)
	return shared, e
}

func archiveTransactionProofMatches(raw string, p *archive.TransactionProof) bool {
	var old archive.TransactionProof
	return p != nil && json.Unmarshal([]byte(raw), &old) == nil && old.Type == p.Type && old.Issuer == p.Issuer && old.Identifier == p.Identifier
}
