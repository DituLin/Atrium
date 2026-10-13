package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"strings"
	"time"
)

// ArchiveDocumentForScreen returns the current public document authorized for a screen.
func (r *ArchiveTasks) ArchiveDocumentForScreen(ctx context.Context, screen, hash, h, id string) (archive.ScreenDocument, error) {
	out := archive.ScreenDocument{NASStatus: "pending", Preview: "structured", Coverage: archive.SearchCoverage{Complete: true}}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := r.db.InReadTx(ctx, func(tx *sql.Tx) error {
		if _, e := r.authorizeSearchIn(ctx, tx, archiveSearchActor{ScreenID: screen, ScreenHash: hash}, h); e != nil {
			return e
		}
		var p searchProjection
		var e error
		if strings.HasPrefix(id, "legacy:") {
			p, e = legacySearchProjectionIn(ctx, tx, h, id, true)
		} else {
			var visible, restricted bool
			var mime string
			e = tx.QueryRowContext(ctx, `SELECT d.tv_visible AND NOT d.withdrawn,d.original_restricted,b.mime FROM archive_documents d JOIN archive_blobs b ON b.household_id=d.household_id AND b.sha256=d.blob_sha256 WHERE d.household_id=? AND d.id=?`, h, id).Scan(&visible, &restricted, &mime)
			if errors.Is(e, sql.ErrNoRows) || e == nil && !visible {
				return domain.ErrNotFound
			}
			if e != nil {
				return e
			}
			out.OriginalSaved = true
			if restricted {
				out.Preview = "restricted"
			} else if strings.HasPrefix(mime, "image/") || mime == "application/pdf" {
				out.Preview = "pages"
			} else if mime != "text/plain" && mime != "text/csv" && !strings.Contains(mime, "officedocument") {
				out.Preview = "unsupported"
			}
			p, e = r.searchProjectionIn(ctx, tx, h, id, true)
		}
		if e != nil {
			return e
		}
		out.Processing = archive.ScreenProcessing(p.Item)
		out.TaskSummary = archive.SummarizeScreenTask(p.Item)
		archive.PresentScreenItem(&p.Item)
		if len(p.Item.Evidence) > 25 {
			p.Item.Evidence = p.Item.Evidence[:25]
			p.Item.Truncated = true
		}
		out.Item = p.Item
		out.Coverage.Complete = out.Processing == "ready" && !p.Item.Truncated
		if !out.Coverage.Complete {
			out.Coverage.Reason = "partial_or_pending"
		}
		var epoch, generation int64
		if e = tx.QueryRowContext(ctx, `SELECT epoch,generation FROM archive_search_state WHERE household_id=?`, h).Scan(&epoch, &generation); e != nil {
			return e
		}
		out.Generation = fmt.Sprintf("%d:%d", epoch, generation)
		return nil
	})
	return out, err
}

// ArchiveOriginalForScreen authorizes one exact document revision in the same
// snapshot as the active screen. Regional privacy denies the whole raster.
func (r *ArchiveTasks) ArchiveOriginalForScreen(ctx context.Context, screen, hash, h, id string, revision int64) (domain.ArchiveBlobRef, error) {
	var ref domain.ArchiveBlobRef
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := r.db.InReadTx(ctx, func(tx *sql.Tx) error {
		if _, e := r.authorizeSearchIn(ctx, tx, archiveSearchActor{ScreenID: screen, ScreenHash: hash}, h); e != nil {
			return e
		}
		e := tx.QueryRowContext(ctx, `SELECT b.household_id,b.sha256,b.size_bytes,b.mime,b.storage_version FROM archive_documents d JOIN archive_blobs b ON b.household_id=d.household_id AND b.sha256=d.blob_sha256 WHERE d.household_id=? AND d.id=? AND d.document_revision=? AND d.withdrawn=0 AND d.tv_visible=1 AND d.original_restricted=0`, h, id, revision).Scan(&ref.HouseholdID, &ref.SHA256, &ref.SizeBytes, &ref.MIME, &ref.StorageVersion)
		if errors.Is(e, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return e
	})
	return ref, err
}
