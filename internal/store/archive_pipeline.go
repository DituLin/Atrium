package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/domain"
	"time"
)

// ArchiveWorkFence is an internal worker capability, never a client response.
type ArchiveWorkFence struct {
	HouseholdID, TaskID, Token string
	StateVersion               int64
}

// ArchiveProduct contains one checkpoint product bound to an extraction fingerprint.
type ArchiveProduct struct {
	Key, Fingerprint, Digest string
	Body                     json.RawMessage
	InvocationID             string
}

// ArchiveInvocation records an immutable operation budget reservation.
type ArchiveInvocation struct {
	ID, Operation string
	Reserved      int64
}

// MaxArchiveProductBytes bounds a durable archive checkpoint product.
const MaxArchiveProductBytes = 32 << 20

func productDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (r *ArchiveTasks) workIn(ctx context.Context, tx *sql.Tx, f ArchiveWorkFence, now time.Time) (*domain.ArchiveTask, error) {
	t, e := archiveTaskIn(ctx, tx, f.HouseholdID, f.TaskID)
	if e != nil {
		return nil, e
	}
	if t.Status != domain.ArchiveTaskRunning || t.LeaseToken != f.Token || t.LeaseExpiresAt == nil || !now.Before(*t.LeaseExpiresAt) || now.Before(t.UpdatedAt) || t.StateVersion != f.StateVersion {
		return nil, domain.ErrConflict
	}
	if e = r.authorizeArchiveDelegate(ctx, tx, t, now); e != nil {
		return nil, e
	}
	return t, nil
}
func saveProductIn(ctx context.Context, tx *sql.Tx, f ArchiveWorkFence, p ArchiveProduct) error {
	if archiveIdentifiers(p.Key, p.Fingerprint) != nil || len(p.Body) == 0 || len(p.Body) > MaxArchiveProductBytes || !json.Valid(p.Body) {
		return domain.Errorf(domain.CodeInvalidRequest, "invalid archive pipeline request")
	}
	digest := productDigest(p.Body)
	if p.Digest != "" && p.Digest != digest {
		return domain.ErrConflict
	}
	var prior string
	e := tx.QueryRowContext(ctx, `SELECT digest FROM archive_products WHERE household_id=? AND task_id=? AND product_key=? AND fingerprint=?`, f.HouseholdID, f.TaskID, p.Key, p.Fingerprint).Scan(&prior)
	if e == nil {
		if prior != digest {
			return domain.ErrConflict
		}
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	var count, total int64
	if e = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(length(body)),0) FROM archive_products WHERE household_id=? AND task_id=? AND fingerprint=?`, f.HouseholdID, f.TaskID, p.Fingerprint).Scan(&count, &total); e != nil {
		return e
	}
	if count >= 260 || total+int64(len(p.Body)) > 128<<20 {
		return domain.ErrConflict
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO archive_products VALUES(?,?,?,?,?,?)`, f.HouseholdID, f.TaskID, p.Key, p.Fingerprint, digest, []byte(p.Body))
	return e
}

// SaveProductAndCheckpoint stores a fenced product and advances its task checkpoint atomically.
func (r *ArchiveTasks) SaveProductAndCheckpoint(ctx context.Context, f ArchiveWorkFence, p ArchiveProduct) (*domain.ArchiveTask, error) {
	var out *domain.ArchiveTask
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		t, e := r.workIn(ctx, tx, f, now)
		if e != nil {
			return e
		}
		if e = saveProductIn(ctx, tx, f, p); e != nil {
			return e
		}
		if e = archiveBindProductsIn(ctx, tx, f, p.Fingerprint); e != nil {
			return e
		}
		if p.InvocationID != "" {
			res, e := tx.ExecContext(ctx, `UPDATE archive_invocations SET status='completed',product_key=? WHERE household_id=? AND task_id=? AND id=? AND attempt_id=? AND status='reserved'`, p.Key, f.HouseholdID, f.TaskID, p.InvocationID, t.AttemptID)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return domain.ErrConflict
			}
		}
		state, _ := json.Marshal(map[string]string{"product_key": p.Key, "fingerprint": p.Fingerprint, "digest": productDigest(p.Body)})
		e = applyArchiveUpdate(t, domain.ArchiveTaskUpdate{ExpectedStateVersion: f.StateVersion, Stage: "candidate", StateJSON: state, ResultJSON: json.RawMessage(`{}`)}, domain.ArchiveTaskRunning)
		if e != nil {
			return e
		}
		e = archiveSave(ctx, tx, t, "product_saved", now, "")
		out = t
		return e
	})
	return out, e
}

// ArchiveProducts returns the checkpoint products authorized by the current work fence.
func (r *ArchiveTasks) ArchiveProducts(ctx context.Context, f ArchiveWorkFence, fingerprint string) ([]ArchiveProduct, error) {
	var out []ArchiveProduct
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		if _, e = r.workIn(ctx, tx, f, now); e != nil {
			return e
		}
		// Reuse only a published extraction of this same immutable logical document
		// and full fingerprint. Copy candidates, never delegation/task state/budget.
		_, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO archive_products(household_id,task_id,product_key,fingerprint,digest,body)
  SELECT p.household_id,?,p.product_key,p.fingerprint,p.digest,p.body FROM archive_products p WHERE p.household_id=? AND p.fingerprint=? AND p.task_id=(SELECT prior.task_id FROM archive_extractions current JOIN archive_extractions prior ON prior.household_id=current.household_id AND prior.document_id=current.document_id AND prior.fingerprint=current.fingerprint AND prior.status='published' WHERE current.household_id=? AND current.task_id=? AND current.fingerprint=? ORDER BY prior.rowid DESC LIMIT 1)`, f.TaskID, f.HouseholdID, fingerprint, f.HouseholdID, f.TaskID, fingerprint)
		if e != nil {
			return e
		}
		if e = archiveBindProductsIn(ctx, tx, f, fingerprint); e != nil {
			return e
		}
		var count, total int64
		if e = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(length(body)),0) FROM archive_products WHERE household_id=? AND task_id=? AND fingerprint=?`, f.HouseholdID, f.TaskID, fingerprint).Scan(&count, &total); e != nil {
			return e
		}
		if count > 260 || total > 128<<20 {
			return domain.ErrConflict
		}
		rows, e := tx.QueryContext(ctx, `SELECT product_key,fingerprint,digest,body FROM archive_products WHERE household_id=? AND task_id=? AND fingerprint=? ORDER BY product_key`, f.HouseholdID, f.TaskID, fingerprint)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }() // Read-only query cursor cleanup.
		for rows.Next() {
			var p ArchiveProduct
			if e = rows.Scan(&p.Key, &p.Fingerprint, &p.Digest, &p.Body); e != nil {
				return e
			}
			if p.Digest != productDigest(p.Body) {
				return domain.ErrConflict
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, e
}

// ReserveArchiveInvocation never refunds reservations, including unknown outcomes after a crash.
// The first maximum is immutable; later configurations cannot reset spent work.
func (r *ArchiveTasks) ReserveArchiveInvocation(ctx context.Context, f ArchiveWorkFence, operation string, cost, maximum int64) (ArchiveInvocation, *domain.ArchiveTask, error) {
	var inv ArchiveInvocation
	var out *domain.ArchiveTask
	if archiveIdentifiers(operation) != nil || cost <= 0 || maximum <= 0 || cost > maximum {
		return inv, nil, domain.Errorf(domain.CodeInvalidRequest, "invalid archive pipeline request")
	}
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		t, e := r.workIn(ctx, tx, f, now)
		if e != nil {
			return e
		}
		var calls int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_invocations WHERE household_id=? AND task_id=? AND operation=?`, f.HouseholdID, f.TaskID, operation).Scan(&calls); e != nil {
			return e
		}
		if calls >= 2 {
			return domain.ErrConflict
		}
		_, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO archive_budgets VALUES(?,?,?,0)`, f.HouseholdID, f.TaskID, maximum)
		if e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `UPDATE archive_budgets SET spent=spent+? WHERE household_id=? AND task_id=? AND spent<=maximum-? AND maximum=?`, cost, f.HouseholdID, f.TaskID, cost, maximum)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return domain.ErrConflict
		}
		inv = ArchiveInvocation{ID: domain.NewID(), Operation: operation, Reserved: cost}
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_invocations(household_id,task_id,id,operation,attempt_id,reserved,status) VALUES(?,?,?,?,?,?,'reserved')`, f.HouseholdID, f.TaskID, inv.ID, operation, t.AttemptID, cost)
		if e != nil {
			return e
		}
		t.StateVersion++
		e = archiveSave(ctx, tx, t, "invocation_reserved", now, "")
		out = t
		return e
	})
	return inv, out, e
}

// ArchiveWorkSource returns the immutable original referenced by fenced extraction work.
func (r *ArchiveTasks) ArchiveWorkSource(ctx context.Context, f ArchiveWorkFence) (domain.ArchiveBlobRef, string, error) {
	var ref domain.ArchiveBlobRef
	var hint string
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		if _, e = r.workIn(ctx, tx, f, now); e != nil {
			return e
		}
		return tx.QueryRowContext(ctx, `SELECT b.household_id,b.sha256,b.size_bytes,b.mime,b.storage_version,json_extract(s.metadata_json,'$.original_name') FROM archive_sources s JOIN archive_blobs b ON b.household_id=s.household_id AND b.sha256=s.sha256 WHERE s.household_id=? AND s.task_id=?`, f.HouseholdID, f.TaskID).Scan(&ref.HouseholdID, &ref.SHA256, &ref.SizeBytes, &ref.MIME, &ref.StorageVersion, &hint)
	})
	return ref, hint, e
}

func archiveBindProductsIn(ctx context.Context, tx *sql.Tx, f ArchiveWorkFence, fingerprint string) error {
	_, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO archive_extraction_products SELECT p.household_id,x.id,p.task_id,p.product_key,p.fingerprint,p.digest FROM archive_products p JOIN archive_extractions x ON x.household_id=p.household_id AND x.task_id=p.task_id AND x.fingerprint=p.fingerprint WHERE p.household_id=? AND p.task_id=? AND p.fingerprint=? AND x.id=(SELECT id FROM archive_extractions WHERE household_id=? AND task_id=? AND fingerprint=? ORDER BY rowid DESC LIMIT 1)`, f.HouseholdID, f.TaskID, fingerprint, f.HouseholdID, f.TaskID, fingerprint)
	return e
}
