package store_test

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func archiveActor(t *testing.T, db *store.DB) domain.IntegrationActor {
	t.Helper()
	ctx := context.Background()
	// Shared fixture identity for T01 lifecycle tests, independent of the wall clock.
	if _, err := db.Integrations().Get(ctx, "archive-test"); err == nil {
		return domain.IntegrationActor{PrincipalID: "archive-test", CredentialID: "archive-test-credential"}
	}
	_, err := db.SQL().ExecContext(ctx, `INSERT OR IGNORE INTO households(id) VALUES('home-1'),('home-2')`)
	require.NoError(t, err)
	return newArchiveActor(t, db, "archive-test", []string{"archive.read", "archive.ingest", "archive.organize"}, []string{"home-1", "home-2"})
}
func newArchiveActor(t *testing.T, db *store.DB, id string, permissions, households []string) domain.IntegrationActor {
	t.Helper()
	p := &domain.IntegrationPrincipal{ID: id, Label: id, Enabled: true, Policy: domain.IntegrationPolicy{Permissions: permissions, HouseholdIDs: households}, PolicyVersion: 1, ExpiresAt: archiveNow.AddDate(10, 0, 0), CreatedAt: archiveNow, UpdatedAt: archiveNow}
	c := &domain.IntegrationCredential{ID: id + "-credential", PrincipalID: id, TokenHash: id + "-hash", ExpiresAt: p.ExpiresAt, CreatedAt: archiveNow}
	require.NoError(t, db.Integrations().Create(context.Background(), p, c))
	return domain.IntegrationActor{PrincipalID: id, CredentialID: c.ID}
}

func TestArchiveClientScopesAndIdempotency(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	a := archiveActor(t, db)
	r := db.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	task, _, err := r.Enqueue(ctx, a, "home-1", "document.extract", "shared-key", archiveInput)
	require.NoError(t, err)
	b := newArchiveActor(t, db, "other", []string{"archive.read", "archive.ingest"}, []string{"home-2"})
	_, err = r.Get(ctx, b, "home-1", task.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = r.Events(ctx, b, "home-1", task.ID, 0, 10)
	require.ErrorIs(t, err, domain.ErrNotFound)
	empty := newArchiveActor(t, db, "empty", []string{"archive.read"}, nil)
	_, err = r.Get(ctx, empty, "home-1", task.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	sameHome := newArchiveActor(t, db, "same-home", []string{"archive.ingest"}, []string{"home-1"})
	other, replay, err := r.Enqueue(ctx, sameHome, "home-1", "document.extract", "shared-key", archiveInput)
	require.NoError(t, err)
	require.False(t, replay)
	require.NotEqual(t, task.ID, other.ID)
	require.Empty(t, other.InputJSON)
	require.Empty(t, other.StateJSON)
	require.Empty(t, other.ResultJSON)
	_, err = r.Get(ctx, sameHome, "home-1", task.ID)
	archiveCode(t, err, domain.CodeForbidden)
	_, err = r.Cancel(ctx, sameHome, "home-1", task.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	invalid := a
	invalid.CredentialID = "missing"
	_, err = r.Get(ctx, invalid, "home-2", "missing")
	archiveCode(t, err, domain.CodeUnauthorized)
	_, _, err = r.Enqueue(ctx, invalid, "home-1", "document.extract", "shared-key", archiveInput)
	archiveCode(t, err, domain.CodeUnauthorized)
}

func TestArchiveRotationPreservesDelegateButRejectsStaleClient(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	a := archiveActor(t, db)
	r := db.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	task, _, err := r.Enqueue(ctx, a, "home-1", "document.extract", "rotation", archiveInput)
	require.NoError(t, err)
	replacement := &domain.IntegrationCredential{ID: "new-credential", PrincipalID: a.PrincipalID, TokenHash: "replacement", ExpiresAt: archiveNow.Add(time.Hour), CreatedAt: archiveNow}
	require.NoError(t, db.Integrations().Rotate(ctx, a.PrincipalID, replacement, archiveNow))
	_, err = r.Get(ctx, a, "home-1", task.ID)
	archiveCode(t, err, domain.CodeUnauthorized)
	_, _, err = r.Enqueue(ctx, a, "home-1", "document.extract", "rotation", archiveInput)
	archiveCode(t, err, domain.CodeUnauthorized)
	_, err = r.Cancel(ctx, a, "home-1", task.ID)
	archiveCode(t, err, domain.CodeUnauthorized)
	worker, err := r.Claim(ctx, "home-1", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, worker)
	_, err = r.Finish(ctx, "home-1", worker.ID, worker.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
	require.NoError(t, err)
	a.CredentialID = replacement.ID
	got, err := r.Get(ctx, a, "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskSucceeded, got.Status)
}

func TestArchivePolicyChangeFencesEveryWorkerMutationAndReplay(t *testing.T) {
	for _, op := range []string{"renew", "checkpoint", "finish", "retry", "wait"} {
		t.Run(op, func(t *testing.T) {
			db := testutil.NewDB(t)
			ctx := context.Background()
			a := archiveActor(t, db)
			r := db.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
			_, _, err := r.Enqueue(ctx, a, "home-1", "document.extract", op, archiveInput)
			require.NoError(t, err)
			task, err := r.Claim(ctx, "home-1", time.Minute)
			require.NoError(t, err)
			p, err := db.Integrations().Get(ctx, a.PrincipalID)
			require.NoError(t, err)
			require.NoError(t, db.Integrations().UpdatePolicy(ctx, a.PrincipalID, p.Policy, archiveNow))
			switch op {
			case "renew":
				_, err = r.Renew(ctx, "home-1", task.ID, task.LeaseToken, time.Minute)
			case "checkpoint":
				_, err = r.Checkpoint(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate())
			case "finish":
				_, err = r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
			case "retry":
				_, err = r.Retry(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate(), archiveNow.Add(time.Hour))
			case "wait":
				_, err = r.Wait(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate())
			}
			archiveCode(t, err, domain.CodeForbidden)
			got, err := r.Get(ctx, a, "home-1", task.ID)
			require.NoError(t, err)
			require.Equal(t, task.Version, got.Version)
			require.Equal(t, "policy_changed", got.DelegationErrorCode)
			p.Policy.Permissions = []string{"archive.read"}
			require.NoError(t, db.Integrations().UpdatePolicy(ctx, a.PrincipalID, p.Policy, archiveNow))
			_, _, err = r.Enqueue(ctx, a, "home-1", "document.extract", op, archiveInput)
			archiveCode(t, err, domain.CodeForbidden)
		})
	}
}

func TestArchiveClaimSkipsUnboundAndUnauthorizedWork(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	a := archiveActor(t, db)
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	first, _, err := r.Enqueue(ctx, a, "home-1", "document.extract", "old", archiveInput)
	require.NoError(t, err)
	require.NoError(t, db.Integrations().Revoke(ctx, a.PrincipalID, archiveNow))
	b := newArchiveActor(t, db, "active", []string{"archive.ingest"}, []string{"home-1"})
	second, _, err := r.Enqueue(ctx, b, "home-1", "document.extract", "new", archiveInput)
	require.NoError(t, err)
	// A pre-T02 task is explicitly unbound, never implicitly assigned a principal.
	_, err = db.SQL().ExecContext(ctx, `INSERT INTO archive_tasks SELECT household_id || '-legacy',household_id,kind,'legacy',input_json,status,stage,attempts,attempt_id,version,state_version,state_json,result_json,clarification_json,available_at,lease_token,lease_expires_at,last_error_code,created_at,updated_at,NULL,NULL FROM archive_tasks WHERE id=?`, first.ID)
	require.NoError(t, err)
	got, err := r.Claim(ctx, "home-1", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, second.ID, got.ID)
	fake.Set(archiveNow.Add(2 * time.Minute))
	require.NoError(t, db.Integrations().Revoke(ctx, b.PrincipalID, archiveNow))
	got, err = r.Claim(ctx, "home-1", time.Minute)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestArchiveLockWaitRechecksClientAndDelegateAuthorization(t *testing.T) {
	for _, invalidation := range []string{"expiry", "credential_expiry", "revoke", "rotate", "policy"} {
		for _, op := range []string{"enqueue", "cancel", "resolve", "get", "events", "finish"} {
			t.Run(invalidation+"/"+op, func(t *testing.T) {
				db := testutil.NewDB(t)
				ctx := context.Background()
				a := archiveActor(t, db)
				fake := &archiveClock{now: archiveNow}
				r := db.ArchiveTasksWithClock(fake)
				_, _, err := r.Enqueue(ctx, a, "home-1", "document.extract", "lock", archiveInput)
				require.NoError(t, err)
				task, err := r.Claim(ctx, "home-1", time.Minute)
				require.NoError(t, err)
				if op == "resolve" {
					task, err = r.Wait(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate())
					require.NoError(t, err)
				}
				db.SQL().SetMaxOpenConns(1)
				lock, err := db.BeginWrite(ctx)
				require.NoError(t, err)
				defer lock.Rollback()
				before := db.SQL().Stats().WaitCount
				result := make(chan error, 1)
				go func() {
					var e error
					switch op {
					case "enqueue":
						_, _, e = r.Enqueue(ctx, a, "home-1", "document.extract", "lock", archiveInput)
					case "cancel":
						_, e = r.Cancel(ctx, a, "home-1", task.ID)
					case "resolve":
						_, _, e = r.ResolveWaiting(ctx, a, "home-1", task.ID, task.Version, "answer", json.RawMessage(`{}`))
					case "get":
						_, e = r.Get(ctx, a, "home-1", task.ID)
					case "events":
						_, e = r.Events(ctx, a, "home-1", task.ID, 0, 10)
					case "finish":
						_, e = r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
					}
					result <- e
				}()
				deadline := time.Now().Add(5 * time.Second)
				for db.SQL().Stats().WaitCount == before && time.Now().Before(deadline) {
					runtime.Gosched()
				}
				require.Greater(t, db.SQL().Stats().WaitCount, before)
				switch invalidation {
				case "expiry":
					_, err = lock.ExecContext(ctx, `UPDATE integration_principals SET expires_at=? WHERE id=?`, store.FormatTime(archiveNow.Add(time.Second)), a.PrincipalID)
					fake.Set(archiveNow.Add(2 * time.Second))
				case "credential_expiry":
					_, err = lock.ExecContext(ctx, `UPDATE integration_credentials SET expires_at=? WHERE id=?`, store.FormatTime(archiveNow.Add(time.Second)), a.CredentialID)
					fake.Set(archiveNow.Add(2 * time.Second))
				case "revoke":
					_, err = lock.ExecContext(ctx, `UPDATE integration_principals SET enabled=0 WHERE id=?`, a.PrincipalID)
				case "rotate":
					_, err = lock.ExecContext(ctx, `UPDATE integration_credentials SET revoked_at=? WHERE id=?`, store.FormatTime(archiveNow), a.CredentialID)
				case "policy":
					_, err = lock.ExecContext(ctx, `UPDATE integration_principals SET policy_json='{}',policy_version=policy_version+1 WHERE id=?`, a.PrincipalID)
				}
				require.NoError(t, err)
				require.NoError(t, lock.Commit())
				err = <-result
				if op == "finish" && (invalidation == "rotate" || invalidation == "credential_expiry") {
					require.NoError(t, err)
					return
				}
				if invalidation == "policy" && op != "finish" {
					require.ErrorIs(t, err, domain.ErrNotFound)
				} else if op == "finish" {
					archiveCode(t, err, domain.CodeForbidden)
				} else {
					archiveCode(t, err, domain.CodeUnauthorized)
				}
				var version int64
				require.NoError(t, db.SQL().QueryRowContext(ctx, `SELECT version FROM archive_tasks WHERE id=?`, task.ID).Scan(&version))
				require.Equal(t, task.Version, version)
			})
		}
	}
}

func TestArchiveHouseholdOrganizerResolvesAndCancelsWithAudit(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	origin := archiveActor(t, db)
	r := db.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	_, _, err := r.Enqueue(ctx, origin, "home-1", "document.extract", "shared-task", archiveInput)
	require.NoError(t, err)
	task, err := r.Claim(ctx, "home-1", time.Minute)
	require.NoError(t, err)
	waiting, err := r.Wait(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate())
	require.NoError(t, err)
	organizer := newArchiveActor(t, db, "organizer", []string{"archive.read", "archive.organize"}, []string{"home-1"})
	resolved, replay, err := r.ResolveWaiting(ctx, organizer, "home-1", task.ID, waiting.Version, "answer", json.RawMessage(`{"subject":"discovered-person"}`))
	require.NoError(t, err)
	require.False(t, replay)
	require.Equal(t, origin.PrincipalID, resolved.PrincipalID)
	require.Equal(t, task.PolicyVersion, resolved.PolicyVersion)
	require.Equal(t, task.InputJSON, resolved.InputJSON)
	again, replay, err := r.ResolveWaiting(ctx, organizer, "home-1", task.ID, waiting.Version, "answer", json.RawMessage(`{"subject":"discovered-person"}`))
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, resolved.Version, again.Version)
	// A changed/revoked origin cannot resume work, but current household authority
	// can always terminate it without refreshing the original delegation.
	require.NoError(t, db.Integrations().Revoke(ctx, origin.PrincipalID, archiveNow))
	cancelled, err := r.Cancel(ctx, organizer, "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskCancelled, cancelled.Status)
	require.Equal(t, origin.PrincipalID, cancelled.PrincipalID)
	require.Equal(t, task.PolicyVersion, cancelled.PolicyVersion)
	events, err := r.Events(ctx, organizer, "home-1", task.ID, 0, 10)
	require.NoError(t, err)
	require.Equal(t, organizer.PrincipalID, events[len(events)-2].ActorPrincipalID)
	require.Equal(t, organizer.PrincipalID, events[len(events)-1].ActorPrincipalID)
	require.Equal(t, origin.PrincipalID, events[len(events)-1].PrincipalID)
}

func TestArchiveOwnerCanCancelFencedTaskWithCurrentIngestGrant(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	a := archiveActor(t, db)
	r := db.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	task, _, err := r.Enqueue(ctx, a, "home-1", "document.extract", "fenced", archiveInput)
	require.NoError(t, err)
	require.NoError(t, db.Integrations().UpdatePolicy(ctx, a.PrincipalID, domain.IntegrationPolicy{Permissions: []string{"archive.ingest"}, HouseholdIDs: []string{"home-1"}}, archiveNow))
	cancelled, err := r.Cancel(ctx, a, "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskCancelled, cancelled.Status)
	require.Empty(t, cancelled.InputJSON)
	_, err = r.Cancel(ctx, a, "home-1", task.ID)
	require.NoError(t, err)
}
