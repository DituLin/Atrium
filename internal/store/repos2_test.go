package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
)

func TestJobsDedupClaimAndRelease(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "family_photos")
	repo := db.Jobs()

	job := &domain.Job{Kind: domain.JobExtractMeta, PhotoID: "photo1", Priority: 10, NextRunAt: now}
	require.NoError(t, repo.Enqueue(ctx, job))
	dup := &domain.Job{Kind: domain.JobExtractMeta, PhotoID: "photo1", NextRunAt: now}
	require.ErrorIs(t, repo.Enqueue(ctx, dup), domain.ErrConflict)

	low := &domain.Job{Kind: domain.JobBuildPreview, PhotoID: "photo1", Priority: 5, NextRunAt: now}
	require.NoError(t, repo.Enqueue(ctx, low))

	claimed, err := repo.Claim(ctx, "worker-1", now)
	require.NoError(t, err)
	require.Equal(t, domain.JobExtractMeta, claimed.Kind, "higher priority is claimed first")
	require.Equal(t, 1, claimed.Attempts)

	// A future job is not claimable yet.
	future := &domain.Job{Kind: domain.JobRecomputeDay, NextRunAt: now.Add(time.Hour)}
	require.NoError(t, repo.Enqueue(ctx, future))

	next, err := repo.Claim(ctx, "worker-2", now)
	require.NoError(t, err)
	require.Equal(t, domain.JobBuildPreview, next.Kind)

	_, err = repo.Claim(ctx, "worker-3", now)
	require.ErrorIs(t, err, domain.ErrNotFound)

	require.NoError(t, repo.Complete(ctx, claimed.ID, now))
	require.NoError(t, repo.Retry(ctx, next.ID, now.Add(time.Minute), "decode_failed", now))

	released, err := repo.ReleaseRunning(ctx, now)
	require.NoError(t, err)
	require.Zero(t, released)

	counts, err := repo.Counts(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[domain.JobDone])
	require.EqualValues(t, 2, counts[domain.JobQueued])

	// A crash leaves jobs running; startup returns them to the queue.
	running, err := repo.Claim(ctx, "worker-4", now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, domain.JobRunning, running.Status)
	n, err := repo.ReleaseRunning(ctx, now)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	require.NoError(t, repo.Fail(ctx, running.ID, "too_large", now))
	got, err := repo.Get(ctx, running.ID)
	require.NoError(t, err)
	require.Equal(t, domain.JobFailed, got.Status)
	require.Equal(t, "too_large", got.LastError)
}

func TestScreensLifecycle(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	repo := db.Screens()

	sc := &domain.Screen{ID: "living_room_tv", Name: "Living room", TokenHash: "hash1", CreatedAt: now, ApprovedAt: now}
	require.NoError(t, repo.Create(ctx, sc))
	require.ErrorIs(t, repo.Create(ctx, &domain.Screen{ID: "living_room_tv", Name: "x", TokenHash: "hash2"}), domain.ErrConflict)

	got, err := repo.GetByTokenHash(ctx, "hash1")
	require.NoError(t, err)
	require.Equal(t, "living_room_tv", got.ID)
	require.Equal(t, domain.ScreenActive, got.Status)

	require.NoError(t, repo.TouchSeen(ctx, sc.ID, "192.168.1.20", "0.1.0+abc", now))
	route := &domain.RouteState{Name: domain.RoutePhotos, Collection: "recent"}
	require.NoError(t, repo.SetRoute(ctx, sc.ID, route, 3, now))

	got, err = repo.Get(ctx, sc.ID)
	require.NoError(t, err)
	require.NotNil(t, got.CurrentRoute)
	require.Equal(t, domain.RoutePhotos, got.CurrentRoute.Name)
	require.EqualValues(t, 3, got.AppliedSequence)
	require.Equal(t, "0.1.0+abc", got.ClientVersion)

	require.NoError(t, repo.Revoke(ctx, sc.ID, now))
	got, err = repo.Get(ctx, sc.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ScreenRevoked, got.Status)
	require.NotNil(t, got.RevokedAt)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestCommandsSequenceAndResolveOnce(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{
		ID: "living_room_tv", Name: "Living room", TokenHash: "hash1", CreatedAt: now, ApprovedAt: now,
	}))
	repo := db.Commands()

	first := &domain.Command{
		ScreenID: "living_room_tv", Kind: domain.CommandNavigate,
		Payload:  domain.CommandPayload{Route: domain.RouteDashboard},
		IssuedBy: "admin", IssuedAt: now, ExpiresAt: now.Add(10 * time.Second), Status: domain.CommandAccepted,
	}
	require.NoError(t, repo.Issue(ctx, first))
	require.EqualValues(t, 1, first.Sequence)

	second := &domain.Command{
		ScreenID: "living_room_tv", Kind: domain.CommandShow,
		Payload:  domain.CommandPayload{PhotoID: "01JPHOTO"},
		IssuedBy: "admin", IssuedAt: now, ExpiresAt: now.Add(10 * time.Second), Status: domain.CommandAccepted,
	}
	require.NoError(t, repo.Issue(ctx, second))
	require.EqualValues(t, 2, second.Sequence)

	open, err := repo.CountOpen(ctx, "living_room_tv")
	require.NoError(t, err)
	require.EqualValues(t, 2, open)

	require.NoError(t, repo.MarkDelivered(ctx, first.ID, now))
	result := &domain.CommandResult{Route: &domain.RouteState{Name: domain.RouteDashboard}}
	ok, err := repo.Resolve(ctx, first.ID, domain.CommandApplied, "", result, now)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = repo.Resolve(ctx, first.ID, domain.CommandFailed, "superseded", nil, now)
	require.NoError(t, err)
	require.False(t, ok, "a terminal command must not be resolved twice")

	got, err := repo.Get(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, domain.CommandApplied, got.Status)
	require.NotNil(t, got.Result)
	require.NotNil(t, got.Result.Route)
	require.Equal(t, domain.RouteDashboard, got.Result.Route.Name)
	require.Equal(t, domain.RouteDashboard, got.Payload.Route)

	overdue, err := repo.OpenBefore(ctx, now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, overdue, 1)

	n, err := repo.MarkAllAcceptedUnknown(ctx, "server_restart", now)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	got, err = repo.Get(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, domain.CommandUnknown, got.Status)
	require.Equal(t, "server_restart", got.ErrorCode)

	listed, err := repo.List(ctx, "living_room_tv", domain.CommandUnknown, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
}

func TestWidgetCacheAuditAndScanRuns(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "family_photos")

	wc := db.WidgetCache()
	_, err := wc.Get(ctx, domain.WidgetWeather)
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.NoError(t, wc.Put(ctx, store.CachedWidget{
		Widget: domain.WidgetWeather, Payload: `{"temperature_c":21}`,
		FetchedAt: now, ExpiresAt: now.Add(30 * time.Minute),
	}))
	cached, err := wc.Get(ctx, domain.WidgetWeather)
	require.NoError(t, err)
	require.Equal(t, `{"temperature_c":21}`, cached.Payload)
	require.NoError(t, wc.SetError(ctx, domain.WidgetWeather, "provider_unreachable"))
	cached, err = wc.Get(ctx, domain.WidgetWeather)
	require.NoError(t, err)
	require.Equal(t, "provider_unreachable", cached.Error)
	require.Equal(t, `{"temperature_c":21}`, cached.Payload, "a failure keeps the last payload")

	audit := db.Audit()
	require.NoError(t, audit.Append(ctx, &domain.AuditEntry{
		At: now, Actor: "admin", Action: "screen.revoke", Target: "living_room_tv",
	}))
	require.NoError(t, audit.Append(ctx, &domain.AuditEntry{
		At: now.Add(-8 * 24 * time.Hour), Actor: "admin", Action: "pairing.approve",
	}))
	entries, err := audit.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	pruned, err := audit.DeleteOlderThan(ctx, now.Add(-7*24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, pruned)

	runs := db.ScanRuns()
	run := &domain.ScanRun{SourceID: "family_photos", Mode: domain.ScanScheduled, StartedAt: now}
	require.NoError(t, runs.Start(ctx, run))
	run.FilesSeen = 120
	run.FilesNew = 4
	require.NoError(t, runs.UpdateCounters(ctx, run))
	require.NoError(t, runs.Finish(ctx, run.ID, domain.ScanCompleted, "", now.Add(time.Minute)))
	got, err := runs.Get(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ScanCompleted, got.Status)
	require.EqualValues(t, 120, got.FilesSeen)
	require.NotNil(t, got.FinishedAt)

	listed, err := runs.List(ctx, "family_photos", 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
}

func TestPairingsAndAdminTokens(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	pr := db.Pairings()
	p := &domain.Pairing{Code: "123456", CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute), RemoteIP: "192.168.1.20"}
	require.NoError(t, pr.Create(ctx, p))
	require.ErrorIs(t, pr.Create(ctx, &domain.Pairing{
		Code: "123456", CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}), domain.ErrConflict)

	exists, err := pr.CodeExists(ctx, "123456")
	require.NoError(t, err)
	require.True(t, exists)

	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{
		ID: "living_room_tv", Name: "Living room", TokenHash: "h", CreatedAt: now, ApprovedAt: now,
	}))
	require.NoError(t, pr.Approve(ctx, p.ID, "living_room_tv", now))
	require.ErrorIs(t, pr.Approve(ctx, p.ID, "living_room_tv", now), domain.ErrNotFound)

	require.NoError(t, pr.Claim(ctx, p.ID, now))
	require.ErrorIs(t, pr.Claim(ctx, p.ID, now), domain.ErrNotFound, "a second claim must not succeed")

	got, err := pr.GetByCode(ctx, "123456")
	require.NoError(t, err)
	require.Equal(t, domain.PairingClaimed, got.Status)
	require.NotNil(t, got.ClaimedAt)

	stale := &domain.Pairing{Code: "654321", CreatedAt: now.Add(-10 * time.Minute), ExpiresAt: now.Add(-5 * time.Minute)}
	require.NoError(t, pr.Create(ctx, stale))
	n, err := pr.ExpireOverdue(ctx, now)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	deleted, err := pr.DeleteOlderThan(ctx, now.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Zero(t, deleted)
	require.NoError(t, pr.DeleteForScreen(ctx, "living_room_tv"))
	_, err = pr.Get(ctx, p.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	at := db.AdminTokens()
	tok := &domain.AdminToken{TokenHash: "adminhash", Label: "cli", CreatedAt: now}
	require.NoError(t, at.Create(ctx, tok))
	require.ErrorIs(t, at.Create(ctx, &domain.AdminToken{TokenHash: "adminhash"}), domain.ErrConflict)
	found, err := at.GetByHash(ctx, "adminhash")
	require.NoError(t, err)
	require.Equal(t, "cli", found.Label)
	require.NoError(t, at.TouchUsed(ctx, tok.ID, now))
	live, err := at.CountLive(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, live)
	revoked, err := at.RevokeAll(ctx, now)
	require.NoError(t, err)
	require.EqualValues(t, 1, revoked)
	live, err = at.CountLive(ctx)
	require.NoError(t, err)
	require.Zero(t, live)
}
