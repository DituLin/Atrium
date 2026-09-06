package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// Photos is the photos repository.
type Photos struct {
	db *DB
	// ex is the database or the caller's batch transaction.
	ex execer
}

// Photos returns the photo repository.
func (d *DB) Photos() *Photos { return &Photos{db: d, ex: d.sql} }

const photoColumns = `id, source_id, rel_path, ext, size_bytes, mtime_unix, fingerprint, status,
	width, height, orientation, captured_at, captured_offset_seconds, captured_confidence, captured_day,
	first_seen_at, last_seen_at, last_seen_generation, missing_generations, is_baseline,
	meta_status, meta_error, preview_status, preview_error, preview_attempts, preview_next_retry_at,
	removed_at, excluded_at, exclude_reason, created_at, updated_at`

// Insert stores a newly discovered photo.
func (p *Photos) Insert(ctx context.Context, ph *domain.Photo) error {
	if ph.ID == "" {
		ph.ID = domain.NewID()
	}
	if ph.CapturedConfidence == "" {
		ph.CapturedConfidence = domain.CapturedUnknown
	}
	if ph.MetaStatus == "" {
		ph.MetaStatus = domain.MetaPending
	}
	if ph.PreviewStatus == "" {
		ph.PreviewStatus = domain.PreviewPending
	}
	_, err := p.ex.ExecContext(ctx, `
		INSERT INTO photos (`+photoColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ph.ID, ph.SourceID, ph.RelPath, ph.Ext, ph.SizeBytes, ph.MtimeUnix, nullString(ph.Fingerprint), string(ph.Status),
		nullZeroInt(ph.Width), nullZeroInt(ph.Height), nullZeroInt(ph.Orientation),
		FormatTimePtr(ph.CapturedAt), nullIntPtr(ph.CapturedOffsetSeconds), string(ph.CapturedConfidence), nullString(ph.CapturedDay),
		FormatTime(ph.FirstSeenAt), FormatTime(ph.LastSeenAt), ph.LastSeenGeneration, ph.MissingGenerations, boolInt(ph.IsBaseline),
		string(ph.MetaStatus), nullString(ph.MetaError), string(ph.PreviewStatus), nullString(ph.PreviewError),
		ph.PreviewAttempts, FormatTimePtr(ph.PreviewNextRetryAt),
		FormatTimePtr(ph.RemovedAt), FormatTimePtr(ph.ExcludedAt), nullString(ph.ExcludeReason),
		FormatTime(ph.CreatedAt), FormatTime(ph.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: insert photo: %w", err)
	}
	return nil
}

// Get returns one photo by ID.
func (p *Photos) Get(ctx context.Context, id string) (*domain.Photo, error) {
	row := p.ex.QueryRowContext(ctx, `SELECT `+photoColumns+` FROM photos WHERE id = ?`, id)
	ph, err := scanPhoto(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get photo %q: %w", id, err)
	}
	return ph, nil
}

// GetByPath returns one photo by its source and relative path.
func (p *Photos) GetByPath(ctx context.Context, sourceID, relPath string) (*domain.Photo, error) {
	row := p.ex.QueryRowContext(ctx,
		`SELECT `+photoColumns+` FROM photos WHERE source_id = ? AND rel_path = ?`, sourceID, relPath)
	ph, err := scanPhoto(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get photo by path: %w", err)
	}
	return ph, nil
}

// SetStatus changes the lifecycle status of a photo.
func (p *Photos) SetStatus(ctx context.Context, id string, status domain.PhotoStatus, now time.Time) error {
	res, err := p.ex.ExecContext(ctx,
		`UPDATE photos SET status = ?, updated_at = ? WHERE id = ?`, string(status), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set photo status %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetPreviewStatus records the preview state and optional error code.
func (p *Photos) SetPreviewStatus(ctx context.Context, id string, status domain.PreviewStatus, errCode string, now time.Time) error {
	res, err := p.ex.ExecContext(ctx,
		`UPDATE photos SET preview_status = ?, preview_error = ?, updated_at = ? WHERE id = ?`,
		string(status), nullString(errCode), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set preview status %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// StatusCounts returns the number of photos per status for eligible sources.
func (p *Photos) StatusCounts(ctx context.Context) (map[domain.PhotoStatus]int64, error) {
	rows, err := p.ex.QueryContext(ctx, `SELECT status, COUNT(*) FROM photos GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("store: photo status counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[domain.PhotoStatus]int64)
	for rows.Next() {
		var s string
		var n int64
		if err := rows.Scan(&s, &n); err != nil {
			return nil, fmt.Errorf("store: scan photo counts: %w", err)
		}
		out[domain.PhotoStatus(s)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate photo counts: %w", err)
	}
	return out, nil
}

// PreviewStatusCounts returns the number of photos per preview status.
func (p *Photos) PreviewStatusCounts(ctx context.Context) (map[domain.PreviewStatus]int64, error) {
	rows, err := p.ex.QueryContext(ctx, `SELECT preview_status, COUNT(*) FROM photos GROUP BY preview_status`)
	if err != nil {
		return nil, fmt.Errorf("store: preview status counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[domain.PreviewStatus]int64)
	for rows.Next() {
		var s string
		var n int64
		if err := rows.Scan(&s, &n); err != nil {
			return nil, fmt.Errorf("store: scan preview counts: %w", err)
		}
		out[domain.PreviewStatus(s)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate preview counts: %w", err)
	}
	return out, nil
}

// CountReady counts photos that screens may see: ready and on an active source.
func (p *Photos) CountReady(ctx context.Context) (int64, error) {
	var n int64
	err := p.ex.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM photos p
		JOIN data_sources s ON s.id = p.source_id
		WHERE p.status = 'ready' AND s.status = 'active'`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count ready photos: %w", err)
	}
	return n, nil
}

// CountCapturedDay counts eligible photos whose home day equals day.
func (p *Photos) CountCapturedDay(ctx context.Context, day string) (int64, error) {
	var n int64
	err := p.ex.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM photos p
		JOIN data_sources s ON s.id = p.source_id
		WHERE p.status = 'ready' AND s.status = 'active' AND p.captured_day = ?`, day).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count captured day: %w", err)
	}
	return n, nil
}

// CountFirstSeenSince counts eligible non-baseline photos first seen at or after t.
func (p *Photos) CountFirstSeenSince(ctx context.Context, t time.Time) (int64, error) {
	var n int64
	err := p.ex.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM photos p
		JOIN data_sources s ON s.id = p.source_id
		WHERE p.status = 'ready' AND s.status = 'active' AND p.is_baseline = 0 AND p.first_seen_at >= ?`,
		FormatTime(t)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count new photos: %w", err)
	}
	return n, nil
}

// CountUnknownCaptured counts eligible photos with an unknown capture time.
func (p *Photos) CountUnknownCaptured(ctx context.Context) (int64, error) {
	var n int64
	err := p.ex.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM photos p
		JOIN data_sources s ON s.id = p.source_id
		WHERE p.status = 'ready' AND s.status = 'active' AND p.captured_confidence = 'unknown'`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count unknown captured: %w", err)
	}
	return n, nil
}

func scanPhoto(sc scanner) (*domain.Photo, error) {
	var (
		ph                                    domain.Photo
		fingerprint                           sql.NullString
		width, height, orientation            sql.NullInt64
		capturedAt, capturedDay               sql.NullString
		capturedOffset                        sql.NullInt64
		capturedConfidence                    string
		firstSeen, lastSeen                   string
		isBaseline                            int64
		metaStatus, previewStatus             string
		metaErr, previewErr, previewNextRetry sql.NullString
		removedAt, excludedAt, excludeReason  sql.NullString
		createdAt, updatedAt, statusStr       string
	)
	if err := sc.Scan(&ph.ID, &ph.SourceID, &ph.RelPath, &ph.Ext, &ph.SizeBytes, &ph.MtimeUnix,
		&fingerprint, &statusStr, &width, &height, &orientation,
		&capturedAt, &capturedOffset, &capturedConfidence, &capturedDay,
		&firstSeen, &lastSeen, &ph.LastSeenGeneration, &ph.MissingGenerations, &isBaseline,
		&metaStatus, &metaErr, &previewStatus, &previewErr, &ph.PreviewAttempts, &previewNextRetry,
		&removedAt, &excludedAt, &excludeReason, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	ph.Fingerprint = fingerprint.String
	ph.Status = domain.PhotoStatus(statusStr)
	ph.Width = int(width.Int64)
	ph.Height = int(height.Int64)
	ph.Orientation = int(orientation.Int64)
	ph.CapturedAt = timePtr(capturedAt)
	ph.CapturedOffsetSeconds = intPtr(capturedOffset)
	ph.CapturedConfidence = domain.CapturedConfidence(capturedConfidence)
	ph.CapturedDay = capturedDay.String
	ph.FirstSeenAt = timeVal(firstSeen)
	ph.LastSeenAt = timeVal(lastSeen)
	ph.IsBaseline = isBaseline != 0
	ph.MetaStatus = domain.MetaStatus(metaStatus)
	ph.MetaError = metaErr.String
	ph.PreviewStatus = domain.PreviewStatus(previewStatus)
	ph.PreviewError = previewErr.String
	ph.PreviewNextRetryAt = timePtr(previewNextRetry)
	ph.RemovedAt = timePtr(removedAt)
	ph.ExcludedAt = timePtr(excludedAt)
	ph.ExcludeReason = excludeReason.String
	ph.CreatedAt = timeVal(createdAt)
	ph.UpdatedAt = timeVal(updatedAt)
	return &ph, nil
}

// Exclusions is the photo_exclusions repository.
type Exclusions struct{ db *DB }

// Exclusions returns the exclusion repository.
func (d *DB) Exclusions() *Exclusions { return &Exclusions{db: d} }

// Add stores an exclusion rule; duplicates return domain.ErrConflict.
func (e *Exclusions) Add(ctx context.Context, ex *domain.Exclusion) error {
	if ex.ID == "" {
		ex.ID = domain.NewID()
	}
	if ex.CreatedAt.IsZero() {
		ex.CreatedAt = time.Now()
	}
	_, err := e.db.sql.ExecContext(ctx,
		`INSERT INTO photo_exclusions (id, source_id, match_kind, pattern, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ex.ID, ex.SourceID, string(ex.MatchKind), ex.Pattern, nullString(ex.Reason), FormatTime(ex.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("exclusion %s/%s: %w", ex.SourceID, ex.Pattern, domain.ErrConflict)
		}
		return fmt.Errorf("store: add exclusion: %w", err)
	}
	return nil
}

// List returns every exclusion, optionally limited to one source.
func (e *Exclusions) List(ctx context.Context, sourceID string) ([]domain.Exclusion, error) {
	q := `SELECT id, source_id, match_kind, pattern, reason, created_at FROM photo_exclusions`
	var args []any
	if sourceID != "" {
		q += ` WHERE source_id = ?`
		args = append(args, sourceID)
	}
	q += ` ORDER BY created_at, id`
	rows, err := e.db.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list exclusions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Exclusion
	for rows.Next() {
		var ex domain.Exclusion
		var kind, createdAt string
		var reason sql.NullString
		if err := rows.Scan(&ex.ID, &ex.SourceID, &kind, &ex.Pattern, &reason, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan exclusion: %w", err)
		}
		ex.MatchKind = domain.MatchKind(kind)
		ex.Reason = reason.String
		ex.CreatedAt = timeVal(createdAt)
		out = append(out, ex)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate exclusions: %w", err)
	}
	return out, nil
}

// Delete removes an exclusion by ID.
func (e *Exclusions) Delete(ctx context.Context, id string) error {
	res, err := e.db.sql.ExecContext(ctx, `DELETE FROM photo_exclusions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete exclusion %q: %w", id, err)
	}
	return affectedOne(res, id)
}
