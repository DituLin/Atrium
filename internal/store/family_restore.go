package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/oklog/ulid/v2"
)

// RestoreFamilyRecord is for a newly created recovery database. It preserves
// archive IDs and timestamps, and never replaces existing rows or grants access.
func (d *DB) RestoreFamilyRecord(ctx context.Context, r domain.FamilyRecord, archive string, verifiedAt time.Time) error {
	if err := r.FamilyRecordInput.Validate(); err != nil {
		return err
	}
	_, idErr := ulid.ParseStrict(r.ID)
	if idErr != nil || r.CreatedAt.IsZero() || r.SupersededBy != "" {
		return domain.Errorf(domain.CodeInvalidRequest, "invalid archived record identity")
	}
	raw, err := json.Marshal(r.FamilyRecordInput)
	if err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO family_records(id,member_id,principal_id,request_id,payload_json,occurred_at,display_on_tv,created_at) VALUES(?,?,?,?,?,?,?,?)`, r.ID, r.MemberID, "archive:"+archive, r.ID, string(raw), r.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), r.DisplayOnTV, r.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO family_record_exports(record_id,exported_at) VALUES(?,?)`, r.ID, verifiedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RestoreFamilyRevisionLinks runs only after all immutable archive records have
// been imported into a new recovery DB. File traversal order is not revision
// order. Validate the complete graph before making it the current projection.
func (d *DB) RestoreFamilyRevisionLinks(ctx context.Context) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,payload_json FROM family_records`)
	if err != nil {
		return err
	}
	records := map[string]domain.FamilyRecordInput{}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var in domain.FamilyRecordInput
		if err = json.Unmarshal([]byte(raw), &in); err != nil {
			rows.Close()
			return err
		}
		records[id] = in
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	owners := map[string]string{}
	for id, in := range records {
		if in.SupersedesID == "" {
			continue
		}
		target, ok := records[in.SupersedesID]
		if !ok || target.MemberID != in.MemberID || target.Retracted || owners[in.SupersedesID] != "" {
			return errors.New("archive revision target is missing, mismatched, retracted or forked")
		}
		owners[in.SupersedesID] = id
	}
	// Iterative traversal avoids recursion depth proportional to archive size.
	checked := map[string]bool{}
	for id := range records {
		path := map[string]bool{}
		for current := id; current != "" && !checked[current]; current = records[current].SupersedesID {
			if path[current] {
				return errors.New("archive revision cycle")
			}
			path[current] = true
		}
		for current := range path {
			checked[current] = true
		}
	}
	for id, in := range records {
		if in.SupersedesID != "" {
			if _, err = tx.ExecContext(ctx, `UPDATE family_records SET supersedes_id=? WHERE id=?`, in.SupersedesID, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
