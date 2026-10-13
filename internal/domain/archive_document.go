package domain

import (
	"mime"
	"regexp"
	"time"
)

// ArchiveStorageVersion is the format version required for persisted blob metadata.
const ArchiveStorageVersion = 1

var archiveSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidateArchiveSHA256 requires a SHA-256 digest encoded as 64 lowercase hex digits.
func ValidateArchiveSHA256(hash string) error {
	if !archiveSHA256.MatchString(hash) {
		return Errorf(CodeInvalidRequest, "invalid archive SHA-256")
	}
	return nil
}

// ArchiveBlobRef is internal metadata, not access authority. Services must
// authorize a document before resolving its original. It never contains a path.
type ArchiveBlobRef struct {
	HouseholdID    string
	SHA256         string
	SizeBytes      int64
	MIME           string
	StorageVersion int
}

// ArchiveBlob records immutable metadata after filesystem publication. It is
// committed with logical document/source/task acceptance by the intake service.
type ArchiveBlob struct {
	ArchiveBlobRef
	CreatedAt time.Time
}

// ArchiveInputKind distinguishes an uploaded file from a received text occurrence.
type ArchiveInputKind string

// Archive input kinds preserve how an original arrived at intake.
const (
	ArchiveInputFile ArchiveInputKind = "file"
	ArchiveInputText ArchiveInputKind = "text"
)

// ArchiveDocument is a logical original. Multiple text occurrences can share
// the same physical blob and still retain separate document/event identities.
// Input identity is immutable; TVVisible represents the current visibility.
type ArchiveDocument struct {
	ID          string
	HouseholdID string
	BlobSHA256  string
	InputKind   ArchiveInputKind
	TVVisible   bool
	CreatedAt   time.Time
}

// Validate checks the household, digest, positive size, storage version and MIME
// syntax of a blob reference; it does not grant permission to read the original.
func (r ArchiveBlobRef) Validate() error {
	if err := ValidateArchiveIdentifier(r.HouseholdID); err != nil {
		return err
	}
	if err := ValidateArchiveSHA256(r.SHA256); err != nil {
		return err
	}
	if r.SizeBytes <= 0 || r.StorageVersion != ArchiveStorageVersion {
		return Errorf(CodeInvalidRequest, "invalid archive blob size or storage version")
	}
	if len(r.MIME) == 0 || len(r.MIME) > 255 {
		return Errorf(CodeInvalidRequest, "invalid archive MIME")
	}
	if _, _, err := mime.ParseMediaType(r.MIME); err != nil {
		return Errorf(CodeInvalidRequest, "invalid archive MIME")
	}
	return nil
}
