package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// archiveBlobEnsureIn is metadata-only, not publication or authorization. The
// intake service must first finish BlobStore.Put outside the DB writer lock,
// then reauthorize and call these helpers with source/task writes in one tx.
// Keeping these helpers package-private prevents a standalone registration API
// from presenting arbitrary metadata as successfully accepted original bytes.
func archiveBlobEnsureIn(ctx context.Context, tx *sql.Tx, ref domain.ArchiveBlobRef, now time.Time) (*domain.ArchiveBlob, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if err := archiveTime(now); err != nil {
		return nil, err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO archive_blobs(household_id,sha256,size_bytes,mime,storage_version,created_at)
	 VALUES(?,?,?,?,?,?) ON CONFLICT(household_id,sha256) DO NOTHING`, ref.HouseholdID, ref.SHA256, ref.SizeBytes, ref.MIME, ref.StorageVersion, FormatTime(now))
	if err != nil {
		return nil, err
	}
	var out domain.ArchiveBlob
	var created string
	err = tx.QueryRowContext(ctx, `SELECT household_id,sha256,size_bytes,mime,storage_version,created_at FROM archive_blobs WHERE household_id=? AND sha256=?`, ref.HouseholdID, ref.SHA256).Scan(&out.HouseholdID, &out.SHA256, &out.SizeBytes, &out.MIME, &out.StorageVersion, &created)
	if err != nil {
		return nil, err
	}
	if out.ArchiveBlobRef != ref {
		return nil, domain.ErrConflict
	}
	out.CreatedAt, err = ParseTime(created)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// archiveDocumentCreateIn creates a new logical identity without content-based
// dedupe. T04 owns file/source replay rules in its acceptance transaction. Nil
// tvVisible uses the user's TV-visible default; explicit false means private.
// Returned metadata is provisional until the caller's transaction commits.
func archiveDocumentCreateIn(ctx context.Context, tx *sql.Tx, household, hash string, inputKind domain.ArchiveInputKind, tvVisible *bool, now time.Time) (*domain.ArchiveDocument, error) {
	if err := domain.ValidateArchiveIdentifier(household); err != nil {
		return nil, err
	}
	if err := domain.ValidateArchiveSHA256(hash); err != nil {
		return nil, err
	}
	if err := archiveTime(now); err != nil {
		return nil, err
	}
	if inputKind != domain.ArchiveInputFile && inputKind != domain.ArchiveInputText {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "invalid archive input kind")
	}
	visible := true
	if tvVisible != nil {
		visible = *tvVisible
	}
	out := &domain.ArchiveDocument{ID: domain.NewID(), HouseholdID: household, BlobSHA256: hash, InputKind: inputKind, TVVisible: visible, CreatedAt: now.UTC()}
	_, err := tx.ExecContext(ctx, `INSERT INTO archive_documents(id,household_id,blob_sha256,input_kind,tv_visible,created_at) VALUES(?,?,?,?,?,?)`, out.ID, household, hash, inputKind, boolInt(visible), FormatTime(now))
	if err != nil {
		return nil, err
	}
	return out, nil
}
