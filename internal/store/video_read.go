package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// VideoScope is supplied by the caller after checking configured source identity.
// Root and extensions are rechecked in the query; it does not grant screen auth.
type VideoScope struct {
	SourceID, Root string
	MP4, MOV       bool
}

// VideoPosition is a descending keyset position, independent of row offsets.
type VideoPosition struct {
	Time time.Time
	ID   string
}

func videoScopeWhere(scopes []VideoScope) (string, []any) {
	parts := []string{}
	args := []any{}
	for _, s := range scopes {
		if !s.MP4 && !s.MOV {
			continue
		}
		parts = append(parts, `(s.id=? AND s.root_path=? AND ((v.ext='mp4' AND ?) OR (v.ext='mov' AND ?)))`)
		args = append(args, s.SourceID, s.Root, s.MP4, s.MOV)
	}
	if len(parts) == 0 {
		return "0", args
	}
	return `s.status='active' AND v.status IN ('pending','ready','unsupported') AND (` + strings.Join(parts, " OR ") + `) AND ` + videoNotExcluded, args
}

// Read metadata is hidden after reauthorization until a new worker republishes.
func visibleVideoColumns() string {
	current := `EXISTS (SELECT 1 FROM video_work w LEFT JOIN settings g ON g.key='source_observation_generation:'||v.source_id
 WHERE w.video_id=v.id AND w.revision=v.revision AND w.cover_bytes>0 AND w.source_generation=COALESCE(g.value,''))`
	cols := strings.Split(videoColumns, ",")
	for i, col := range cols {
		col = strings.TrimSpace(col)
		switch col {
		case "status":
			cols[i] = `CASE WHEN v.status='ready' AND NOT ` + current + ` THEN 'pending' ELSE v.status END`
		case "metadata_json":
			cols[i] = `CASE WHEN v.status='ready' AND NOT ` + current + ` THEN '{}' ELSE v.metadata_json END`
		default:
			cols[i] = "v." + col
		}
	}
	return strings.Join(cols, ",")
}

// ListVisible returns at most limit eligible records; no filesystem I/O occurs.
func (v *Videos) ListVisible(ctx context.Context, scopes []VideoScope, before *VideoPosition, limit int) ([]domain.Video, error) {
	if limit < 1 || limit > 101 {
		return nil, fmt.Errorf("store: invalid video page size")
	}
	where, args := videoScopeWhere(scopes)
	if before != nil {
		where += ` AND (v.first_seen_at<? OR (v.first_seen_at=? AND v.id<?))`
		args = append(args, FormatTime(before.Time), FormatTime(before.Time), before.ID)
	}
	args = append(args, limit)
	rows, err := v.ex.QueryContext(ctx, `SELECT `+visibleVideoColumns()+` FROM videos v JOIN data_sources s ON s.id=v.source_id WHERE `+where+` ORDER BY v.first_seen_at DESC,v.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []domain.Video{}
	for rows.Next() {
		item, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// GetVisible applies the same current scope and exclusion rules as listing.
func (v *Videos) GetVisible(ctx context.Context, scopes []VideoScope, id string) (*domain.Video, error) {
	where, args := videoScopeWhere(scopes)
	args = append(args, id)
	return videoResult(v.ex.QueryRowContext(ctx, `SELECT `+visibleVideoColumns()+` FROM videos v JOIN data_sources s ON s.id=v.source_id WHERE `+where+` AND v.id=?`, args...))
}

// VideoCover is a published cache reference, never a source path.
type VideoCover struct {
	Token           string
	Revision, Bytes int64
	Width, Height   int
}

// GetCover only returns covers published under the current source authority.
func (v *Videos) GetCover(ctx context.Context, scopes []VideoScope, id string) (*VideoCover, error) {
	where, args := videoScopeWhere(scopes)
	args = append(args, id)
	var c VideoCover
	err := v.ex.QueryRowContext(ctx, `SELECT w.token,w.revision,w.cover_bytes,w.cover_width,w.cover_height
 FROM videos v JOIN data_sources s ON s.id=v.source_id JOIN video_work w ON w.video_id=v.id
 LEFT JOIN settings g ON g.key='source_observation_generation:'||s.id
 WHERE `+where+` AND v.id=? AND v.status='ready' AND w.revision=v.revision AND w.cover_bytes>0
 AND w.source_generation=COALESCE(g.value,'')`, args...).Scan(&c.Token, &c.Revision, &c.Bytes, &c.Width, &c.Height)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
