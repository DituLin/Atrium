package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// ScanRuns is the scan_runs repository.
type ScanRuns struct{ db *DB }

// ScanRuns returns the scan run repository.
func (d *DB) ScanRuns() *ScanRuns { return &ScanRuns{db: d} }

const scanRunColumns = `id, source_id, mode, status, started_at, finished_at, files_seen, files_new,
	files_changed, files_missing, files_removed, files_unsupported, errors, note`

// Start records the beginning of a scan.
func (s *ScanRuns) Start(ctx context.Context, run *domain.ScanRun) error {
	if run.ID == "" {
		run.ID = domain.NewID()
	}
	if run.Status == "" {
		run.Status = domain.ScanRunning
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	_, err := s.db.sql.ExecContext(ctx,
		`INSERT INTO scan_runs (id, source_id, mode, status, started_at) VALUES (?, ?, ?, ?, ?)`,
		run.ID, run.SourceID, string(run.Mode), string(run.Status), FormatTime(run.StartedAt))
	if err != nil {
		return fmt.Errorf("store: start scan run: %w", err)
	}
	return nil
}

// UpdateCounters writes progress counters for a running scan.
func (s *ScanRuns) UpdateCounters(ctx context.Context, run *domain.ScanRun) error {
	_, err := s.db.sql.ExecContext(ctx, `
		UPDATE scan_runs SET files_seen = ?, files_new = ?, files_changed = ?, files_missing = ?,
			files_removed = ?, files_unsupported = ?, errors = ? WHERE id = ?`,
		run.FilesSeen, run.FilesNew, run.FilesChanged, run.FilesMissing,
		run.FilesRemoved, run.FilesUnsupported, run.Errors, run.ID)
	if err != nil {
		return fmt.Errorf("store: update scan counters: %w", err)
	}
	return nil
}

// Finish closes a scan run with a terminal status.
func (s *ScanRuns) Finish(ctx context.Context, id string, status domain.ScanStatus, note string, at time.Time) error {
	res, err := s.db.sql.ExecContext(ctx,
		`UPDATE scan_runs SET status = ?, finished_at = ?, note = ? WHERE id = ?`,
		string(status), FormatTime(at), nullString(note), id)
	if err != nil {
		return fmt.Errorf("store: finish scan run %q: %w", id, err)
	}
	return affectedOne(res, id)
}

// Get returns one scan run.
func (s *ScanRuns) Get(ctx context.Context, id string) (*domain.ScanRun, error) {
	row := s.db.sql.QueryRowContext(ctx, `SELECT `+scanRunColumns+` FROM scan_runs WHERE id = ?`, id)
	run, err := scanScanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get scan run %q: %w", id, err)
	}
	return run, nil
}

// List returns scan runs newest first, optionally for one source.
func (s *ScanRuns) List(ctx context.Context, sourceID string, limit int) ([]domain.ScanRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + scanRunColumns + ` FROM scan_runs`
	var args []any
	if sourceID != "" {
		q += ` WHERE source_id = ?`
		args = append(args, sourceID)
	}
	q += ` ORDER BY started_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list scan runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ScanRun
	for rows.Next() {
		run, err := scanScanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan scan_run: %w", err)
		}
		out = append(out, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate scan runs: %w", err)
	}
	return out, nil
}

func scanScanRun(sc scanner) (*domain.ScanRun, error) {
	var (
		run              domain.ScanRun
		mode, status     string
		startedAt        string
		finishedAt, note sql.NullString
	)
	if err := sc.Scan(&run.ID, &run.SourceID, &mode, &status, &startedAt, &finishedAt,
		&run.FilesSeen, &run.FilesNew, &run.FilesChanged, &run.FilesMissing,
		&run.FilesRemoved, &run.FilesUnsupported, &run.Errors, &note); err != nil {
		return nil, err
	}
	run.Mode = domain.ScanMode(mode)
	run.Status = domain.ScanStatus(status)
	run.StartedAt = timeVal(startedAt)
	run.FinishedAt = timePtr(finishedAt)
	run.Note = note.String
	return &run, nil
}

// DeleteOlderThan removes scan runs past the retention window (design §5).
func (s *ScanRuns) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.sql.ExecContext(ctx,
		`DELETE FROM scan_runs WHERE started_at < ?`, FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("store: prune scan runs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}
