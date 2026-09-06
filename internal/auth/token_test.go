package auth_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store/testutil"
)

func TestTokenFormatAndScope(t *testing.T) {
	screen, err := auth.NewScreenToken()
	require.NoError(t, err)
	admin, err := auth.NewAdminToken()
	require.NoError(t, err)

	require.Len(t, screen, len(auth.ScreenPrefix)+auth.SecretChars)
	require.Len(t, admin, len(auth.AdminPrefix)+auth.SecretChars)
	require.True(t, auth.ValidToken(screen, auth.ScreenPrefix))
	require.True(t, auth.ValidToken(admin, auth.AdminPrefix))
	require.False(t, auth.ValidToken(screen, auth.AdminPrefix))

	require.Equal(t, auth.ScopeScreen, auth.ScopeOf(screen))
	require.Equal(t, auth.ScopeAdmin, auth.ScopeOf(admin))
	require.Equal(t, auth.ScopeNone, auth.ScopeOf("atr_scr_short"))
	require.Equal(t, auth.ScopeNone, auth.ScopeOf(""))
	require.Equal(t, auth.ScopeNone, auth.ScopeOf("atr_scr_"+string(make([]byte, 43))))

	// Two tokens are never equal, and the hash is stable and hex.
	other, err := auth.NewScreenToken()
	require.NoError(t, err)
	require.NotEqual(t, screen, other)
	require.Equal(t, auth.HashToken(screen), auth.HashToken(screen))
	require.NotEqual(t, auth.HashToken(screen), auth.HashToken(other))
	require.Len(t, auth.HashToken(screen), 64)
	require.NotContains(t, auth.HashToken(screen), screen, "the hash must not embed the token")
}

func TestAdminTokenFileIsMode0600(t *testing.T) {
	dir := t.TempDir()
	token, err := auth.NewAdminToken()
	require.NoError(t, err)

	path, err := auth.WriteAdminTokenFile(dir, token)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, auth.AdminTokenFile), path)

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	read, err := auth.ReadAdminTokenFile(dir)
	require.NoError(t, err)
	require.Equal(t, token, read)

	// Rewriting an existing file must not widen its permissions.
	require.NoError(t, os.Chmod(path, 0o644))
	_, err = auth.WriteAdminTokenFile(dir, token)
	require.NoError(t, err)
	st, err = os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	_, err = auth.ReadAdminTokenFile(t.TempDir())
	require.Error(t, err)
}

func TestAdminServiceIssueEnsureAndReset(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	svc := auth.NewAdminService(db)

	first, created, err := svc.EnsureInitial(ctx, "init", now)
	require.NoError(t, err)
	require.True(t, created)
	require.True(t, auth.ValidToken(first, auth.AdminPrefix))

	// A second init keeps the existing credential.
	again, created, err := svc.EnsureInitial(ctx, "init", now)
	require.NoError(t, err)
	require.False(t, created)
	require.Empty(t, again)

	rec, err := db.AdminTokens().GetByHash(ctx, auth.HashToken(first))
	require.NoError(t, err)
	require.Nil(t, rec.RevokedAt)

	// Reset revokes everything and issues a replacement.
	replacement, err := svc.Reset(ctx, "token reset", now.Add(time.Hour))
	require.NoError(t, err)
	require.NotEqual(t, first, replacement)

	old, err := db.AdminTokens().GetByHash(ctx, auth.HashToken(first))
	require.NoError(t, err)
	require.NotNil(t, old.RevokedAt, "the previous token must be revoked")

	fresh, err := db.AdminTokens().GetByHash(ctx, auth.HashToken(replacement))
	require.NoError(t, err)
	require.Nil(t, fresh.RevokedAt)

	live, err := db.AdminTokens().CountLive(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, live)
}

func TestAdminServiceRotate(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	svc := auth.NewAdminService(db)

	first, rec, err := svc.Issue(ctx, "cli", now)
	require.NoError(t, err)

	next, err := svc.Rotate(ctx, rec.ID, "rotated", now.Add(time.Minute))
	require.NoError(t, err)
	require.NotEqual(t, first, next)

	old, err := db.AdminTokens().GetByHash(ctx, auth.HashToken(first))
	require.NoError(t, err)
	require.NotNil(t, old.RevokedAt)
}

func TestPairingCodeIsSixDigits(t *testing.T) {
	seen := map[string]int{}
	for i := 0; i < 200; i++ {
		code, err := auth.NewPairingCode()
		require.NoError(t, err)
		require.Len(t, code, 6)
		for _, r := range code {
			require.True(t, r >= '0' && r <= '9', "code must be digits only")
		}
		seen[code]++
	}
	require.Greater(t, len(seen), 150, "codes must not repeat systematically")
}

func TestPairingServiceStateMachine(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	current := now
	svc := auth.NewPairingService(db, func() time.Time { return current })

	pr, err := svc.Start(ctx, "living room tv", "192.168.1.20")
	require.NoError(t, err)
	require.Equal(t, domain.PairingPending, pr.Status)
	require.Equal(t, now.Add(auth.PairingTTL), pr.ExpiresAt)

	// Claiming a pending pairing is a conflict.
	_, err = svc.Claim(ctx, pr.ID)
	claimErr, ok := domain.AsError(err)
	require.True(t, ok)
	require.Equal(t, domain.CodeConflict, claimErr.Code)

	res, err := svc.Approve(ctx, pr.ID, "living_room_tv", "Living room")
	require.NoError(t, err)
	require.Equal(t, domain.PairingApproved, res.Pairing.Status)
	require.Equal(t, "living_room_tv", res.Screen.ID)

	// A double approval is rejected.
	_, err = svc.Approve(ctx, pr.ID, "living_room_tv", "Living room")
	require.Error(t, err)

	claim, err := svc.Claim(ctx, pr.ID)
	require.NoError(t, err)
	require.True(t, auth.ValidToken(claim.Token, auth.ScreenPrefix))
	require.Equal(t, auth.HashToken(claim.Token), claim.Screen.TokenHash)

	_, err = svc.Claim(ctx, pr.ID)
	second, ok := domain.AsError(err)
	require.True(t, ok)
	require.Equal(t, domain.CodePairingClaimed, second.Code)

	// A pending pairing expires with its TTL.
	fresh, err := svc.Start(ctx, "", "")
	require.NoError(t, err)
	current = current.Add(auth.PairingTTL + time.Second)
	expired, err := svc.Get(ctx, fresh.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PairingExpired, expired.Status)
}

func TestApproveRejectsInvalidScreenSlug(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	svc := auth.NewPairingService(db, nil)
	pr, err := svc.Start(ctx, "", "")
	require.NoError(t, err)

	_, err = svc.Approve(ctx, pr.ID, "Living Room TV", "Living room")
	apiErr, ok := domain.AsError(err)
	require.True(t, ok)
	require.Equal(t, domain.CodeInvalidRequest, apiErr.Code)
}
