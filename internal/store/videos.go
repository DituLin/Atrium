package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// Videos is the internal video index. Callers must enforce source identity and
// authorization before reading files or exposing records to a screen.
type Videos struct {
	db *DB
	ex execer
}

// Videos returns the internal video repository.
func (d *DB) Videos() *Videos { return &Videos{db: d, ex: d.sql} }

// WithTx binds video writes to the caller-owned scan transaction.
func (v *Videos) WithTx(tx *sql.Tx) *Videos { return &Videos{db: v.db, ex: tx} }

const videoColumns = `id,source_id,rel_path,ext,size_bytes,mtime_unix,revision,status,metadata_json,
 last_seen_generation,missing_generations,first_seen_at,last_seen_at,updated_at`

// Observe atomically reconciles a stable file. It never restores exclusions.
// Changed or revived files get a new revision so stale workers cannot publish.
func (v *Videos) Observe(ctx context.Context, o domain.VideoObservation, now time.Time) (*domain.Video, error) {
	ext := domain.NormalizeExt(path.Ext(o.RelPath))
	if o.SourceID == "" || !fs.ValidPath(o.RelPath) || strings.ContainsAny(o.RelPath, "\\\x00") ||
		(ext != "mp4" && ext != "mov") || o.SizeBytes < 0 || o.Generation < 1 {
		return nil, fmt.Errorf("store: invalid video observation")
	}
	changed := `(videos.status != 'excluded' AND (videos.size_bytes != excluded.size_bytes OR videos.mtime_unix != excluded.mtime_unix OR videos.status = 'removed'))`
	q := `INSERT INTO videos (id,source_id,rel_path,ext,size_bytes,mtime_unix,last_seen_generation,first_seen_at,last_seen_at,updated_at)
 SELECT ?,?,?,?,?,?,?,?,?,? WHERE ? > COALESCE((SELECT completed_generation FROM video_scan_state WHERE source_id=?),0)
 ON CONFLICT(source_id,rel_path) DO UPDATE SET
 size_bytes=CASE WHEN videos.status='excluded' THEN videos.size_bytes ELSE excluded.size_bytes END,
 mtime_unix=CASE WHEN videos.status='excluded' THEN videos.mtime_unix ELSE excluded.mtime_unix END,
 revision=videos.revision+CASE WHEN ` + changed + ` THEN 1 ELSE 0 END,
 status=CASE WHEN ` + changed + ` THEN 'pending' ELSE videos.status END,
 metadata_json=CASE WHEN ` + changed + ` THEN '{}' ELSE videos.metadata_json END,
 first_seen_at=CASE WHEN videos.status='removed' THEN excluded.first_seen_at ELSE videos.first_seen_at END,
 last_seen_generation=excluded.last_seen_generation,missing_generations=0,
 last_seen_at=excluded.last_seen_at,updated_at=excluded.updated_at
 WHERE excluded.last_seen_generation>=videos.last_seen_generation AND excluded.last_seen_generation>videos.last_missing_generation
 RETURNING ` + videoColumns
	ts := FormatTime(now)
	result, err := scanVideo(v.ex.QueryRowContext(ctx, q, domain.NewID(), o.SourceID, o.RelPath, ext, o.SizeBytes, o.MtimeUnix, o.Generation, ts, ts, ts, o.Generation, o.SourceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("store: observe video: %w", err)
	}
	return result, nil
}

// Get returns an internal video record by ID without screen authorization.
func (v *Videos) Get(ctx context.Context, id string) (*domain.Video, error) {
	return videoResult(v.ex.QueryRowContext(ctx, `SELECT `+videoColumns+` FROM videos WHERE id=?`, id))
}

// GetByPath returns a video record for scanner reconciliation.
func (v *Videos) GetByPath(ctx context.Context, sourceID, rel string) (*domain.Video, error) {
	return videoResult(v.ex.QueryRowContext(ctx, `SELECT `+videoColumns+` FROM videos WHERE source_id=? AND rel_path=?`, sourceID, rel))
}

// SetMetadata returns false when the task revision has become stale or revoked.
// Ready describes a successful probe, not a promise about all TV decoders.
func (v *Videos) SetMetadata(ctx context.Context, id string, revision int64, m domain.VideoMetadata, now time.Time) (bool, error) {
	if m.Width <= 0 || m.Height <= 0 || m.DurationMS <= 0 || m.VideoCodec == "" || m.Container == "" {
		return false, fmt.Errorf("store: invalid video metadata")
	}
	data, err := json.Marshal(m)
	if err != nil {
		return false, fmt.Errorf("store: encode video metadata: %w", err)
	}
	res, err := v.ex.ExecContext(ctx, `UPDATE videos SET metadata_json=?,status='ready',updated_at=?
 WHERE id=? AND revision=? AND status IN ('pending','ready')
 AND EXISTS (SELECT 1 FROM data_sources s WHERE s.id=videos.source_id AND s.status='active')`, string(data), FormatTime(now), id, revision)
	if err != nil {
		return false, fmt.Errorf("store: publish video metadata: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: video metadata affected: %w", err)
	}
	return n == 1, nil
}

// CompleteScan may only be called after a complete listing with confirmed source
// identity. Repeating a generation is idempotent; aborted scans must not call it.
func (v *Videos) CompleteScan(ctx context.Context, sourceID string, generation int64, now time.Time) error {
	if generation < 1 {
		return fmt.Errorf("store: invalid video scan generation")
	}
	if _, inTx := v.ex.(*sql.Tx); !inTx {
		return v.db.InTx(ctx, func(tx *sql.Tx) error { return v.WithTx(tx).CompleteScan(ctx, sourceID, generation, now) })
	}
	res, err := v.ex.ExecContext(ctx, `INSERT INTO video_scan_state(source_id,completed_generation) VALUES(?,?)
 ON CONFLICT(source_id) DO UPDATE SET completed_generation=excluded.completed_generation
 WHERE excluded.completed_generation>video_scan_state.completed_generation`, sourceID, generation)
	if err != nil {
		return fmt.Errorf("store: video scan fence: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: video scan fence affected: %w", err)
	}
	if n == 0 {
		return nil
	}
	_, err = v.ex.ExecContext(ctx, `UPDATE videos SET missing_generations=missing_generations+1,
 last_missing_generation=?,status=CASE WHEN missing_generations+1>=? THEN 'removed' ELSE status END,
 updated_at=? WHERE source_id=? AND last_seen_generation<? AND last_missing_generation<?
 AND status IN ('pending','ready','unsupported')`, generation, RemovalThreshold, FormatTime(now), sourceID, generation, generation)
	if err != nil {
		return fmt.Errorf("store: finalize video scan: %w", err)
	}
	return nil
}

// ExcludeMatching applies the existing path exclusion semantics to videos.
func (v *Videos) ExcludeMatching(ctx context.Context, sourceID string, kind domain.MatchKind, pattern string, now time.Time) error {
	q := `UPDATE videos SET status='excluded',metadata_json='{}',revision=revision+1,updated_at=? WHERE source_id=? AND status!='excluded' AND `
	args := []any{FormatTime(now), sourceID}
	switch kind {
	case domain.MatchPrefix:
		q += `(rel_path=? OR rel_path LIKE ? ESCAPE '\')`
		args = append(args, trimSlash(pattern), escapeLike(trimSlash(pattern))+`/%`)
	case domain.MatchPath:
		q += `rel_path=?`
		args = append(args, pattern)
	default:
		return fmt.Errorf("store: invalid video exclusion kind")
	}
	if _, err := v.ex.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("store: exclude videos: %w", err)
	}
	return nil
}

func videoResult(row scanner) (*domain.Video, error) {
	v, err := scanVideo(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: read video: %w", err)
	}
	return v, nil
}

func scanVideo(row scanner) (*domain.Video, error) {
	var v domain.Video
	var metadata, first, last, updated string
	err := row.Scan(&v.ID, &v.SourceID, &v.RelPath, &v.Ext, &v.SizeBytes, &v.MtimeUnix, &v.Revision, &v.Status, &metadata,
		&v.LastSeenGeneration, &v.MissingGenerations, &first, &last, &updated)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(metadata), &v.Metadata); err != nil {
		return nil, err
	}
	v.FirstSeenAt = timeVal(first)
	v.LastSeenAt = timeVal(last)
	v.UpdatedAt = timeVal(updated)
	return &v, nil
}
