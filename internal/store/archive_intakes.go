package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

func (r *ArchiveTasks) authorizeIntake(ctx context.Context, tx *sql.Tx, actor domain.IntegrationActor, household string, m domain.ArchiveIntakeMetadata, now time.Time) (*domain.IntegrationPrincipal, error) {
	p, err := r.authorizeArchiveClient(ctx, tx, actor, household, "archive.ingest", now)
	if err != nil {
		return nil, err
	}
	// Uniform combination check precedes every source/hash lookup, even replay.
	if m.Intent() == "private" && (!p.Policy.AllowsArchive("archive.read", household) || !p.Policy.AllowsArchive("archive.organize", household)) {
		return nil, domain.Errorf(domain.CodeForbidden, "private intake requires archive.ingest, archive.read and archive.organize")
	}
	return p, nil
}
func (r *ArchiveTasks) IntakePreflight(ctx context.Context, actor domain.IntegrationActor, household string, m domain.ArchiveIntakeMetadata) (int64, error) {
	if err := m.Validate(); err != nil {
		return 0, err
	}
	var version int64
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		p, err := r.authorizeIntake(ctx, tx, actor, household, m, now)
		if err == nil {
			version = p.PolicyVersion
		}
		return err
	})
	return version, err
}
func intakeReceiptIn(ctx context.Context, tx *sql.Tx, household, principal, key string) (*domain.ArchiveIntakeReceipt, string, error) {
	var out domain.ArchiveIntakeReceipt
	var received, metadata string
	err := tx.QueryRowContext(ctx, `SELECT receipt_id,task_id,document_id,size_bytes,sha256,received_at,metadata_json FROM archive_sources WHERE household_id=? AND principal_id=? AND source_key=?`, household, principal, key).Scan(&out.ReceiptID, &out.TaskID, &out.DocumentID, &out.SizeBytes, &out.SHA256, &received, &metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", domain.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	out.OriginalStatus = "saved"
	out.ReceivedAt, err = ParseTime(received)
	return &out, metadata, err
}
func (r *ArchiveTasks) projectIntakeIn(ctx context.Context, tx *sql.Tx, household string, p *domain.IntegrationPrincipal, out *domain.ArchiveIntakeReceipt, now time.Time) error {
	if !p.Policy.AllowsArchive("archive.read", household) {
		out.DocumentID = ""
		out.Task = nil
		return nil
	}
	task, err := archiveTaskIn(ctx, tx, household, out.TaskID)
	if err != nil {
		return err
	}
	task.DelegationErrorCode, err = r.archiveDelegationCode(ctx, tx, task, now)
	if err != nil {
		return err
	}
	out.Task = domain.ProjectArchiveTask(task)
	return nil
}

// AcceptIntake must only receive a BlobStore publication from trusted host code.
// It reauthorizes the exact credential and preflight policy version under the
// writer lock. Source conflict/replay precedes document and visibility writes.
func (r *ArchiveTasks) AcceptIntake(ctx context.Context, actor domain.IntegrationActor, household string, policyVersion int64, m domain.ArchiveIntakeMetadata, ref domain.ArchiveBlobRef) (*domain.ArchiveIntakeReceipt, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if ref.HouseholdID != household {
		return nil, domain.ErrNotFound
	}
	key, err := m.ArchiveSourceIdentity.Key(household, actor.PrincipalID)
	if err != nil {
		return nil, err
	}
	var out *domain.ArchiveIntakeReceipt
	err = r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		p, err := r.authorizeIntake(ctx, tx, actor, household, m, now)
		if err != nil {
			return err
		}
		if policyVersion < 1 || p.PolicyVersion != policyVersion {
			return domain.Errorf(domain.CodeForbidden, "archive policy changed during upload")
		}
		existing, business, err := intakeReceiptIn(ctx, tx, household, p.ID, key)
		if err == nil {
			if existing.SHA256 != ref.SHA256 || existing.SizeBytes != ref.SizeBytes || business != string(m.BusinessJSON()) {
				return domain.Errorf(domain.CodeIdempotencyConflict, "source already accepted different content or metadata")
			}
			out = existing
			return r.projectIntakeIn(ctx, tx, household, p, out, now)
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if _, err = archiveBlobEnsureIn(ctx, tx, ref, now); err != nil {
			return err
		}
		var documentID string
		visible := true
		var visibilityVersion int64 = 1
		newDocument := false
		err = sql.ErrNoRows
		if m.Kind == domain.ArchiveInputFile {
			err = tx.QueryRowContext(ctx, `SELECT id,tv_visible,visibility_version FROM archive_documents WHERE household_id=? AND blob_sha256=? AND input_kind='file' ORDER BY created_at,id LIMIT 1`, household, ref.SHA256).Scan(&documentID, &visible, &visibilityVersion)
		}
		if errors.Is(err, sql.ErrNoRows) {
			newDocument = true
			visible = m.Intent() != "private"
			doc, e := archiveDocumentCreateIn(ctx, tx, household, ref.SHA256, m.Kind, &visible, now)
			if e != nil {
				return e
			}
			documentID = doc.ID
		} else if err != nil {
			return err
		}
		input, _ := json.Marshal(map[string]any{"schema_version": 1, "document_id": documentID})
		task, _, err := r.enqueueInTx(ctx, tx, actor, household, "document.extract", key, input, now)
		if err != nil {
			return err
		}
		out = &domain.ArchiveIntakeReceipt{ReceiptID: domain.NewID(), TaskID: task.ID, DocumentID: documentID, OriginalStatus: "saved", SizeBytes: ref.SizeBytes, SHA256: ref.SHA256, ReceivedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO archive_sources(household_id,source_key,principal_id,receipt_id,document_id,task_id,metadata_json,sha256,size_bytes,received_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, household, key, p.ID, out.ReceiptID, documentID, task.ID, string(m.BusinessJSON()), ref.SHA256, ref.SizeBytes, FormatTime(now))
		if err != nil {
			return err
		}
		if m.Intent() == "private" && (newDocument || visible) {
			var before any
			if !newDocument {
				before = boolInt(visible)
				visibilityVersion++
				if _, err = tx.ExecContext(ctx, `UPDATE archive_documents SET tv_visible=0,visibility_version=?,document_revision=document_revision+1 WHERE household_id=? AND id=?`, visibilityVersion, household, documentID); err != nil {
					return err
				}
				var docRevision int64
				if err = tx.QueryRowContext(ctx, `SELECT document_revision FROM archive_documents WHERE household_id=? AND id=?`, household, documentID).Scan(&docRevision); err != nil {
					return err
				}
				if err = archiveDocumentChangedIn(ctx, tx, household, documentID, docRevision); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO archive_visibility_events(household_id,document_id,version,actor_principal_id,source_key,before_visible,after_visible,intent,created_at) VALUES(?,?,?,?,?,?,0,'private',?)`, household, documentID, visibilityVersion, p.ID, key, before, FormatTime(now)); err != nil {
				return err
			}
		}
		return r.projectIntakeIn(ctx, tx, household, p, out, now)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (r *ArchiveTasks) LookupIntake(ctx context.Context, actor domain.IntegrationActor, household string, s domain.ArchiveSourceIdentity) (*domain.ArchiveIntakeReceipt, error) {
	key, err := s.Key(household, actor.PrincipalID)
	if err != nil {
		return nil, err
	}
	var out *domain.ArchiveIntakeReceipt
	err = r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		p, err := r.authorizeArchiveClient(ctx, tx, actor, household, "archive.ingest", now)
		if err != nil {
			return err
		}
		out, _, err = intakeReceiptIn(ctx, tx, household, p.ID, key)
		if err != nil {
			return err
		}
		return r.projectIntakeIn(ctx, tx, household, p, out, now)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetIntakeTask keeps ordinary Get's current read authorization while requiring
// a committed original/source receipt before projecting saved/pending status.
func (r *ArchiveTasks) GetIntakeTask(ctx context.Context, actor domain.IntegrationActor, household, id string) (*domain.ArchiveTask, error) {
	if err := archiveIdentifiers(household, id); err != nil {
		return nil, err
	}
	var out *domain.ArchiveTask
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		if _, err = r.authorizeArchiveClient(ctx, tx, actor, household, "archive.read", now); err != nil {
			return err
		}
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_sources WHERE household_id=? AND task_id=?`, household, id).Scan(&exists); err != nil {
			return err
		}
		if exists != 1 {
			return domain.ErrNotFound
		}
		out, err = archiveTaskIn(ctx, tx, household, id)
		if err != nil {
			return err
		}
		out.DelegationErrorCode, err = r.archiveDelegationCode(ctx, tx, out, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
