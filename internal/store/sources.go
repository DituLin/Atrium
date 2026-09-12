package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// Sources is the data_sources repository.
type Sources struct {
	db *DB
	ex execer
}

// Sources returns the data source repository.
func (d *DB) Sources() *Sources { return &Sources{db: d, ex: d.sql} }

const sourceColumns = `id, name, root_path, status, revoked_at, revoke_reason,
	identity_bound, identity_bound_at, health, health_detail, last_check_at, last_success_at,
	scan_generation, last_scan_started_at, last_scan_completed_at, baseline_completed_at,
	share_total_bytes, share_free_bytes, share_stats_at, created_at, updated_at`

// Upsert preserves observations on an unchanged active root. A replacement or
// reauthorization starts a new observation generation, without changing scans.
func (s *Sources) Upsert(ctx context.Context, id, name, rootPath string, now time.Time) error {
	return s.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		repo := &Sources{db: s.db, ex: tx}
		previous, err := repo.Get(ctx, id)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		changed := previous == nil || previous.Status != domain.SourceActive || previous.RootPath != rootPath
		ts := FormatTime(now)
		_, err = tx.ExecContext(ctx, `
   INSERT INTO data_sources (id, name, root_path, status, health, scan_generation, created_at, updated_at)
   VALUES (?, ?, ?, 'active', 'unknown', 0, ?, ?)
   ON CONFLICT(id) DO UPDATE SET name=excluded.name, root_path=excluded.root_path,
    status='active', revoked_at=NULL, revoke_reason=NULL, updated_at=excluded.updated_at`,
			id, name, rootPath, ts, ts)
		if err != nil {
			return fmt.Errorf("store: upsert source %q: %w", id, err)
		}
		if changed {
			return repo.resetObservation(ctx, id, now)
		}
		return nil
	})
}

// Revoke marks a source revoked with a reason; the index is kept.
func (s *Sources) Revoke(ctx context.Context, id, reason string, now time.Time) error {
	return s.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE data_sources SET status='revoked', revoked_at=?, revoke_reason=?, updated_at=? WHERE id=?`,
			FormatTime(now), reason, FormatTime(now), id)
		if err != nil {
			return fmt.Errorf("store: revoke source %q: %w", id, err)
		}
		if err := affectedOne(res, id); err != nil {
			return err
		}
		return (&Sources{db: s.db, ex: tx}).resetObservation(ctx, id, now)
	})
}

// Restore requires a new observation only when authorization changes. An
// idempotent restore of an active source preserves its current observation.
func (s *Sources) Restore(ctx context.Context, id string, now time.Time) error {
	return s.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		repo := &Sources{db: s.db, ex: tx}
		row, err := repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if row.Status == domain.SourceActive {
			return nil
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE data_sources SET status='active', revoked_at=NULL, revoke_reason=NULL, updated_at=? WHERE id=?`,
			FormatTime(now), id)
		if err != nil {
			return fmt.Errorf("store: restore source %q: %w", id, err)
		}
		return repo.resetObservation(ctx, id, now)
	})
}

// RevokeMissing revokes every active source whose ID is not in keep.
func (s *Sources) RevokeMissing(ctx context.Context, keep []string, reason string, now time.Time) ([]string, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	kept := make(map[string]struct{}, len(keep))
	for _, k := range keep {
		kept[k] = struct{}{}
	}
	var revoked []string
	for _, src := range all {
		if _, ok := kept[src.ID]; ok || src.Status == domain.SourceRevoked {
			continue
		}
		if err := s.Revoke(ctx, src.ID, reason, now); err != nil {
			return revoked, err
		}
		revoked = append(revoked, src.ID)
	}
	return revoked, nil
}

// Get returns one source or domain.ErrNotFound.
func (s *Sources) Get(ctx context.Context, id string) (*domain.Source, error) {
	row := s.ex.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM data_sources WHERE id = ?`, id)
	src, err := scanSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get source %q: %w", id, err)
	}
	return src, nil
}

// List returns all sources ordered by ID.
func (s *Sources) List(ctx context.Context) ([]domain.Source, error) {
	rows, err := s.ex.QueryContext(ctx, `SELECT `+sourceColumns+` FROM data_sources ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Source
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan source: %w", err)
		}
		out = append(out, *src)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate sources: %w", err)
	}
	return out, nil
}

// SetHealth records a probe outcome.
func (s *Sources) SetHealth(ctx context.Context, id string, health domain.Health, detail string, success bool, now time.Time) error {
	ts := FormatTime(now)
	q := `UPDATE data_sources SET health = ?, health_detail = ?, last_check_at = ?, updated_at = ?`
	args := []any{string(health), nullString(detail), ts, ts}
	if success {
		q += `, last_success_at = ?`
		args = append(args, ts)
	}
	q += ` WHERE id = ?`
	args = append(args, id)
	res, err := s.ex.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("store: set source health %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// BindIdentity stores the mount fingerprint observed on the first success.
func (s *Sources) BindIdentity(ctx context.Context, id string, ident domain.Identity, now time.Time) error {
	blob, err := json.Marshal(ident)
	if err != nil {
		return fmt.Errorf("store: encode identity: %w", err)
	}
	res, err := s.ex.ExecContext(ctx,
		`UPDATE data_sources SET identity_bound = ?, identity_bound_at = ?, updated_at = ? WHERE id = ?`,
		string(blob), FormatTime(now), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: bind identity %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetShareStats records volume capacity for the source.
func (s *Sources) SetShareStats(ctx context.Context, id string, total, free int64, now time.Time) error {
	res, err := s.ex.ExecContext(ctx,
		`UPDATE data_sources SET share_total_bytes = ?, share_free_bytes = ?, share_stats_at = ?, updated_at = ? WHERE id = ?`,
		total, free, FormatTime(now), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set share stats %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// scanner abstracts *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scanSource(sc scanner) (*domain.Source, error) {
	var (
		s                                       domain.Source
		revokedAt, revokeReason                 sql.NullString
		identityBound, identityBoundAt          sql.NullString
		healthDetail, lastCheckAt, lastSuccess  sql.NullString
		scanStarted, scanCompleted, baselineAt  sql.NullString
		shareTotal, shareFree                   sql.NullInt64
		shareStatsAt                            sql.NullString
		createdAt, updatedAt, status, healthStr string
	)
	if err := sc.Scan(&s.ID, &s.Name, &s.RootPath, &status, &revokedAt, &revokeReason,
		&identityBound, &identityBoundAt, &healthStr, &healthDetail, &lastCheckAt, &lastSuccess,
		&s.ScanGeneration, &scanStarted, &scanCompleted, &baselineAt,
		&shareTotal, &shareFree, &shareStatsAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	s.Status = domain.SourceStatus(status)
	s.Health = domain.Health(healthStr)
	s.RevokedAt = timePtr(revokedAt)
	s.RevokeReason = revokeReason.String
	s.HealthDetail = healthDetail.String
	s.LastCheckAt = timePtr(lastCheckAt)
	s.LastSuccessAt = timePtr(lastSuccess)
	s.LastScanStartedAt = timePtr(scanStarted)
	s.LastScanCompletedA = timePtr(scanCompleted)
	s.BaselineCompleted = timePtr(baselineAt)
	s.ShareTotalBytes = int64Ptr(shareTotal)
	s.ShareFreeBytes = int64Ptr(shareFree)
	s.ShareStatsAt = timePtr(shareStatsAt)
	s.CreatedAt = timeVal(createdAt)
	s.UpdatedAt = timeVal(updatedAt)
	if identityBound.Valid && identityBound.String != "" {
		var ident domain.Identity
		if err := json.Unmarshal([]byte(identityBound.String), &ident); err == nil {
			s.IdentityBound = &ident
		}
	}
	s.IdentityBoundAt = timePtr(identityBoundAt)
	return &s, nil
}

func affectedOne(res sql.Result, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%q: %w", id, domain.ErrNotFound)
	}
	return nil
}
