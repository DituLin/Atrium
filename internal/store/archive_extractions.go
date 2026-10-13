package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/domain"
)

// ArchiveExtraction identifies a fenced extraction and its source revisions.
type ArchiveExtraction struct {
	ID, DocumentID, Fingerprint          string
	DocumentRevision, ResolutionRevision int64
}

func archiveExtractionIn(ctx context.Context, tx *sql.Tx, f ArchiveWorkFence, fingerprint string) (ArchiveExtraction, error) {
	var x ArchiveExtraction
	e := tx.QueryRowContext(ctx, `SELECT id,document_id,fingerprint,document_revision,resolution_revision FROM archive_extractions WHERE household_id=? AND task_id=? AND fingerprint=? ORDER BY rowid DESC LIMIT 1`, f.HouseholdID, f.TaskID, fingerprint).Scan(&x.ID, &x.DocumentID, &x.Fingerprint, &x.DocumentRevision, &x.ResolutionRevision)
	return x, e
}

// BeginArchiveExtraction starts or resumes extraction under the supplied work fence.
func (r *ArchiveTasks) BeginArchiveExtraction(ctx context.Context, f ArchiveWorkFence, fingerprint string) (ArchiveExtraction, *domain.ArchiveTask, error) {
	return r.beginArchiveExtraction(ctx, f, fingerprint, false)
}

// RebaseArchiveExtraction rebases fenced work onto current source and resolution revisions.
func (r *ArchiveTasks) RebaseArchiveExtraction(ctx context.Context, f ArchiveWorkFence, fingerprint string) (ArchiveExtraction, *domain.ArchiveTask, error) {
	return r.beginArchiveExtraction(ctx, f, fingerprint, true)
}
func (r *ArchiveTasks) beginArchiveExtraction(ctx context.Context, f ArchiveWorkFence, fingerprint string, rebase bool) (ArchiveExtraction, *domain.ArchiveTask, error) {
	var out ArchiveExtraction
	var task *domain.ArchiveTask
	if archiveIdentifiers(fingerprint) != nil {
		return out, nil, domain.ErrConflict
	}
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		task, e = r.workIn(ctx, tx, f, now)
		if e != nil {
			return e
		}
		var doc string
		var revision int64
		e = tx.QueryRowContext(ctx, `SELECT d.id,d.document_revision FROM archive_sources s JOIN archive_documents d ON d.household_id=s.household_id AND d.id=s.document_id WHERE s.household_id=? AND s.task_id=?`, f.HouseholdID, f.TaskID).Scan(&doc, &revision)
		if errors.Is(e, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if e != nil {
			return e
		}
		rr, e := archiveResolutionRevisionIn(ctx, tx, f.HouseholdID)
		if e != nil {
			return e
		}
		out, e = archiveExtractionIn(ctx, tx, f, fingerprint)
		if e == nil {
			if out.DocumentRevision == revision && out.ResolutionRevision == rr {
				return nil
			}
			if !rebase {
				return domain.ErrConflict
			}
			e = sql.ErrNoRows
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		out = ArchiveExtraction{ID: domain.NewID(), DocumentID: doc, Fingerprint: fingerprint, DocumentRevision: revision, ResolutionRevision: rr}
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_extractions(household_id,id,task_id,document_id,fingerprint,document_revision,resolution_revision) VALUES(?,?,?,?,?,?,?)`, f.HouseholdID, out.ID, f.TaskID, doc, fingerprint, revision, rr)
		if e != nil {
			return e
		}
		if e = archiveBindProductsIn(ctx, tx, f, fingerprint); e != nil {
			return e
		}
		task.StateVersion++
		task.StateJSON = json.RawMessage(entityJSON(map[string]string{"extraction_id": out.ID, "fingerprint": fingerprint}))
		return archiveSave(ctx, tx, task, "extraction_started", now, "")
	})
	return out, task, e
}
