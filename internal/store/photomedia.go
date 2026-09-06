package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// MetaResult is what the extract_meta job writes back.
type MetaResult struct {
	Fingerprint           string
	Width                 int
	Height                int
	Orientation           int
	CapturedAt            *time.Time
	CapturedOffsetSeconds *int
	CapturedConfidence    domain.CapturedConfidence
	CapturedDay           string
	Error                 string
}

// SetMeta stores the outcome of metadata extraction.
func (p *Photos) SetMeta(ctx context.Context, id string, res MetaResult, now time.Time) error {
	sqlRes, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET fingerprint = ?, width = ?, height = ?, orientation = ?,
			captured_at = ?, captured_offset_seconds = ?, captured_confidence = ?, captured_day = ?,
			meta_status = 'ready', meta_error = ?, updated_at = ?
		WHERE id = ?`,
		nullString(res.Fingerprint), nullZeroInt(res.Width), nullZeroInt(res.Height), nullZeroInt(res.Orientation),
		FormatTimePtr(res.CapturedAt), nullIntPtr(res.CapturedOffsetSeconds),
		string(res.CapturedConfidence), nullString(res.CapturedDay),
		nullString(res.Error), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set photo meta %q: %w", id, err)
	}
	return affectedOne(sqlRes, id)
}

// SetMetaFailed records that metadata could not be read at all.
func (p *Photos) SetMetaFailed(ctx context.Context, id, code string, now time.Time) error {
	res, err := p.ex.ExecContext(ctx,
		`UPDATE photos SET meta_status = 'failed', meta_error = ?, updated_at = ? WHERE id = ?`,
		nullString(code), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set photo meta failed %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetPreviewProcessing marks a preview build as in flight.
func (p *Photos) SetPreviewProcessing(ctx context.Context, id string, now time.Time) error {
	res, err := p.ex.ExecContext(ctx,
		`UPDATE photos SET preview_status = 'processing', updated_at = ? WHERE id = ?`,
		FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set preview processing %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetPreviewReady publishes a photo: the preview exists, so a pending row
// becomes ready and therefore eligible for screens.
func (p *Photos) SetPreviewReady(ctx context.Context, id string, now time.Time) error {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET preview_status = 'ready', preview_error = NULL, preview_attempts = 0,
			preview_next_retry_at = NULL,
			status = CASE WHEN status IN ('pending','unsupported') THEN 'ready' ELSE status END,
			updated_at = ?
		WHERE id = ?`, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set preview ready %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetPreviewRetry records a recoverable failure and when to try again.
func (p *Photos) SetPreviewRetry(ctx context.Context, id, code string, nextRetry, now time.Time) error {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET preview_status = 'pending', preview_error = ?, preview_attempts = preview_attempts + 1,
			preview_next_retry_at = ?, updated_at = ?
		WHERE id = ?`, nullString(code), FormatTime(nextRetry), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set preview retry %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetPreviewFailed records a terminal preview failure.
func (p *Photos) SetPreviewFailed(ctx context.Context, id, code string, now time.Time) error {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET preview_status = 'failed', preview_error = ?, preview_attempts = preview_attempts + 1,
			preview_next_retry_at = NULL, updated_at = ?
		WHERE id = ?`, nullString(code), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set preview failed %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetUnsupported promotes a decode failure to a permanent format verdict. The
// row stays in the index so diagnostics can count it, but it never enters a
// collection again until a full rescan or an explicit retry.
func (p *Photos) SetUnsupported(ctx context.Context, id, code string, now time.Time) error {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET status = 'unsupported', preview_status = 'failed', preview_error = ?,
			preview_attempts = preview_attempts + 1, preview_next_retry_at = NULL, updated_at = ?
		WHERE id = ?`, nullString(code), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set photo unsupported %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetPreviewEvicted records that the cached file was reclaimed. The index row
// is kept: eviction and removal are different states (PRD §5.2).
func (p *Photos) SetPreviewEvicted(ctx context.Context, id string, now time.Time) error {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET preview_status = 'evicted', preview_error = NULL, updated_at = ?
		WHERE id = ? AND status != 'removed'`, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set preview evicted %q: %w", id, err)
	}
	_ = res
	return nil
}

// Retry clears the preview failure state so the pipeline picks the photo up
// again (admin `photos retry`).
func (p *Photos) Retry(ctx context.Context, id string, now time.Time) error {
	res, err := p.ex.ExecContext(ctx, `
		UPDATE photos SET meta_status = CASE WHEN meta_status = 'failed' THEN 'pending' ELSE meta_status END,
			meta_error = NULL, preview_status = 'pending', preview_error = NULL,
			preview_attempts = 0, preview_next_retry_at = NULL,
			status = CASE WHEN status = 'unsupported' THEN 'pending' ELSE status END,
			updated_at = ?
		WHERE id = ?`, FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: retry photo %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// SetExcluded hides or unhides one photo.
func (p *Photos) SetExcluded(ctx context.Context, id string, excluded bool, reason string, now time.Time) error {
	var q string
	var args []any
	if excluded {
		q = `UPDATE photos SET status = 'excluded', excluded_at = ?, exclude_reason = ?, updated_at = ? WHERE id = ?`
		args = []any{FormatTime(now), nullString(reason), FormatTime(now), id}
	} else {
		q = `UPDATE photos SET status = 'pending', excluded_at = NULL, exclude_reason = NULL, updated_at = ? WHERE id = ?`
		args = []any{FormatTime(now), id}
	}
	res, err := p.ex.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("store: set photo excluded %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// escapeLike neutralises LIKE wildcards in an operator-supplied prefix.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// trimSlash normalises a directory prefix to its slash-free form.
func trimSlash(s string) string { return strings.Trim(s, "/") }

// CapturedRow is the minimal projection used by the recompute_day job.
type CapturedRow struct {
	ID         string
	CapturedAt time.Time
}

// ListCapturedAfter pages through photos that have a capture time, ordered by
// ID so a batch job can resume without a cursor table.
func (p *Photos) ListCapturedAfter(ctx context.Context, afterID string, limit int) ([]CapturedRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := p.ex.QueryContext(ctx, `
		SELECT id, captured_at FROM photos
		WHERE captured_at IS NOT NULL AND id > ? ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list captured photos: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CapturedRow
	for rows.Next() {
		var r CapturedRow
		var at string
		if err := rows.Scan(&r.ID, &at); err != nil {
			return nil, fmt.Errorf("store: scan captured photo: %w", err)
		}
		r.CapturedAt = timeVal(at)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate captured photos: %w", err)
	}
	return out, nil
}

// SetCapturedDay rewrites the home-timezone day of one photo.
func (p *Photos) SetCapturedDay(ctx context.Context, id, day string, now time.Time) error {
	_, err := p.ex.ExecContext(ctx,
		`UPDATE photos SET captured_day = ?, updated_at = ? WHERE id = ?`,
		nullString(day), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set captured day %q: %w", id, err)
	}
	return nil
}

// SetDimensions records the true pixel size of a photo. It is written by the
// preview builder for the common case of a file whose EXIF carries no pixel
// dimension tags, which is every PNG and most non-camera JPEGs.
func (p *Photos) SetDimensions(ctx context.Context, id string, width, height int, now time.Time) error {
	_, err := p.ex.ExecContext(ctx,
		`UPDATE photos SET width = ?, height = ?, updated_at = ? WHERE id = ?`,
		nullZeroInt(width), nullZeroInt(height), FormatTime(now), id)
	if err != nil {
		return fmt.Errorf("store: set photo dimensions %q: %w", id, err)
	}
	return nil
}
