package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/screen"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestArchiveGrantPreservesLegacyPolicyAndCAS(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	a := archiveActor(t, db)
	// Existing resource policies remain explicit and complete through the grant.
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO family_members(id,name,updated_at) VALUES('child','Child','2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	p, err := db.Integrations().Get(ctx, a.PrincipalID)
	require.NoError(t, err)
	require.NoError(t, db.Sources().Upsert(ctx, "source", "Photos", "/synthetic/photos", archiveNow))
	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{ID: "screen", Name: "TV", TokenHash: "screen-hash", Status: domain.ScreenActive, CreatedAt: archiveNow, ApprovedAt: archiveNow}))
	p.Policy = domain.IntegrationPolicy{Permissions: []string{"home.read", "family.read", "family.write"}, MemberIDs: []string{"child"}, ScreenIDs: []string{"screen"}, SourceIDs: []string{"source"}, HouseholdIDs: []string{"home-2"}}
	require.NoError(t, db.Integrations().UpdatePolicy(ctx, p.ID, p.Policy, archiveNow))
	grant := domain.ArchiveGrant{Permissions: []string{"archive.read", "archive.ingest"}, HouseholdIDs: []string{"home-1"}, ExpectedPolicyVersion: 2}
	got, err := db.Integrations().GrantArchives(ctx, p.ID, grant, archiveNow)
	require.NoError(t, err)
	require.EqualValues(t, 3, got.PolicyVersion)
	require.Equal(t, []string{"home.read", "family.read", "family.write", "archive.read", "archive.ingest"}, got.Policy.Permissions)
	require.Equal(t, p.Policy.MemberIDs, got.Policy.MemberIDs)
	require.Equal(t, p.Policy.ScreenIDs, got.Policy.ScreenIDs)
	require.Equal(t, p.Policy.SourceIDs, got.Policy.SourceIDs)
	require.Equal(t, []string{"home-2", "home-1"}, got.Policy.HouseholdIDs)
	require.False(t, got.Policy.Allows("archive.organize"))
	require.False(t, got.Policy.Allows("archive.analyze"))
	_, err = db.Integrations().GrantArchives(ctx, p.ID, grant, archiveNow)
	archiveCode(t, err, domain.CodeConflict)
	after, err := db.Integrations().Get(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, got, after)
	for _, bad := range []domain.ArchiveGrant{
		{Permissions: []string{"family.write"}, HouseholdIDs: []string{"home-1"}, ExpectedPolicyVersion: 3},
		{Permissions: []string{"archive.read"}, HouseholdIDs: []string{"missing"}, ExpectedPolicyVersion: 3},
		{Permissions: []string{"archive.read"}, ExpectedPolicyVersion: 3},
		{HouseholdIDs: []string{"home-1"}, ExpectedPolicyVersion: 3},
	} {
		_, err = db.Integrations().GrantArchives(ctx, p.ID, bad, archiveNow)
		archiveCode(t, err, domain.CodeInvalidRequest)
	}
	_, err = db.Integrations().Get(ctx, p.ID)
	require.NoError(t, err)
}

func TestArchiveHouseholdReferencesMustExist(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	p := &domain.IntegrationPrincipal{ID: "unknown-household", Label: "test", Enabled: true, PolicyVersion: 1, Policy: domain.IntegrationPolicy{HouseholdIDs: []string{"missing"}}}
	c := &domain.IntegrationCredential{ID: "credential", PrincipalID: p.ID, TokenHash: "hash"}
	archiveCode(t, db.Integrations().Create(ctx, p, c), domain.CodeInvalidRequest)
}

func TestConcurrentArchiveGrantCASHasOneWinner(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	a := archiveActor(t, db)
	grant := domain.ArchiveGrant{Permissions: []string{"archive.analyze"}, HouseholdIDs: []string{"home-1"}, ExpectedPolicyVersion: 1}
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			p, err := db.Integrations().GrantArchives(ctx, a.PrincipalID, grant, archiveNow)
			if err == nil && p.PolicyVersion != 2 {
				results <- domain.Errorf(domain.CodeInternal, "wrong transaction result")
				return
			}
			results <- err
		}()
	}
	winners := 0
	for i := 0; i < 8; i++ {
		err := <-results
		if err == nil {
			winners++
		} else {
			archiveCode(t, err, domain.CodeConflict)
		}
	}
	require.Equal(t, 1, winners)
	p, err := db.Integrations().Get(ctx, a.PrincipalID)
	require.NoError(t, err)
	require.EqualValues(t, 2, p.PolicyVersion)
	require.True(t, p.Policy.Allows("archive.read"))
	require.True(t, p.Policy.Allows("archive.analyze"))
}

func TestArchiveGrantPreservesRevokedLegacyReferencesWithoutRestoringAccess(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	actor := archiveActor(t, db)
	require.NoError(t, db.Sources().Upsert(ctx, "source", "Photos", "/synthetic/photos", archiveNow))
	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{ID: "screen", Name: "TV", TokenHash: "screen-hash", Status: domain.ScreenActive, CreatedAt: archiveNow, ApprovedAt: archiveNow}))
	_, err := db.SQL().ExecContext(ctx, `INSERT INTO family_members(id,name,updated_at) VALUES('child','Child','2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	photo := &domain.Photo{ID: "photo", SourceID: "source", RelPath: "synthetic.jpg", Ext: "jpg", Status: domain.PhotoReady, PreviewStatus: domain.PreviewReady, FirstSeenAt: archiveNow, LastSeenAt: archiveNow, CreatedAt: archiveNow, UpdatedAt: archiveNow}
	require.NoError(t, db.Photos().Insert(ctx, photo))
	policy := domain.IntegrationPolicy{Permissions: []string{"photos.read", "screens.read", "screens.control", "family.read", "family.write"}, ScreenIDs: []string{"screen"}, SourceIDs: []string{"source"}, MemberIDs: []string{"child"}, HouseholdIDs: []string{"home-2"}}
	require.NoError(t, db.Integrations().UpdatePolicy(ctx, actor.PrincipalID, policy, archiveNow))
	_, err = db.Photos().GetScoped(ctx, policy.SourceIDs, photo.ID)
	require.NoError(t, err)
	require.NoError(t, db.Screens().Revoke(ctx, "screen", archiveNow))
	require.NoError(t, db.Sources().Revoke(ctx, "source", "synthetic revocation", archiveNow))
	grant := domain.ArchiveGrant{Permissions: []string{"archive.read", "archive.ingest"}, HouseholdIDs: []string{"home-1"}, ExpectedPolicyVersion: 2}
	got, err := db.Integrations().GrantArchives(ctx, actor.PrincipalID, grant, archiveNow)
	require.NoError(t, err, "adding archives must not require removing revoked legacy references")
	expected := policy
	expected.Permissions = append(append([]string{}, policy.Permissions...), grant.Permissions...)
	expected.HouseholdIDs = []string{"home-2", "home-1"}
	require.Equal(t, expected, got.Policy)
	require.EqualValues(t, 3, got.PolicyVersion)
	source, err := db.Sources().Get(ctx, "source")
	require.NoError(t, err)
	require.Equal(t, domain.SourceRevoked, source.Status)
	revokedScreen, err := db.Screens().Get(ctx, "screen")
	require.NoError(t, err)
	require.Equal(t, domain.ScreenRevoked, revokedScreen.Status)
	_, err = db.Photos().GetScoped(ctx, got.Policy.SourceIDs, photo.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	commands := screen.NewService(screen.Options{DB: db, Now: func() time.Time { return archiveNow }})
	_, err = commands.IssueIntegration(ctx, actor.PrincipalID, domain.NewIDAt(archiveNow), "screen", domain.CommandRefresh, domain.CommandPayload{})
	archiveCode(t, err, domain.CodeNotFound)
	// Complete replacement continues to validate every referenced resource.
	archiveCode(t, db.Integrations().UpdatePolicy(ctx, actor.PrincipalID, got.Policy, archiveNow), domain.CodeInvalidRequest)
	unchanged, err := db.Integrations().Get(ctx, actor.PrincipalID)
	require.NoError(t, err)
	require.Equal(t, got, unchanged)
}
