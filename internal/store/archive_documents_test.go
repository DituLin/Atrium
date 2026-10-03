package store

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func archiveDocumentsDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "archives.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Migrate(context.Background())
	require.NoError(t, err)
	_, err = db.sql.Exec(`INSERT INTO households(id) VALUES('home-1'),('home-2')`)
	require.NoError(t, err)
	return db
}

func archiveTestPublished(t *testing.T) (*archive.BlobStore, domain.ArchiveBlobRef) {
	t.Helper()
	s, err := archive.NewBlobStore(filepath.Join(t.TempDir(), "originals"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	ref, err := s.Put(context.Background(), "home-1", strings.NewReader("喝奶 70ml"), archive.PutOptions{})
	require.NoError(t, err)
	return s, ref
}

var documentsNow = time.Date(2026, 10, 3, 1, 2, 3, 0, time.UTC)

func TestArchiveDocumentsShareBlobButRetainIdentityAndVisibility(t *testing.T) {
	ctx := context.Background()
	db := archiveDocumentsDB(t)
	_, ref := archiveTestPublished(t)
	var first, second *domain.ArchiveDocument
	require.NoError(t, db.InWriteTx(ctx, func(tx *sql.Tx) error {
		blob, err := archiveBlobEnsureIn(ctx, tx, ref, documentsNow)
		if err != nil {
			return err
		}
		require.Equal(t, ref, blob.ArchiveBlobRef)
		require.Equal(t, documentsNow, blob.CreatedAt)
		first, err = archiveDocumentCreateIn(ctx, tx, ref.HouseholdID, ref.SHA256, domain.ArchiveInputText, nil, documentsNow)
		if err != nil {
			return err
		}
		private := false
		second, err = archiveDocumentCreateIn(ctx, tx, ref.HouseholdID, ref.SHA256, domain.ArchiveInputText, &private, documentsNow)
		return err
	}))
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, first.BlobSHA256, second.BlobSHA256)
	require.True(t, first.TVVisible, "unspecified follows the user's TV-visible default")
	require.False(t, second.TVVisible, "explicit private must not collapse into an omitted value")
	var docs, blobs int
	require.NoError(t, db.sql.QueryRow(`SELECT count(*) FROM archive_documents`).Scan(&docs))
	require.Equal(t, 2, docs)
	require.NoError(t, db.sql.QueryRow(`SELECT count(*) FROM archive_blobs`).Scan(&blobs))
	require.Equal(t, 1, blobs)
	var persisted bool
	require.NoError(t, db.sql.QueryRow(`SELECT tv_visible FROM archive_documents WHERE household_id=? AND id=?`, "home-1", second.ID).Scan(&persisted))
	require.False(t, persisted)
}

func TestArchiveBlobMetadataReuseRejectsDriftAndPreservesCreatedTime(t *testing.T) {
	ctx := context.Background()
	db := archiveDocumentsDB(t)
	_, ref := archiveTestPublished(t)
	require.NoError(t, db.InWriteTx(ctx, func(tx *sql.Tx) error { _, err := archiveBlobEnsureIn(ctx, tx, ref, documentsNow); return err }))
	require.NoError(t, db.InWriteTx(ctx, func(tx *sql.Tx) error {
		blob, err := archiveBlobEnsureIn(ctx, tx, ref, documentsNow.Add(time.Hour))
		require.NoError(t, err)
		require.Equal(t, documentsNow, blob.CreatedAt)
		return nil
	}))
	for _, mutate := range []func(*domain.ArchiveBlobRef){func(r *domain.ArchiveBlobRef) { r.SizeBytes++ }, func(r *domain.ArchiveBlobRef) { r.MIME = "image/png" }} {
		bad := ref
		mutate(&bad)
		err := db.InWriteTx(ctx, func(tx *sql.Tx) error { _, err := archiveBlobEnsureIn(ctx, tx, bad, documentsNow); return err })
		require.ErrorIs(t, err, domain.ErrConflict)
	}
	for _, field := range []string{"size_bytes=size_bytes+1", "mime='image/png'", "created_at='2026-01-01T00:00:00Z'", "sha256='" + strings.Repeat("b", 64) + "'", "household_id='home-2'"} {
		_, err := db.sql.Exec(`UPDATE archive_blobs SET ` + field)
		require.Error(t, err, field)
	}
}

func TestArchiveDocumentHouseholdForeignKeyAndImmutableOriginal(t *testing.T) {
	ctx := context.Background()
	db := archiveDocumentsDB(t)
	_, ref := archiveTestPublished(t)
	var doc *domain.ArchiveDocument
	require.NoError(t, db.InWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := archiveBlobEnsureIn(ctx, tx, ref, documentsNow); err != nil {
			return err
		}
		var err error
		doc, err = archiveDocumentCreateIn(ctx, tx, ref.HouseholdID, ref.SHA256, domain.ArchiveInputFile, nil, documentsNow)
		return err
	}))
	err := db.InWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := archiveDocumentCreateIn(ctx, tx, "home-2", ref.SHA256, domain.ArchiveInputFile, nil, documentsNow)
		return err
	})
	require.Error(t, err, "a home-1 hash cannot back a home-2 document")
	_, err = db.sql.Exec(`INSERT INTO archive_documents(id,household_id,blob_sha256,input_kind,tv_visible,created_at) VALUES('cross-home','home-2',?,'file',1,?)`, ref.SHA256, FormatTime(documentsNow))
	require.Error(t, err, "composite FK must enforce household even for raw SQL")
	unknown := ref
	unknown.HouseholdID = "missing-home"
	err = db.InWriteTx(ctx, func(tx *sql.Tx) error { _, err := archiveBlobEnsureIn(ctx, tx, unknown, documentsNow); return err })
	require.Error(t, err)
	for _, field := range []string{"id='changed'", "household_id='home-2'", "blob_sha256='" + strings.Repeat("a", 64) + "'", "input_kind='text'", "created_at='2026-01-01T00:00:00Z'"} {
		_, err := db.sql.Exec(`UPDATE archive_documents SET ` + field)
		require.Error(t, err, field)
	}
	_, err = db.sql.Exec(`UPDATE archive_documents SET tv_visible=0 WHERE household_id=? AND id=?`, doc.HouseholdID, doc.ID)
	require.NoError(t, err, "current visibility remains mutable")
	_, err = db.sql.Exec(`UPDATE archive_documents SET tv_visible=2`)
	require.Error(t, err)
}

func TestArchivePublishedOriginalSurvivesMetadataRollbackAndRetry(t *testing.T) {
	ctx := context.Background()
	db := archiveDocumentsDB(t)
	s, ref := archiveTestPublished(t)
	rollback := errors.New("later source or task insert failed")
	attempt := func(fail bool) error {
		return db.InWriteTx(ctx, func(tx *sql.Tx) error {
			if _, err := archiveBlobEnsureIn(ctx, tx, ref, documentsNow); err != nil {
				return err
			}
			if _, err := archiveDocumentCreateIn(ctx, tx, ref.HouseholdID, ref.SHA256, domain.ArchiveInputText, nil, documentsNow); err != nil {
				return err
			}
			if fail {
				return rollback
			}
			return nil
		})
	}
	require.ErrorIs(t, attempt(true), rollback)
	for _, table := range []string{"archive_blobs", "archive_documents"} {
		var count int
		require.NoError(t, db.sql.QueryRow(`SELECT count(*) FROM `+table).Scan(&count))
		require.Zero(t, count)
	}
	f, err := s.OpenVerified(ctx, ref)
	require.NoError(t, err)
	before, err := f.Stat()
	require.NoError(t, err)
	require.NoError(t, f.Close())
	again, err := s.Put(ctx, "home-1", strings.NewReader("喝奶 70ml"), archive.PutOptions{})
	require.NoError(t, err)
	require.Equal(t, ref, again)
	require.NoError(t, attempt(false))
	// A failed later acceptance must not remove an original already referenced.
	require.ErrorIs(t, attempt(true), rollback)
	f, err = s.OpenVerified(ctx, ref)
	require.NoError(t, err)
	defer f.Close()
	after, err := f.Stat()
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "喝奶 70ml", string(data))
	var docs int
	require.NoError(t, db.sql.QueryRow(`SELECT count(*) FROM archive_documents`).Scan(&docs))
	require.Equal(t, 1, docs)
}

func TestArchiveMetadataValidationAndDatabaseConstraints(t *testing.T) {
	ctx := context.Background()
	db := archiveDocumentsDB(t)
	_, ref := archiveTestPublished(t)
	for _, mutate := range []func(*domain.ArchiveBlobRef){func(r *domain.ArchiveBlobRef) { r.SizeBytes = 0 }, func(r *domain.ArchiveBlobRef) { r.SHA256 = strings.Repeat("A", 64) }, func(r *domain.ArchiveBlobRef) { r.SHA256 = "../invalid" }, func(r *domain.ArchiveBlobRef) { r.MIME = "" }, func(r *domain.ArchiveBlobRef) { r.MIME = "invalid\nheader" }, func(r *domain.ArchiveBlobRef) { r.StorageVersion = 2 }, func(r *domain.ArchiveBlobRef) { r.HouseholdID = "../home-1" }} {
		bad := ref
		mutate(&bad)
		err := db.InWriteTx(ctx, func(tx *sql.Tx) error { _, err := archiveBlobEnsureIn(ctx, tx, bad, documentsNow); return err })
		require.Error(t, err)
	}
	err := db.InWriteTx(ctx, func(tx *sql.Tx) error { _, err := archiveBlobEnsureIn(ctx, tx, ref, time.Time{}); return err })
	require.Error(t, err)
	for _, values := range []struct {
		hash    string
		size    int
		mime    string
		version int
	}{{strings.Repeat("A", 64), 1, "text/plain", 1}, {strings.Repeat("a", 64) + "\x00hidden", 1, "text/plain", 1}, {strings.Repeat("a", 64), 0, "text/plain", 1}, {strings.Repeat("a", 64), 1, "", 1}, {strings.Repeat("a", 64), 1, "text/plain", 2}} {
		_, err := db.sql.Exec(`INSERT INTO archive_blobs(household_id,sha256,size_bytes,mime,storage_version,created_at) VALUES('home-1',?,?,?,?,?)`, values.hash, values.size, values.mime, values.version, FormatTime(documentsNow))
		require.Error(t, err)
	}
	err = db.InWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := archiveDocumentCreateIn(ctx, tx, "home-1", ref.SHA256, "unknown", nil, documentsNow)
		return err
	})
	require.Error(t, err)
}
