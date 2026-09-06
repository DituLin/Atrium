package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// Previews is the preview_files repository.
type Previews struct {
	db *DB
	// ex is the database or the caller's batch transaction.
	ex execer
}

// Previews returns the preview file repository.
func (d *DB) Previews() *Previews { return &Previews{db: d, ex: d.sql} }

// Put upserts a cached derived image row.
func (p *Previews) Put(ctx context.Context, f *domain.PreviewFile) error {
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now()
	}
	if f.LastAccessAt.IsZero() {
		f.LastAccessAt = f.CreatedAt
	}
	_, err := p.ex.ExecContext(ctx, `
		INSERT INTO preview_files (photo_id, variant, rel_path, bytes, width, height, fingerprint, created_at, last_access_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(photo_id, variant) DO UPDATE SET
			rel_path = excluded.rel_path, bytes = excluded.bytes, width = excluded.width,
			height = excluded.height, fingerprint = excluded.fingerprint,
			created_at = excluded.created_at, last_access_at = excluded.last_access_at`,
		f.PhotoID, string(f.Variant), f.RelPath, f.Bytes, f.Width, f.Height, f.Fingerprint,
		FormatTime(f.CreatedAt), FormatTime(f.LastAccessAt))
	if err != nil {
		return fmt.Errorf("store: put preview file: %w", err)
	}
	return nil
}

// Get returns one preview file row.
func (p *Previews) Get(ctx context.Context, photoID string, variant domain.Variant) (*domain.PreviewFile, error) {
	row := p.ex.QueryRowContext(ctx, `
		SELECT photo_id, variant, rel_path, bytes, width, height, fingerprint, created_at, last_access_at
		FROM preview_files WHERE photo_id = ? AND variant = ?`, photoID, string(variant))
	var (
		f                   domain.PreviewFile
		variantStr          string
		createdAt, accessAt string
	)
	err := row.Scan(&f.PhotoID, &variantStr, &f.RelPath, &f.Bytes, &f.Width, &f.Height, &f.Fingerprint, &createdAt, &accessAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get preview file: %w", err)
	}
	f.Variant = domain.Variant(variantStr)
	f.CreatedAt = timeVal(createdAt)
	f.LastAccessAt = timeVal(accessAt)
	return &f, nil
}

// Touch updates the LRU access timestamp.
func (p *Previews) Touch(ctx context.Context, photoID string, variant domain.Variant, now time.Time) error {
	_, err := p.ex.ExecContext(ctx,
		`UPDATE preview_files SET last_access_at = ? WHERE photo_id = ? AND variant = ?`,
		FormatTime(now), photoID, string(variant))
	if err != nil {
		return fmt.Errorf("store: touch preview file: %w", err)
	}
	return nil
}

// Delete removes one preview file row.
func (p *Previews) Delete(ctx context.Context, photoID string, variant domain.Variant) error {
	_, err := p.ex.ExecContext(ctx,
		`DELETE FROM preview_files WHERE photo_id = ? AND variant = ?`, photoID, string(variant))
	if err != nil {
		return fmt.Errorf("store: delete preview file: %w", err)
	}
	return nil
}

// DeleteForPhoto removes every variant of a photo.
func (p *Previews) DeleteForPhoto(ctx context.Context, photoID string) error {
	_, err := p.ex.ExecContext(ctx, `DELETE FROM preview_files WHERE photo_id = ?`, photoID)
	if err != nil {
		return fmt.Errorf("store: delete preview files: %w", err)
	}
	return nil
}

// TotalBytes sums the cached preview sizes.
func (p *Previews) TotalBytes(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	if err := p.ex.QueryRowContext(ctx, `SELECT SUM(bytes) FROM preview_files`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: sum preview bytes: %w", err)
	}
	return n.Int64, nil
}

// LRU returns the least recently accessed preview files, oldest first.
func (p *Previews) LRU(ctx context.Context, limit int) ([]domain.PreviewFile, error) {
	rows, err := p.ex.QueryContext(ctx, `
		SELECT photo_id, variant, rel_path, bytes, width, height, fingerprint, created_at, last_access_at
		FROM preview_files ORDER BY last_access_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list preview LRU: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.PreviewFile
	for rows.Next() {
		var (
			f                   domain.PreviewFile
			variantStr          string
			createdAt, accessAt string
		)
		if err := rows.Scan(&f.PhotoID, &variantStr, &f.RelPath, &f.Bytes, &f.Width, &f.Height,
			&f.Fingerprint, &createdAt, &accessAt); err != nil {
			return nil, fmt.Errorf("store: scan preview file: %w", err)
		}
		f.Variant = domain.Variant(variantStr)
		f.CreatedAt = timeVal(createdAt)
		f.LastAccessAt = timeVal(accessAt)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate preview files: %w", err)
	}
	return out, nil
}
