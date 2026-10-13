package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/domain"
)

// ProjectLegacyArchive rereads the current chain under the same writer lock as
// the projection. Delivery order cannot reactivate an older revision.
func (d *DB) ProjectLegacyArchive(ctx context.Context, record string) error {
	return d.InWriteTx(ctx, func(tx *sql.Tx) error {
		var h string
		if e := tx.QueryRowContext(ctx, `SELECT household_id FROM local_household WHERE singleton=1`).Scan(&h); e != nil {
			return e
		}
		root := record
		seen := map[string]bool{}
		for {
			if seen[root] || len(seen) >= 1024 {
				return domain.ErrConflict
			}
			seen[root] = true
			var parent sql.NullString
			e := tx.QueryRowContext(ctx, `SELECT supersedes_id FROM family_records WHERE id=?`, root).Scan(&parent)
			if errors.Is(e, sql.ErrNoRows) {
				return domain.ErrNotFound
			}
			if e != nil {
				return e
			}
			if !parent.Valid {
				break
			}
			root = parent.String
		}
		current := root
		chain := []string{root}
		for {
			var next string
			e := tx.QueryRowContext(ctx, `SELECT id FROM family_records WHERE supersedes_id=?`, current).Scan(&next)
			if errors.Is(e, sql.ErrNoRows) {
				break
			}
			if e != nil {
				return e
			}
			if len(chain) >= 1024 {
				return domain.ErrConflict
			}
			chain = append(chain, next)
			current = next
		}
		var body string
		if e := tx.QueryRowContext(ctx, `SELECT payload_json FROM family_records WHERE id=?`, current).Scan(&body); e != nil {
			return e
		}
		var in domain.FamilyRecordInput
		if e := json.Unmarshal([]byte(body), &in); e != nil {
			return e
		}
		entity, e := archiveLegacyEntityIn(ctx, tx, h, in.MemberID)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_legacy_projections VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(household_id,root_record_id) DO UPDATE SET current_record_id=excluded.current_record_id,entity_id=excluded.entity_id,body_json=excluded.body_json,active=excluded.active,tv_visible=excluded.tv_visible`, h, root, domain.NewID(), current, entity, body, !in.Retracted, in.DisplayOnTV)
		if e != nil {
			return e
		}
		for _, id := range chain {
			_, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO archive_legacy_record_events VALUES(?,?,?)`, h, id, root)
			if e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, `UPDATE archive_legacy_outbox SET status='completed' WHERE record_id=?`, record)
		return e
	})
}

// ProjectPendingLegacyArchives projects a bounded batch of pending legacy family records.
func (d *DB) ProjectPendingLegacyArchives(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, domain.ErrConflict
	}
	rows, e := d.sql.QueryContext(ctx, `SELECT record_id FROM archive_legacy_outbox WHERE status='pending' ORDER BY record_id LIMIT ?`, limit)
	if e != nil {
		return 0, e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			_ = rows.Close() // Read-only query cursor cleanup.
			return 0, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	_ = rows.Close() // Read-only query cursor cleanup.
	if e != nil {
		return 0, e
	}
	for i, id := range ids {
		if e = d.ProjectLegacyArchive(ctx, id); e != nil {
			return i, e
		}
	}
	return len(ids), nil
}
