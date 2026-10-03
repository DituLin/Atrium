package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/domain"
	"strings"
	"time"
)

// FamilyRecords persists structured records locally; the export row commits with
// the record so a NAS outage cannot lose a pending archive operation.
type FamilyRecords struct{ db *DB }

func (d *DB) FamilyRecords() *FamilyRecords { return &FamilyRecords{db: d} }
func (r *FamilyRecords) PutMember(ctx context.Context, m domain.FamilyMember, now time.Time) error {
	if err := m.Validate(); err != nil {
		return err
	}
	_, err := r.db.sql.ExecContext(ctx, `INSERT INTO family_members(id,name,updated_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,updated_at=excluded.updated_at`, m.ID, m.Name, now.UTC().Format(time.RFC3339Nano))
	return err
}
func (r *FamilyRecords) Members(ctx context.Context) ([]domain.FamilyMember, error) {
	rows, err := r.db.sql.QueryContext(ctx, `SELECT id,name FROM family_members ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FamilyMember{}
	for rows.Next() {
		var m domain.FamilyMember
		if err = rows.Scan(&m.ID, &m.Name); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Record scopes idempotency to the authenticated principal. Callers must provide
// identity from trusted request context rather than model-authored arguments.
func (r *FamilyRecords) Record(ctx context.Context, principal, request string, in domain.FamilyRecordInput, now time.Time) (*domain.FamilyRecord, bool, error) {
	if strings.TrimSpace(principal) == "" || len(principal) > 128 || strings.TrimSpace(request) == "" || len(request) > 256 {
		return nil, false, domain.Errorf(domain.CodeInvalidRequest, "missing or oversized request identity")
	}
	if err := in.Validate(); err != nil {
		return nil, false, err
	}
	in.OccurredAt = in.OccurredAt.UTC()
	if in.Measurements == nil {
		in.Measurements = []domain.FamilyMeasurement{}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, false, err
	}
	tx, err := r.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var id, payload, created string
	err = tx.QueryRowContext(ctx, `SELECT id,payload_json,created_at FROM family_records WHERE principal_id=? AND request_id=?`, principal, request).Scan(&id, &payload, &created)
	if err == nil {
		if payload != string(raw) {
			return nil, false, domain.Errorf(domain.CodeIdempotencyConflict, "request already used for a different family record")
		}
		var exported sql.NullString
		var successor string
		if err = tx.QueryRowContext(ctx, `SELECT exported_at,COALESCE((SELECT id FROM family_records WHERE supersedes_id=?),'') FROM family_record_exports WHERE record_id=?`, id, id).Scan(&exported, &successor); err != nil {
			return nil, false, err
		}
		out := &domain.FamilyRecord{FamilyRecordInput: in, ID: id, CreatedAt: timeVal(created), ExportedAt: timePtr(exported), SupersededBy: successor}
		return out, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM family_members WHERE id=?`, in.MemberID).Scan(&count); err != nil {
		return nil, false, err
	}
	if count == 0 {
		return nil, false, domain.ErrNotFound
	}
	if in.SupersedesID != "" {
		var targetMember, targetPayload string
		if err = tx.QueryRowContext(ctx, `SELECT member_id,payload_json FROM family_records WHERE id=?`, in.SupersedesID).Scan(&targetMember, &targetPayload); errors.Is(err, sql.ErrNoRows) {
			return nil, false, domain.ErrNotFound
		} else if err != nil {
			return nil, false, err
		}
		var target domain.FamilyRecordInput
		if err = json.Unmarshal([]byte(targetPayload), &target); err != nil {
			return nil, false, err
		}
		if targetMember != in.MemberID {
			return nil, false, domain.ErrNotFound
		}
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM family_records WHERE supersedes_id=?`, in.SupersedesID).Scan(&count); err != nil {
			return nil, false, err
		}
		if target.Retracted || count != 0 {
			return nil, false, domain.Errorf(domain.CodeIdempotencyConflict, "record already superseded or retracted; query the current history")
		}
	}
	id = domain.NewID()
	created = now.UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO family_records(id,member_id,principal_id,request_id,payload_json,occurred_at,display_on_tv,created_at,supersedes_id) VALUES(?,?,?,?,?,?,?,?,NULLIF(?,''))`, id, in.MemberID, principal, request, string(raw), in.OccurredAt.Format("2006-01-02T15:04:05.000000000Z"), in.DisplayOnTV, created, in.SupersedesID)
	if err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO family_record_exports(record_id) VALUES(?)`, id); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return &domain.FamilyRecord{FamilyRecordInput: in, ID: id, CreatedAt: now.UTC()}, false, nil
}

const familyRecordSelect = `SELECT r.id,r.payload_json,r.created_at,e.exported_at,COALESCE((SELECT s.id FROM family_records s WHERE s.supersedes_id=r.id),'') FROM family_records r JOIN family_record_exports e ON e.record_id=r.id `
const familyTVFilter = `(?=0 OR (r.display_on_tv=1 AND COALESCE(json_extract(r.payload_json,'$.retracted'),0)=0 AND NOT EXISTS(SELECT 1 FROM family_records s WHERE s.supersedes_id=r.id)))`

func readFamilyRecords(rows *sql.Rows) ([]domain.FamilyRecord, error) {
	defer rows.Close()
	out := []domain.FamilyRecord{}
	for rows.Next() {
		var r domain.FamilyRecord
		var raw, created string
		var exported sql.NullString
		if err := rows.Scan(&r.ID, &raw, &created, &exported, &r.SupersededBy); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &r.FamilyRecordInput); err != nil {
			return nil, err
		}
		r.CreatedAt = timeVal(created)
		r.ExportedAt = timePtr(exported)
		out = append(out, r)
	}
	return out, rows.Err()
}
func (r *FamilyRecords) List(ctx context.Context, member string, tvOnly bool, limit int) ([]domain.FamilyRecord, error) {
	if limit < 1 || limit > 500 {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "limit must be 1 to 500")
	}
	rows, err := r.db.sql.QueryContext(ctx, familyRecordSelect+`WHERE r.member_id=? AND `+familyTVFilter+` ORDER BY r.occurred_at DESC,r.id DESC LIMIT ?`, member, tvOnly, limit)
	if err != nil {
		return nil, err
	}
	return readFamilyRecords(rows)
}
func (r *FamilyRecords) PendingExports(ctx context.Context, limit int) ([]domain.FamilyRecord, error) {
	if limit < 1 || limit > 500 {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "limit must be 1 to 500")
	}
	rows, err := r.db.sql.QueryContext(ctx, familyRecordSelect+`WHERE e.exported_at IS NULL ORDER BY r.created_at,r.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	records, err := readFamilyRecords(rows)
	for i := range records {
		records[i].SupersededBy = ""
	}
	return records, err
}
func (r *FamilyRecords) MarkExported(ctx context.Context, id string, now time.Time) error {
	res, err := r.db.sql.ExecContext(ctx, `UPDATE family_record_exports SET exported_at=COALESCE(exported_at,?) WHERE record_id=?`, now.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

type familyCursor struct {
	Member string    `json:"m"`
	TV     bool      `json:"tv"`
	At     time.Time `json:"at"`
	ID     string    `json:"id"`
}

// ListPage binds continuation to a member and projection. Authorization is
// checked independently by the API on every page; cursor data grants no access.
func (r *FamilyRecords) ListPage(ctx context.Context, member string, tvOnly bool, limit int, cursor string) (domain.FamilyRecordPage, error) {
	page := domain.FamilyRecordPage{Records: []domain.FamilyRecord{}}
	bad := func() (domain.FamilyRecordPage, error) {
		return page, domain.Errorf(domain.CodeInvalidRequest, "invalid family records page")
	}
	if limit < 1 || limit > 500 || len(cursor) > 2048 {
		return bad()
	}
	var c familyCursor
	query := familyRecordSelect + `WHERE r.member_id=? AND ` + familyTVFilter
	args := []any{member, tvOnly}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &c) != nil || c.Member != member || c.TV != tvOnly || c.At.IsZero() || c.ID == "" {
			return bad()
		}
		at := c.At.UTC().Format("2006-01-02T15:04:05.000000000Z")
		query += ` AND (r.occurred_at < ? OR (r.occurred_at = ? AND r.id < ?))`
		args = append(args, at, at, c.ID)
	}
	query += ` ORDER BY r.occurred_at DESC,r.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := r.db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	records, err := readFamilyRecords(rows)
	if err != nil {
		return page, err
	}
	if len(records) > limit {
		records = records[:limit]
		last := records[len(records)-1]
		raw, _ := json.Marshal(familyCursor{Member: member, TV: tvOnly, At: last.OccurredAt, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	page.Records = records
	return page, nil
}
