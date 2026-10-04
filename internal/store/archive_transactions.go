package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DituLin/Atrium/internal/archive"
)

func archiveTransactionEventIn(ctx context.Context, tx *sql.Tx, h, event string, fields []archive.PublicationField) (string, error) {
	proof := archive.TransactionIdentity(fields)
	if proof == nil {
		return event, nil
	}
	var canonical string
	e := tx.QueryRowContext(ctx, `SELECT event_id FROM archive_transaction_events WHERE household_id=? AND transaction_type=? AND issuer=? AND identifier=?`, h, proof.Type, proof.Issuer, proof.Identifier).Scan(&canonical)
	if errors.Is(e, sql.ErrNoRows) {
		canonical = event
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_transaction_events VALUES(?,?,?,?,?)`, h, proof.Type, proof.Issuer, proof.Identifier, event)
	}
	if e != nil {
		return "", e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO archive_event_equivalences VALUES(?,?,?,?) ON CONFLICT(household_id,event_id) DO NOTHING`, h, event, canonical, entityJSON(proof))
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
