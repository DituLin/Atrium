package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// authorizeArchiveClient must run inside the same transaction as the protected
// read/write. The clock is sampled only after that transaction acquires its lock.
func (r *ArchiveTasks) authorizeArchiveClient(ctx context.Context, tx *sql.Tx, actor domain.IntegrationActor, household, permission string, now time.Time) (*domain.IntegrationPrincipal, error) {
	repo := r.db.Integrations().WithTx(tx)
	credential, err := repo.GetCredential(ctx, actor.CredentialID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.Errorf(domain.CodeUnauthorized, "integration credential is inactive")
	}
	if err != nil {
		return nil, err
	}
	if credential.PrincipalID != actor.PrincipalID || !credential.Active(now) {
		return nil, domain.Errorf(domain.CodeUnauthorized, "integration credential is inactive")
	}
	p, err := repo.Get(ctx, actor.PrincipalID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.Errorf(domain.CodeUnauthorized, "integration is inactive")
	}
	if err != nil {
		return nil, err
	}
	if !p.Active(now) {
		return nil, domain.Errorf(domain.CodeUnauthorized, "integration is inactive")
	}
	// Scope denial deliberately hides household and task existence.
	if !p.Policy.AllowsHousehold(household) {
		return nil, domain.ErrNotFound
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM households WHERE id=?`, household).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 1 {
		return nil, domain.ErrNotFound
	}
	if permission != "" && !p.Policy.AllowsArchive(permission, household) {
		return nil, domain.Errorf(domain.CodeForbidden, "archive permission denied")
	}
	return p, nil
}

func (r *ArchiveTasks) archiveDelegationCode(ctx context.Context, tx *sql.Tx, task *domain.ArchiveTask, now time.Time) (string, error) {
	if task.PrincipalID == "" || task.PolicyVersion < 1 {
		return "unbound", nil
	}
	p, err := r.db.Integrations().WithTx(tx).Get(ctx, task.PrincipalID)
	if errors.Is(err, domain.ErrNotFound) {
		return "principal_inactive", nil
	}
	if err != nil {
		return "", err
	}
	if !p.Active(now) {
		return "principal_inactive", nil
	}
	if p.PolicyVersion != task.PolicyVersion {
		return "policy_changed", nil
	}
	if !p.Policy.AllowsArchive("archive.ingest", task.HouseholdID) {
		return "permission_denied", nil
	}
	return "", nil
}
func (r *ArchiveTasks) authorizeArchiveDelegate(ctx context.Context, tx *sql.Tx, task *domain.ArchiveTask, now time.Time) error {
	code, err := r.archiveDelegationCode(ctx, tx, task, now)
	if err != nil {
		return err
	}
	if code != "" {
		return domain.Errorf(domain.CodeForbidden, "archive delegation is no longer authorized").WithDetails(map[string]any{"delegation_error_code": code})
	}
	return nil
}

// An ingest-only caller receives a receipt, not extracted content or input from
// prior work. Read-capable callers can inspect the complete scoped task.
func archiveClientResult(task *domain.ArchiveTask, p *domain.IntegrationPrincipal) *domain.ArchiveTask {
	if p.Policy.AllowsArchive("archive.read", task.HouseholdID) {
		return task
	}
	return &domain.ArchiveTask{ID: task.ID, HouseholdID: task.HouseholdID, PrincipalID: task.PrincipalID, PolicyVersion: task.PolicyVersion, Kind: task.Kind, Status: task.Status, Version: task.Version, StateVersion: task.StateVersion, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, DelegationErrorCode: task.DelegationErrorCode}
}

// A household organizer can act across intake integrations when it also holds
// read permission. An owner may cancel with ingest or clarify with organize.
func archiveTaskClientPermission(p *domain.IntegrationPrincipal, task *domain.ArchiveTask, permission string) error {
	if p.Policy.AllowsArchive("archive.read", task.HouseholdID) && p.Policy.AllowsArchive("archive.organize", task.HouseholdID) {
		return nil
	}
	if p.ID != task.PrincipalID {
		return domain.ErrNotFound
	}
	if !p.Policy.AllowsArchive(permission, task.HouseholdID) {
		return domain.Errorf(domain.CodeForbidden, "archive permission denied")
	}
	return nil
}
