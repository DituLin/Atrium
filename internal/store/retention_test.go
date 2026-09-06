package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
)

// TestRetentionDeletesHistoryAndKeepsTheRecent covers design §5: operational
// history is pruned after a week, pairings after a day, and nothing that is
// system state is touched.
func TestRetentionDeletesHistoryAndKeepsTheRecent(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * 24 * time.Hour)
	recent := now.Add(-2 * 24 * time.Hour)

	seedSource(t, db, "family_photos")
	require.NoError(t, db.Screens().Create(ctx, &domain.Screen{
		ID: "living_room_tv", Name: "Living room", TokenHash: "hash",
		Status: domain.ScreenActive, CreatedAt: old, ApprovedAt: old,
	}))

	// Commands: one old, one recent.
	for _, at := range []time.Time{old, recent} {
		cmd := &domain.Command{
			ScreenID: "living_room_tv", Kind: domain.CommandRefresh, IssuedBy: "admin",
			IssuedAt: at, ExpiresAt: at.Add(10 * time.Second), Status: domain.CommandApplied,
		}
		require.NoError(t, db.Commands().Issue(ctx, cmd))
	}
	// Scan runs.
	for _, at := range []time.Time{old, recent} {
		run := &domain.ScanRun{
			SourceID: "family_photos", Mode: domain.ScanScheduled,
			Status: domain.ScanCompleted, StartedAt: at,
		}
		require.NoError(t, db.ScanRuns().Start(ctx, run))
	}
	// Audit entries.
	for _, at := range []time.Time{old, recent} {
		require.NoError(t, db.Audit().Append(ctx, &domain.AuditEntry{
			At: at, Actor: "admin", Action: "screen.revoke", Target: "living_room_tv",
		}))
	}
	// Jobs: one finished long ago, one still queued.
	doneJob := &domain.Job{
		Kind: domain.JobExtractMeta, SourceID: "family_photos", Status: domain.JobQueued,
		NextRunAt: old, CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Jobs().Enqueue(ctx, doneJob))
	require.NoError(t, db.Jobs().Complete(ctx, doneJob.ID, old))
	queued := &domain.Job{
		Kind: domain.JobBuildPreview, SourceID: "family_photos", Status: domain.JobQueued,
		NextRunAt: old, CreatedAt: old, UpdatedAt: old,
	}
	require.NoError(t, db.Jobs().Enqueue(ctx, queued))
	// Pairings: yesterday's is history, today's is live.
	for i, at := range []time.Time{now.Add(-48 * time.Hour), now.Add(-time.Minute)} {
		require.NoError(t, db.Pairings().Create(ctx, &domain.Pairing{
			Code: []string{"111111", "222222"}[i], Status: domain.PairingPending,
			ClientHint: "tv", RemoteIP: "127.0.0.1", CreatedAt: at, ExpiresAt: at.Add(5 * time.Minute),
		}))
	}

	res, err := db.RunRetention(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 1, res.Commands)
	assert.EqualValues(t, 1, res.ScanRuns)
	assert.EqualValues(t, 1, res.AuditLog)
	assert.EqualValues(t, 1, res.Jobs, "only terminal jobs are pruned")
	assert.EqualValues(t, 1, res.Pairings)
	assert.EqualValues(t, 5, res.Total())

	cmds, err := db.Commands().List(ctx, "living_room_tv", "", 50)
	require.NoError(t, err)
	assert.Len(t, cmds, 1)
	runs, err := db.ScanRuns().List(ctx, "family_photos", 50)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
	entries, err := db.Audit().List(ctx, 50)
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	// The queued job survived; system state is untouched.
	stillQueued, err := db.Jobs().Get(ctx, queued.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobQueued, stillQueued.Status)
	screens, err := db.Screens().List(ctx)
	require.NoError(t, err)
	assert.Len(t, screens, 1)
}

func TestRetentionWindowsMatchTheDesign(t *testing.T) {
	assert.Equal(t, 7*24*time.Hour, store.HistoryRetention)
	assert.Equal(t, 24*time.Hour, store.PairingRetention)
}
