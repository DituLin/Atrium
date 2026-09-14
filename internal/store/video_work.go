package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// VideoTask binds one video revision to an expiring claim and source authority.
type VideoTask struct {
	Video    domain.Video
	Source   ObservationLease
	Token    string
	Attempts int
}

// VideoWork owns persistent claims and atomic metadata/cover publication.
type VideoWork struct{ db *DB }

// Valid rechecks a claim before source I/O; publication checks it again.
func (w *VideoWork) Valid(ctx context.Context, t VideoTask, now time.Time) (bool, error) {
	var valid bool
	err := w.db.InTx(ctx, func(tx *sql.Tx) error { var err error; valid, err = w.current(ctx, tx, t, now); return err })
	return valid, err
}

// RetainedTokens protects both current covers and in-progress cache writes.
func (w *VideoWork) RetainedTokens(ctx context.Context, now time.Time) (map[string]bool, error) {
	rows, err := w.db.sql.QueryContext(ctx, `SELECT w.token FROM video_work w JOIN videos v ON v.id=w.video_id
 JOIN data_sources s ON s.id=v.source_id LEFT JOIN settings g ON g.key='source_observation_generation:'||s.id
 WHERE w.revision=v.revision AND s.status='active' AND COALESCE(g.value,'')=w.source_generation
 AND v.status IN ('pending','ready') AND (w.lease_until>? OR (v.status='ready' AND w.cover_bytes>0)) AND `+videoNotExcluded, FormatTime(now))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		out[token] = true
	}
	return out, rows.Err()
}

// VideoWork returns the video work repository.
func (d *DB) VideoWork() *VideoWork { return &VideoWork{db: d} }

// videoNotExcluded enforces rules even before the next scanner pass.
const videoNotExcluded = `NOT EXISTS (SELECT 1 FROM photo_exclusions e WHERE e.source_id=v.source_id
 AND length(trim(e.pattern,'/'))>0 AND (v.rel_path=trim(e.pattern,'/') OR
 (e.match_kind='prefix' AND substr(v.rel_path,1,length(trim(e.pattern,'/'))+1)=trim(e.pattern,'/')||'/')))`

// Claim returns nil when no eligible work is due. It serializes selection and
// lease replacement so workers and process restarts cannot share a claim.
func (w *VideoWork) Claim(ctx context.Context, now time.Time, ttl time.Duration) (*VideoTask, error) {
	if ttl <= 0 || ttl > 10*time.Minute {
		return nil, fmt.Errorf("store: invalid video lease duration")
	}
	var task *VideoTask
	err := w.db.InTx(ctx, func(tx *sql.Tx) error {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT v.id FROM videos v JOIN data_sources s ON s.id=v.source_id
 LEFT JOIN settings g ON g.key='source_observation_generation:'||s.id
 LEFT JOIN video_work w ON w.video_id=v.id
 WHERE v.status IN ('pending','ready') AND s.status='active' AND s.health='online'
 AND (w.video_id IS NULL OR w.revision<>v.revision OR w.source_generation<>COALESCE(g.value,'') OR w.cover_bytes=0)
 AND (w.video_id IS NULL OR w.revision<>v.revision OR w.source_generation<>COALESCE(g.value,'') OR (w.lease_until<=? AND w.next_run_at<=?))
 AND `+videoNotExcluded+` ORDER BY v.first_seen_at,v.id LIMIT 1`, FormatTime(now), FormatTime(now)).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		video, err := w.db.Videos().WithTx(tx).Get(ctx, id)
		if err != nil {
			return err
		}
		sources := &Sources{db: w.db, ex: tx}
		src, err := sources.Get(ctx, video.SourceID)
		if err != nil {
			return err
		}
		generation, err := sources.observationGeneration(ctx, src.ID)
		if err != nil {
			return err
		}
		token := domain.NewID()
		var attempts int
		err = tx.QueryRowContext(ctx, `INSERT INTO video_work(video_id,revision,source_generation,token,lease_until,next_run_at,attempts)
 VALUES(?,?,?,?,?,?,1) ON CONFLICT(video_id) DO UPDATE SET
 revision=excluded.revision,source_generation=excluded.source_generation,token=excluded.token,
 lease_until=excluded.lease_until,next_run_at=excluded.next_run_at,
 attempts=CASE WHEN video_work.revision=excluded.revision AND video_work.source_generation=excluded.source_generation THEN video_work.attempts+1 ELSE 1 END,
 error_code='',cover_bytes=0,cover_width=0,cover_height=0 RETURNING attempts`, id, video.Revision, generation, token, FormatTime(now.Add(ttl)), FormatTime(now)).Scan(&attempts)
		if err != nil {
			return err
		}
		task = &VideoTask{Video: *video, Source: ObservationLease{Source: *src, generation: generation}, Token: token, Attempts: attempts}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: claim video work: %w", err)
	}
	return task, nil
}

func (w *VideoWork) current(ctx context.Context, tx *sql.Tx, t VideoTask, now time.Time) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM video_work w JOIN videos v ON v.id=w.video_id
 JOIN data_sources s ON s.id=v.source_id LEFT JOIN settings g ON g.key='source_observation_generation:'||s.id
 WHERE v.id=? AND v.revision=? AND w.revision=v.revision AND w.token=? AND w.lease_until>?
 AND w.cover_bytes=0 AND v.status IN ('pending','ready') AND s.status='active' AND s.health='online'
 AND s.id=? AND s.root_path=? AND w.source_generation=? AND COALESCE(g.value,'')=w.source_generation AND `+videoNotExcluded,
		t.Video.ID, t.Video.Revision, t.Token, FormatTime(now), t.Source.Source.ID, t.Source.Source.RootPath, t.Source.generation).Scan(&n)
	return n == 1, err
}

// Publish commits a validated JPEG reference and metadata together. The cache
// object is keyed by Token; a false result requires the caller to discard it.
func (w *VideoWork) Publish(ctx context.Context, t VideoTask, m domain.VideoMetadata, bytes int64, width, height int, now time.Time) (bool, error) {
	if bytes <= 0 || bytes > 2*1024*1024 || width <= 0 || height <= 0 || width > 640 || height > 640 {
		return false, fmt.Errorf("store: invalid video cover")
	}
	published := false
	err := w.db.InTx(ctx, func(tx *sql.Tx) error {
		ok, err := w.current(ctx, tx, t, now)
		if err != nil || !ok {
			return err
		}
		ok, err = w.db.Videos().WithTx(tx).SetMetadata(ctx, t.Video.ID, t.Video.Revision, m, now)
		if err != nil || !ok {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE video_work SET cover_bytes=?,cover_width=?,cover_height=?,lease_until=?,error_code='' WHERE video_id=? AND token=?`, bytes, width, height, FormatTime(now), t.Video.ID, t.Token)
		published = err == nil
		return err
	})
	if err != nil {
		return false, fmt.Errorf("store: publish video work: %w", err)
	}
	return published, nil
}

// Retry records a path-free failure code and a bounded retry time. A stale task
// cannot delay the replacement revision or authorization's work.
func (w *VideoWork) Retry(ctx context.Context, t VideoTask, code string, next, now time.Time) (bool, error) {
	if code == "" || len(code) > 64 || next.Before(now) || next.After(now.Add(time.Hour)) {
		return false, fmt.Errorf("store: invalid video retry")
	}
	for _, r := range code {
		if (r < 'a' || r > 'z') && r != '_' {
			return false, fmt.Errorf("store: invalid video retry code")
		}
	}
	updated := false
	err := w.db.InTx(ctx, func(tx *sql.Tx) error {
		ok, err := w.current(ctx, tx, t, now)
		if err != nil || !ok {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE video_work SET error_code=?,next_run_at=?,lease_until=? WHERE video_id=? AND token=?`, code, FormatTime(next), FormatTime(now), t.Video.ID, t.Token)
		updated = err == nil
		return err
	})
	if err != nil {
		return false, fmt.Errorf("store: retry video work: %w", err)
	}
	return updated, nil
}

// ForgetCover clears only the observed published token. A replacement claim or
// publication is untouched; active claims have no published bytes to clear.
func (w *VideoWork) ForgetCover(ctx context.Context, token string, now time.Time) (bool, error) {
	result, err := w.db.sql.ExecContext(ctx, `UPDATE video_work SET cover_bytes=0,cover_width=0,cover_height=0,
 next_run_at=?,error_code='cover_missing' WHERE token=? AND cover_bytes>0 AND lease_until<=?`, FormatTime(now), token, FormatTime(now))
	if err != nil {
		return false, fmt.Errorf("store: forget video cover: %w", err)
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
