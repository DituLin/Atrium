package archive

import (
	"context"
	"io"

	"github.com/DituLin/Atrium/internal/domain"
)

// IntakeRepository owns short, atomic authorization and acceptance transactions.
// No filesystem I/O is performed while its SQLite writer lock is held.
type IntakeRepository interface {
	IntakePreflight(context.Context, domain.IntegrationActor, string, domain.ArchiveIntakeMetadata) (int64, error)
	AcceptIntake(context.Context, domain.IntegrationActor, string, int64, domain.ArchiveIntakeMetadata, domain.ArchiveBlobRef) (*domain.ArchiveIntakeReceipt, error)
	LookupIntake(context.Context, domain.IntegrationActor, string, domain.ArchiveSourceIdentity) (*domain.ArchiveIntakeReceipt, error)
	GetIntakeTask(context.Context, domain.IntegrationActor, string, string) (*domain.ArchiveTask, error)
}
type Intake struct {
	blobs *BlobStore
	repo  IntakeRepository
}

func NewIntake(blobs *BlobStore, repo IntakeRepository) *Intake {
	return &Intake{blobs: blobs, repo: repo}
}

// Receive publishes the original, validates the transport terminator, then
// accepts all DB state atomically. validateEnd must consume/check the legal end
// of the single-slot envelope, never accept a truncated or extra multipart part.
// The host HTTP adapter owns bounded body reads and cancellation. A published
// original is retained on all later failures for safe retry/reuse.
func (s *Intake) Receive(ctx context.Context, actor domain.IntegrationActor, household string, m domain.ArchiveIntakeMetadata, content io.Reader, validateEnd func() error) (*domain.ArchiveIntakeReceipt, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if m.ExpectedSize != nil && *m.ExpectedSize > s.blobs.maxBytes {
		return nil, ErrTooLarge
	}
	if validateEnd == nil {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "missing intake envelope validation")
	}
	version, err := s.repo.IntakePreflight(ctx, actor, household, m)
	if err != nil {
		return nil, err
	}
	ref, err := s.blobs.Put(ctx, household, content, PutOptions{ExpectedSize: m.ExpectedSize, ExpectedSHA256: m.ExpectedSHA256})
	if err != nil {
		return nil, err
	}
	if err := validateEnd(); err != nil {
		return nil, err
	}
	return s.repo.AcceptIntake(ctx, actor, household, version, m, ref)
}
func (s *Intake) Lookup(ctx context.Context, actor domain.IntegrationActor, household string, source domain.ArchiveSourceIdentity) (*domain.ArchiveIntakeReceipt, error) {
	return s.repo.LookupIntake(ctx, actor, household, source)
}
func (s *Intake) Task(ctx context.Context, actor domain.IntegrationActor, household, id string) (*domain.ArchiveTaskProjection, error) {
	t, err := s.repo.GetIntakeTask(ctx, actor, household, id)
	if err != nil {
		return nil, err
	}
	return domain.ProjectArchiveTask(t), nil
}
