package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// ScopedPhotoQuery applies authorization before ordering or pagination.
// Empty SourceIDs grants no access. A cursor is validated by the caller.
type ScopedPhotoQuery struct {
	SourceIDs  []string
	Collection domain.Collection
	Day        string
	Limit      int
	AfterID    string
	AfterTime  time.Time
	HasTime    bool
}

// ScopedPhotoCounts contains no counts from hidden sources or excluded photos.
type ScopedPhotoCounts struct {
	Ready           int64 `json:"ready"`
	PendingPreview  int64 `json:"pending_preview"`
	Unsupported     int64 `json:"unsupported"`
	UnknownCaptured int64 `json:"unknown_captured"`
	Baseline        int64 `json:"baseline"`
	CapturedToday   int64 `json:"captured_today"`
	NewToday        int64 `json:"new_today"`
}

func scopedSources(ids []string) (string, []any) {
	if len(ids) == 0 {
		return " AND 0", nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return " AND p.source_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")", args
}

const scopedReady = eligible + ` AND p.preview_status = 'ready'`

// ListScoped returns at most 50 ready previews from explicitly allowed sources.
func (p *Photos) ListScoped(ctx context.Context, query ScopedPhotoQuery) ([]domain.Photo, error) {
	if query.Limit < 1 || query.Limit > 50 {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "limit must be between 1 and 50")
	}
	if query.Collection != domain.CollectionRecent && query.Collection != domain.CollectionAll && query.Collection != domain.CollectionCapturedToday {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "invalid collection")
	}
	scope, args := scopedSources(query.SourceIDs)
	q := `SELECT ` + listColumns + ` FROM photos p JOIN data_sources s ON s.id=p.source_id WHERE ` + scopedReady + scope
	switch query.Collection {
	case domain.CollectionRecent:
		q += ` AND p.is_baseline=0`
		if query.AfterID != "" {
			q += ` AND (p.first_seen_at < ? OR (p.first_seen_at = ? AND p.id < ?))`
			ts := FormatTime(query.AfterTime)
			args = append(args, ts, ts, query.AfterID)
		}
		q += ` ORDER BY p.first_seen_at DESC,p.id DESC`
	case domain.CollectionCapturedToday:
		q += ` AND p.captured_day=?`
		args = append(args, query.Day)
		if query.AfterID != "" {
			q += ` AND (p.captured_at > ? OR (p.captured_at = ? AND p.id > ?))`
			ts := FormatTime(query.AfterTime)
			args = append(args, ts, ts, query.AfterID)
		}
		q += ` ORDER BY p.captured_at ASC,p.id ASC`
	case domain.CollectionAll:
		if query.AfterID != "" {
			if query.HasTime {
				q += ` AND ((p.captured_at IS NOT NULL AND (p.captured_at < ? OR (p.captured_at = ? AND p.id < ?))) OR p.captured_at IS NULL)`
				ts := FormatTime(query.AfterTime)
				args = append(args, ts, ts, query.AfterID)
			} else {
				q += ` AND p.captured_at IS NULL AND p.id < ?`
				args = append(args, query.AfterID)
			}
		}
		q += ` ORDER BY (p.captured_at IS NULL),p.captured_at DESC,p.id DESC`
	}
	q += ` LIMIT ?`
	args = append(args, query.Limit)
	return p.queryList(ctx, q, args...)
}

// GetScoped never returns an unauthorized, revoked, or unusable photo.
func (p *Photos) GetScoped(ctx context.Context, sources []string, id string) (*domain.Photo, error) {
	scope, args := scopedSources(sources)
	args = append(args, id)
	rows, err := p.queryList(ctx, `SELECT `+listColumns+` FROM photos p JOIN data_sources s ON s.id=p.source_id WHERE `+scopedReady+scope+` AND p.id=?`, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	return &rows[0], nil
}

// ScopedEligibleIDs supports a bounded seeded shuffle without including hidden IDs.
func (p *Photos) ScopedEligibleIDs(ctx context.Context, sources []string, limit int) ([]string, error) {
	if limit < 1 || limit > 100000 {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "invalid random pool limit")
	}
	scope, args := scopedSources(sources)
	args = append(args, limit)
	rows, err := p.ex.QueryContext(ctx, `SELECT p.id FROM photos p JOIN data_sources s ON s.id=p.source_id WHERE `+scopedReady+scope+` ORDER BY p.id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: scoped photo IDs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CountScoped computes a small projection directly in SQL, with no shared cache.
func (p *Photos) CountScoped(ctx context.Context, sources []string, day string, dayStart time.Time) (ScopedPhotoCounts, error) {
	scope, scopeArgs := scopedSources(sources)
	args := []any{day, FormatTime(dayStart)}
	args = append(args, scopeArgs...)
	var c ScopedPhotoCounts
	err := p.ex.QueryRowContext(ctx, `SELECT
 COUNT(CASE WHEN p.status='ready' AND p.preview_status='ready' THEN 1 END),
 COUNT(CASE WHEN p.status IN ('pending','ready') AND p.preview_status!='ready' THEN 1 END),
 COUNT(CASE WHEN p.status='unsupported' THEN 1 END),
 COUNT(CASE WHEN p.status='ready' AND p.preview_status='ready' AND p.captured_at IS NULL THEN 1 END),
 COUNT(CASE WHEN p.status='ready' AND p.preview_status='ready' AND p.is_baseline=1 THEN 1 END),
 COUNT(CASE WHEN p.status='ready' AND p.preview_status='ready' AND p.captured_day=? THEN 1 END),
 COUNT(CASE WHEN p.status='ready' AND p.preview_status='ready' AND p.is_baseline=0 AND p.first_seen_at>=? THEN 1 END)
 FROM photos p JOIN data_sources s ON s.id=p.source_id WHERE s.status='active' AND p.status NOT IN ('excluded','removed')`+scope, args...).Scan(&c.Ready, &c.PendingPreview, &c.Unsupported, &c.UnknownCaptured, &c.Baseline, &c.CapturedToday, &c.NewToday)
	return c, err
}
