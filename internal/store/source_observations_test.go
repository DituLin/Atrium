package store_test

import (
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestObservationLeaseSurvivesUnchangedConfigButNotAuthorizationABA(t *testing.T) {
	db := testutil.NewDB(t)
	repo := db.Sources()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Upsert(t.Context(), "family", "Family", "/root", now))
	lease, err := repo.BeginObservation(t.Context(), "family", "/root")
	require.NoError(t, err)
	require.NoError(t, repo.Upsert(t.Context(), "family", "Renamed", "/root", now))
	require.NoError(t, repo.Restore(t.Context(), "family", now))
	total, free := int64(100), int64(50)
	observation := store.SourceObservation{Health: domain.HealthOnline, Success: true, At: now,
		Identity: &domain.Identity{FSType: "smbfs", MountFromHash: "old-mount"}, TotalBytes: &total, FreeBytes: &free}
	committed, err := repo.CommitObservation(t.Context(), lease, observation, false)
	require.NoError(t, err)
	require.True(t, committed, "unchanged active config and idempotent restore keep the generation")
	require.NoError(t, repo.Upsert(t.Context(), "family", "Renamed", "/root", now))
	row, err := repo.Get(t.Context(), "family")
	require.NoError(t, err)
	require.NotNil(t, row.LastCheckAt)
	require.NotNil(t, row.LastSuccessAt)
	require.NotNil(t, row.ShareStatsAt)
	require.NoError(t, repo.Revoke(t.Context(), "family", "revoked", now))
	require.NoError(t, repo.Restore(t.Context(), "family", now))
	// Open a second repository handle to prove the guard is durable, not an
	// in-memory counter that loses the same-timestamp revoke/restore cycle.
	reopened, err := store.Open(t.Context(), db.Path())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	committed, err = reopened.Sources().CommitObservation(t.Context(), lease, observation, false)
	require.NoError(t, err)
	require.False(t, committed)
	row, err = repo.Get(t.Context(), "family")
	require.NoError(t, err)
	require.Nil(t, row.LastCheckAt)
	require.Nil(t, row.LastSuccessAt)
	require.Nil(t, row.ShareStatsAt)
	require.Equal(t, "old-mount", row.IdentityBound.MountFromHash, "existing identity safety binding survives authorization renewal")
}

func TestObservationPublicationRollsBackAllFieldsOnFailure(t *testing.T) {
	db := testutil.NewDB(t)
	repo := db.Sources()
	now := time.Now()
	require.NoError(t, repo.Upsert(t.Context(), "family", "Family", "/root", now))
	lease, err := repo.BeginObservation(t.Context(), "family", "/root")
	require.NoError(t, err)
	_, err = db.SQL().ExecContext(t.Context(), `CREATE TRIGGER fail_capacity BEFORE UPDATE OF share_total_bytes ON data_sources BEGIN SELECT RAISE(ABORT, 'capacity failure'); END`)
	require.NoError(t, err)
	total, free := int64(100), int64(50)
	committed, err := repo.CommitObservation(t.Context(), lease, store.SourceObservation{
		Health: domain.HealthOnline, Success: true, At: now,
		Identity: &domain.Identity{FSType: "smbfs"}, TotalBytes: &total, FreeBytes: &free,
	}, false)
	require.Error(t, err)
	require.False(t, committed)
	row, err := repo.Get(t.Context(), "family")
	require.NoError(t, err)
	require.Nil(t, row.LastCheckAt)
	require.Nil(t, row.IdentityBound)
	require.Nil(t, row.ShareStatsAt)
}

func TestConcurrentFirstBindingCannotReplaceCommittedIdentity(t *testing.T) {
	db := testutil.NewDB(t)
	repo := db.Sources()
	now := time.Now()
	require.NoError(t, repo.Upsert(t.Context(), "family", "Family", "/root", now))
	first, err := repo.BeginObservation(t.Context(), "family", "/root")
	require.NoError(t, err)
	delayed, err := repo.BeginObservation(t.Context(), "family", "/root")
	require.NoError(t, err)
	committed, err := repo.CommitObservation(t.Context(), first, store.SourceObservation{
		Health: domain.HealthOnline, Success: true, At: now, Identity: &domain.Identity{FSType: "smbfs", MountFromHash: "mount-A"},
	}, false)
	require.NoError(t, err)
	require.True(t, committed)
	committed, err = repo.CommitObservation(t.Context(), delayed, store.SourceObservation{
		Health: domain.HealthOnline, Success: true, At: now.Add(time.Second), Identity: &domain.Identity{FSType: "smbfs", MountFromHash: "mount-B"},
	}, false)
	require.NoError(t, err)
	require.False(t, committed, "first identity binding supersedes other unbound observations")
	row, err := repo.Get(t.Context(), "family")
	require.NoError(t, err)
	require.Equal(t, "mount-A", row.IdentityBound.MountFromHash)
	require.WithinDuration(t, now, *row.LastCheckAt, time.Microsecond)
}

func TestDelayedOlderObservationCannotReplaceNewerHealth(t *testing.T) {
	db := testutil.NewDB(t)
	repo := db.Sources()
	now := time.Now()
	require.NoError(t, repo.Upsert(t.Context(), "family", "Family", "/root", now))
	older, err := repo.BeginObservation(t.Context(), "family", "/root")
	require.NoError(t, err)
	newer, err := repo.BeginObservation(t.Context(), "family", "/root")
	require.NoError(t, err)
	committed, err := repo.CommitObservation(t.Context(), newer, store.SourceObservation{Health: domain.HealthOffline, At: now.Add(time.Second)}, false)
	require.NoError(t, err)
	require.True(t, committed)
	committed, err = repo.CommitObservation(t.Context(), older, store.SourceObservation{Health: domain.HealthOnline, Success: true, At: now}, false)
	require.NoError(t, err)
	require.False(t, committed, "a late result cannot move last_check_at backwards")
	row, err := repo.Get(t.Context(), "family")
	require.NoError(t, err)
	require.Equal(t, domain.HealthOffline, row.Health)
	require.Nil(t, row.LastSuccessAt)
}
