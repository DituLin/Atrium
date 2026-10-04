package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
)

func TestMigrateFromEmptyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "atrium.db")
	db, err := store.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	n, err := db.Migrate(ctx)
	require.NoError(t, err)
	require.Equal(t, 14, n)

	again, err := db.Migrate(ctx)
	require.NoError(t, err)
	require.Zero(t, again, "re-running migrations must be a no-op")

	version, err := db.SchemaVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, 14, version)
}

func TestOpenEnablesWALAndForeignKeys(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()

	var mode string
	require.NoError(t, db.SQL().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
	require.Equal(t, "wal", mode)

	var fk int
	require.NoError(t, db.SQL().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk))
	require.Equal(t, 1, fk)
}

func TestForeignKeysAreEnforced(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO photos
		(id, source_id, rel_path, ext, size_bytes, mtime_unix, status, first_seen_at, last_seen_at,
		 last_seen_generation, created_at, updated_at)
		VALUES ('p1', 'missing_source', 'a.jpg', 'jpg', 1, 1, 'pending', '', '', 0, '', '')`)
	require.Error(t, err, "insert with a dangling source_id must fail")
}

func TestSchemaMatchesDesignTables(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	rows, err := db.SQL().QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		got = append(got, name)
	}
	require.NoError(t, rows.Err())
	require.ElementsMatch(t, []string{
		"archive_products", "archive_invocations", "archive_extraction_products", "archive_budgets", "archive_extractions", "archive_subject_events", "archive_observations", "archive_observation_revisions", "archive_publications", "archive_publication_observations", "archive_outbox", "archive_manual_revisions", "archive_private_regions", "archive_questions", "archive_legacy_outbox", "archive_legacy_projections", "archive_legacy_record_events", "archive_transaction_events", "archive_event_equivalences", "archive_observation_alternatives", "archive_publication_alternatives",
		"archive_entities", "archive_entity_attributes", "archive_entity_bindings", "archive_entity_binding_merge_context", "archive_entity_history", "archive_entity_link_merge_context", "archive_entity_links", "archive_entity_merges", "archive_entity_redirects", "archive_legacy_entities", "archive_protected_identity_facts", "archive_resolution_revisions",
		"archive_blobs", "archive_documents", "archive_sources", "archive_visibility_events", "households", "local_household", "archive_tasks", "archive_task_events", "archive_task_resolutions", "family_members", "family_records", "family_record_exports", "admin_tokens", "audit_log", "data_sources", "jobs", "pairings", "photo_exclusions",
		"video_work", "video_scan_state", "videos", "photos", "preview_files", "scan_runs", "schema_migrations", "screen_commands",
		"screens", "settings", "widget_cache", "integration_principals", "integration_credentials", "integration_operations",
	}, got)
}

func TestSettingsRoundTrip(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	s := db.Settings()

	_, err := s.Get(ctx, "absent")
	require.ErrorIs(t, err, domain.ErrNotFound)

	require.NoError(t, s.Set(ctx, store.SettingHomeTimezone, "Asia/Singapore"))
	v, err := s.Get(ctx, store.SettingHomeTimezone)
	require.NoError(t, err)
	require.Equal(t, "Asia/Singapore", v)

	require.NoError(t, s.Set(ctx, store.SettingHomeTimezone, "Europe/Berlin"))
	v, err = s.GetDefault(ctx, store.SettingHomeTimezone, "UTC")
	require.NoError(t, err)
	require.Equal(t, "Europe/Berlin", v)

	require.NoError(t, s.SetInt(ctx, "counter", 42))
	n, err := s.GetInt(ctx, "counter", 0)
	require.NoError(t, err)
	require.EqualValues(t, 42, n)
}

func TestSourcesUpsertAndReconcile(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	repo := db.Sources()

	require.NoError(t, repo.Upsert(ctx, "family_photos", "Family photos", "/Volumes/photos/family", now))
	require.NoError(t, repo.Upsert(ctx, "old_share", "Old share", "/Volumes/photos/old", now))

	require.NoError(t, repo.SetHealth(ctx, "family_photos", domain.HealthOnline, "", true, now))
	ident := domain.Identity{FSType: "smbfs", MountFromHash: "abc123", IsNetworkMount: true}
	require.NoError(t, repo.BindIdentity(ctx, "family_photos", ident, now))

	revoked, err := repo.RevokeMissing(ctx, []string{"family_photos"}, "removed_from_config", now)
	require.NoError(t, err)
	require.Equal(t, []string{"old_share"}, revoked)

	got, err := repo.Get(ctx, "family_photos")
	require.NoError(t, err)
	require.Equal(t, domain.SourceActive, got.Status)
	require.Equal(t, domain.HealthOnline, got.Health)
	require.NotNil(t, got.IdentityBound)
	require.Equal(t, "smbfs", got.IdentityBound.FSType)
	require.NotNil(t, got.LastSuccessAt)

	gone, err := repo.Get(ctx, "old_share")
	require.NoError(t, err)
	require.Equal(t, domain.SourceRevoked, gone.Status)
	require.Equal(t, "removed_from_config", gone.RevokeReason)

	// A re-run of config reconciliation must not clear health or identity.
	require.NoError(t, repo.Upsert(ctx, "family_photos", "Family photos", "/Volumes/photos/family", now))
	again, err := repo.Get(ctx, "family_photos")
	require.NoError(t, err)
	require.Equal(t, domain.HealthOnline, again.Health)
	require.NotNil(t, again.IdentityBound)

	_, err = repo.Get(ctx, "nope")
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.True(t, errors.Is(repo.Revoke(ctx, "nope", "x", now), domain.ErrNotFound))
}
