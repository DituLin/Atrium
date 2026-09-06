package store

import (
	"context"
	"fmt"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// eligible is the visibility rule for every screen-facing query: the photo is
// ready and its source is still authorized (design §6.4). Revoking a source
// hides its photos without touching a single photo row.
const eligible = `p.status = 'ready' AND s.status = 'active'`

const listColumns = `p.id, p.source_id, p.rel_path, p.ext, p.size_bytes, p.mtime_unix, p.fingerprint, p.status,
	p.width, p.height, p.orientation, p.captured_at, p.captured_offset_seconds, p.captured_confidence, p.captured_day,
	p.first_seen_at, p.last_seen_at, p.last_seen_generation, p.missing_generations, p.is_baseline,
	p.meta_status, p.meta_error, p.preview_status, p.preview_error, p.preview_attempts, p.preview_next_retry_at,
	p.removed_at, p.excluded_at, p.exclude_reason, p.created_at, p.updated_at`

// RecentCursor is the keyset position in the `recent` collection.
type RecentCursor struct {
	FirstSeenAt time.Time
	ID          string
}

// CapturedCursor is the keyset position in `captured_today`.
type CapturedCursor struct {
	CapturedAt time.Time
	ID         string
}

// AllCursor is the keyset position in `all`, which sorts undated photos last.
type AllCursor struct {
	HasCaptured bool
	CapturedAt  time.Time
	ID          string
}

// ListRecent returns non-baseline photos newest-discovered first.
func (p *Photos) ListRecent(ctx context.Context, cur *RecentCursor, limit int) ([]domain.Photo, error) {
	q := `SELECT ` + listColumns + ` FROM photos p JOIN data_sources s ON s.id = p.source_id
		WHERE ` + eligible + ` AND p.is_baseline = 0`
	var args []any
	if cur != nil {
		q += ` AND (p.first_seen_at < ? OR (p.first_seen_at = ? AND p.id < ?))`
		ts := FormatTime(cur.FirstSeenAt)
		args = append(args, ts, ts, cur.ID)
	}
	q += ` ORDER BY p.first_seen_at DESC, p.id DESC LIMIT ?`
	args = append(args, limit)
	return p.queryList(ctx, q, args...)
}

// ListCapturedDay returns photos taken on one home-timezone day, oldest first
// so the day reads as a story rather than in reverse.
func (p *Photos) ListCapturedDay(ctx context.Context, day string, cur *CapturedCursor, limit int) ([]domain.Photo, error) {
	q := `SELECT ` + listColumns + ` FROM photos p JOIN data_sources s ON s.id = p.source_id
		WHERE ` + eligible + ` AND p.captured_day = ?`
	args := []any{day}
	if cur != nil {
		q += ` AND (p.captured_at > ? OR (p.captured_at = ? AND p.id > ?))`
		ts := FormatTime(cur.CapturedAt)
		args = append(args, ts, ts, cur.ID)
	}
	q += ` ORDER BY p.captured_at ASC, p.id ASC LIMIT ?`
	args = append(args, limit)
	return p.queryList(ctx, q, args...)
}

// ListAll returns the browsing grid: newest capture first, undated photos last.
func (p *Photos) ListAll(ctx context.Context, cur *AllCursor, limit int) ([]domain.Photo, error) {
	q := `SELECT ` + listColumns + ` FROM photos p JOIN data_sources s ON s.id = p.source_id
		WHERE ` + eligible
	var args []any
	if cur != nil {
		if cur.HasCaptured {
			q += ` AND ((p.captured_at IS NOT NULL AND (p.captured_at < ? OR (p.captured_at = ? AND p.id < ?)))
				OR p.captured_at IS NULL)`
			ts := FormatTime(cur.CapturedAt)
			args = append(args, ts, ts, cur.ID)
		} else {
			q += ` AND p.captured_at IS NULL AND p.id < ?`
			args = append(args, cur.ID)
		}
	}
	q += ` ORDER BY (p.captured_at IS NULL) ASC, p.captured_at DESC, p.id DESC LIMIT ?`
	args = append(args, limit)
	return p.queryList(ctx, q, args...)
}

// ListEligibleIDs returns the IDs a random round may draw from. The cap keeps
// the shuffle bounded; a library above it is sampled from the newest slice.
func (p *Photos) ListEligibleIDs(ctx context.Context, limit int) ([]string, error) {
	rows, err := p.ex.QueryContext(ctx, `
		SELECT p.id FROM photos p JOIN data_sources s ON s.id = p.source_id
		WHERE `+eligible+` AND p.preview_status = 'ready'
		ORDER BY p.id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list eligible photo ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan photo id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate photo ids: %w", err)
	}
	return out, nil
}

// GetEligible returns a photo only when a screen is allowed to see it.
func (p *Photos) GetEligible(ctx context.Context, id string) (*domain.Photo, error) {
	rows, err := p.queryList(ctx, `SELECT `+listColumns+` FROM photos p
		JOIN data_sources s ON s.id = p.source_id WHERE `+eligible+` AND p.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	return &rows[0], nil
}

// ListByIDs returns photos for a set of IDs, preserving the caller's order.
func (p *Photos) ListByIDs(ctx context.Context, ids []string) ([]domain.Photo, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]byte, 0, len(ids)*2)
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	rows, err := p.queryList(ctx, `SELECT `+listColumns+` FROM photos p
		JOIN data_sources s ON s.id = p.source_id
		WHERE `+eligible+` AND p.id IN (`+string(placeholders)+`)`, args...)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Photo, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]domain.Photo, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// CountBaseline counts eligible photos that came in with the first import.
func (p *Photos) CountBaseline(ctx context.Context) (int64, error) {
	var n int64
	err := p.ex.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM photos p JOIN data_sources s ON s.id = p.source_id
		WHERE `+eligible+` AND p.is_baseline = 1`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count baseline photos: %w", err)
	}
	return n, nil
}

func (p *Photos) queryList(ctx context.Context, q string, args ...any) ([]domain.Photo, error) {
	rows, err := p.ex.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query photos: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Photo
	for rows.Next() {
		ph, err := scanPhoto(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan photo: %w", err)
		}
		out = append(out, *ph)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate photos: %w", err)
	}
	return out, nil
}

// Neighbours returns the IDs adjacent to a photo inside a collection, used by
// the photo viewer's previous/next actions. An empty string means the photo is
// at that end of the collection.
func (p *Photos) Neighbours(ctx context.Context, collection domain.Collection, day string, photo *domain.Photo) (prev, next string, err error) {
	base := `SELECT p.id FROM photos p JOIN data_sources s ON s.id = p.source_id WHERE ` + eligible
	switch collection {
	case domain.CollectionRecent:
		ts := FormatTime(photo.FirstSeenAt)
		cond := ` AND p.is_baseline = 0`
		prev, err = p.scalar(ctx, base+cond+
			` AND (p.first_seen_at > ? OR (p.first_seen_at = ? AND p.id > ?))
			  ORDER BY p.first_seen_at ASC, p.id ASC LIMIT 1`, ts, ts, photo.ID)
		if err != nil {
			return "", "", err
		}
		next, err = p.scalar(ctx, base+cond+
			` AND (p.first_seen_at < ? OR (p.first_seen_at = ? AND p.id < ?))
			  ORDER BY p.first_seen_at DESC, p.id DESC LIMIT 1`, ts, ts, photo.ID)
	case domain.CollectionCapturedToday:
		if photo.CapturedAt == nil {
			return "", "", nil
		}
		ts := FormatTime(*photo.CapturedAt)
		cond := ` AND p.captured_day = ?`
		prev, err = p.scalar(ctx, base+cond+
			` AND (p.captured_at < ? OR (p.captured_at = ? AND p.id < ?))
			  ORDER BY p.captured_at DESC, p.id DESC LIMIT 1`, day, ts, ts, photo.ID)
		if err != nil {
			return "", "", err
		}
		next, err = p.scalar(ctx, base+cond+
			` AND (p.captured_at > ? OR (p.captured_at = ? AND p.id > ?))
			  ORDER BY p.captured_at ASC, p.id ASC LIMIT 1`, day, ts, ts, photo.ID)
	case domain.CollectionAll:
		return p.neighboursAll(ctx, base, photo)
	default:
		// A random round has no stable order, so it has no neighbours.
		return "", "", nil
	}
	return prev, next, err
}

// neighboursAll walks the "captured desc, undated last" order.
func (p *Photos) neighboursAll(ctx context.Context, base string, photo *domain.Photo) (string, string, error) {
	if photo.CapturedAt == nil {
		prev, err := p.scalar(ctx, base+
			` AND p.captured_at IS NULL AND p.id > ? ORDER BY p.id ASC LIMIT 1`, photo.ID)
		if err != nil {
			return "", "", err
		}
		if prev == "" {
			prev, err = p.scalar(ctx, base+
				` AND p.captured_at IS NOT NULL ORDER BY p.captured_at ASC, p.id ASC LIMIT 1`)
			if err != nil {
				return "", "", err
			}
		}
		next, err := p.scalar(ctx, base+
			` AND p.captured_at IS NULL AND p.id < ? ORDER BY p.id DESC LIMIT 1`, photo.ID)
		return prev, next, err
	}
	ts := FormatTime(*photo.CapturedAt)
	prev, err := p.scalar(ctx, base+
		` AND p.captured_at IS NOT NULL AND (p.captured_at > ? OR (p.captured_at = ? AND p.id > ?))
		  ORDER BY p.captured_at ASC, p.id ASC LIMIT 1`, ts, ts, photo.ID)
	if err != nil {
		return "", "", err
	}
	next, err := p.scalar(ctx, base+
		` AND p.captured_at IS NOT NULL AND (p.captured_at < ? OR (p.captured_at = ? AND p.id < ?))
		  ORDER BY p.captured_at DESC, p.id DESC LIMIT 1`, ts, ts, photo.ID)
	if err != nil {
		return "", "", err
	}
	if next == "" {
		next, err = p.scalar(ctx, base+` AND p.captured_at IS NULL ORDER BY p.id DESC LIMIT 1`)
	}
	return prev, next, err
}

// scalar runs a query expected to return at most one string.
func (p *Photos) scalar(ctx context.Context, q string, args ...any) (string, error) {
	rows, err := p.ex.QueryContext(ctx, q, args...)
	if err != nil {
		return "", fmt.Errorf("store: neighbour query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return "", rows.Err()
	}
	var id string
	if err := rows.Scan(&id); err != nil {
		return "", fmt.Errorf("store: scan neighbour: %w", err)
	}
	return id, nil
}
