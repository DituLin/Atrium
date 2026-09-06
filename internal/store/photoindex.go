package store

import (
	"context"
	"fmt"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// RemovalThreshold is the number of consecutive completed scans a file must be
// missing from before it is marked removed (PRD §5.2, FR-08).
const RemovalThreshold = 2

// TouchSeen records that an unchanged file was observed in a scan generation.
func (p *Photos) TouchSeen(ctx context.Context, id string, generation int64, now time.Time) error {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET last_seen_at = ?, last_seen_generation = ?, missing_generations = 0, updated_at = ?
		WHERE id = ?`, FormatTime(now), generation, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: touch photo %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// MarkChanged records a new size or mtime for an existing row and resets the
// derived state: the fingerprint, metadata and preview all belong to the
// previous version of the file.
func (p *Photos) MarkChanged(ctx context.Context, id string, size, mtime, generation int64, now time.Time) error {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET size_bytes = ?, mtime_unix = ?, fingerprint = NULL,
			status = CASE WHEN status IN ('removed','pending','ready','unsupported') THEN 'pending' ELSE status END,
			meta_status = 'pending', meta_error = NULL,
			preview_status = 'pending', preview_error = NULL, preview_attempts = 0, preview_next_retry_at = NULL,
			last_seen_at = ?, last_seen_generation = ?, missing_generations = 0, removed_at = NULL, updated_at = ?
		WHERE id = ?`,
		size, mtime, FormatTime(now), generation, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: mark photo changed %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// Revive brings back a previously removed path. It is a genuine discovery
// event, so first_seen_at is reset and the baseline flag is cleared (PRD §5.2).
func (p *Photos) Revive(ctx context.Context, id string, size, mtime, generation int64, now time.Time) error {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET size_bytes = ?, mtime_unix = ?, fingerprint = NULL, status = 'pending',
			meta_status = 'pending', meta_error = NULL,
			preview_status = 'pending', preview_error = NULL, preview_attempts = 0, preview_next_retry_at = NULL,
			first_seen_at = ?, last_seen_at = ?, last_seen_generation = ?, missing_generations = 0,
			is_baseline = 0, removed_at = NULL, updated_at = ?
		WHERE id = ?`,
		size, mtime, FormatTime(now), FormatTime(now), generation, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: revive photo %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// IncrementMissing advances the missing counter for every live row that a
// completed scan did not observe. Only a completed scan with a confirmed
// identity may call this (FR-08: a failed scan never mass-deletes).
func (p *Photos) IncrementMissing(ctx context.Context, sourceID string, generation int64, now time.Time) (int64, error) {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET missing_generations = missing_generations + 1, updated_at = ?
		WHERE source_id = ? AND last_seen_generation < ? AND status IN ('ready','pending','unsupported')`,
		FormatTime(now), sourceID, generation)
	if err != nil {
		return 0, fmt.Errorf("store: increment missing for %q: %w", sourceID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// PromoteRemoved marks rows missing for at least RemovalThreshold generations
// as removed and returns their IDs so their cached previews can be deleted.
func (p *Photos) PromoteRemoved(ctx context.Context, sourceID string, now time.Time) ([]string, error) {
	rows, err := p.ex.QueryContext(ctx, `
		SELECT id FROM photos
		WHERE source_id = ? AND missing_generations >= ? AND status IN ('ready','pending','unsupported')`,
		sourceID, RemovalThreshold)
	if err != nil {
		return nil, fmt.Errorf("store: select removable photos: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan removable photo: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate removable photos: %w", err)
	}
	for _, id := range ids {
		if _, err := p.ex.ExecContext(ctx,
			`UPDATE photos SET status = 'removed', removed_at = ?, preview_status = 'unavailable', updated_at = ?
			 WHERE id = ?`, FormatTime(now), FormatTime(now), id); err != nil {
			return ids, fmt.Errorf("store: mark photo removed %q: %w", id, err)
		}
	}
	return ids, nil
}

// CompleteScan commits a scan generation onto the source and, on the first
// completed scan, closes the baseline import window.
func (s *Sources) CompleteScan(ctx context.Context, id string, generation int64, now time.Time) error {
	ts := FormatTime(now)
	res, err := s.db.sql.ExecContext(ctx, `
		UPDATE data_sources SET scan_generation = ?, last_scan_completed_at = ?,
			baseline_completed_at = COALESCE(baseline_completed_at, ?), updated_at = ?
		WHERE id = ?`, generation, ts, ts, ts, id)
	if err != nil {
		return fmt.Errorf("store: complete scan for %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// StartScan records that a scan began.
func (s *Sources) StartScan(ctx context.Context, id string, now time.Time) error {
	res, err := s.db.sql.ExecContext(ctx,
		`UPDATE data_sources SET last_scan_started_at = ?, updated_at = ? WHERE id = ?`,
		FormatTime(now), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: start scan for %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// ExcludeMatching marks every photo covered by an exclusion rule as excluded.
func (p *Photos) ExcludeMatching(ctx context.Context, sourceID string, kind domain.MatchKind, pattern, reason string, now time.Time) (int64, error) {
	q := `UPDATE photos SET status = 'excluded', excluded_at = ?, exclude_reason = ?, updated_at = ?
	      WHERE source_id = ? AND status != 'excluded' AND `
	args := []any{FormatTime(now), nullString(reason), FormatTime(now), sourceID}
	if kind == domain.MatchPrefix {
		q += `(rel_path = ? OR rel_path LIKE ? ESCAPE '\')`
		args = append(args, trimSlash(pattern), escapeLike(trimSlash(pattern))+`/%`)
	} else {
		q += `rel_path = ?`
		args = append(args, pattern)
	}
	res, err := p.ex.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("store: exclude photos: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// CountBySourceStatus counts photos of one source in a given status.
func (p *Photos) CountBySourceStatus(ctx context.Context, sourceID string, status domain.PhotoStatus) (int64, error) {
	var n int64
	err := p.ex.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM photos WHERE source_id = ? AND status = ?`, sourceID, string(status)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count photos by status: %w", err)
	}
	return n, nil
}
