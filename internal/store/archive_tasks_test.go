package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

var archiveNow = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
var archiveInput = json.RawMessage(`{"source_id":"scan-1","member_id":"child-1"}`)

func enqueueArchive(t *testing.T, db *store.DB, key string) *domain.ArchiveTask {
	t.Helper()
	task, replay, err := db.ArchiveTasksWithClock(&archiveClock{now: archiveNow}).Enqueue(context.Background(), archiveActor(t, db), "home-1", "document.extract", key, archiveInput)
	require.NoError(t, err)
	require.False(t, replay)
	require.NotEmpty(t, task.ID)
	require.Equal(t, domain.ArchiveTaskQueued, task.Status)
	require.EqualValues(t, 1, task.Version)
	require.EqualValues(t, 1, task.StateVersion)
	return task
}
func claimArchive(t *testing.T, db *store.DB, now time.Time) *domain.ArchiveTask {
	t.Helper()
	task, err := db.ArchiveTasksWithClock(&archiveClock{now: now}).Claim(context.Background(), "home-1", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.NotEmpty(t, task.LeaseToken)
	return task
}
func archiveCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	require.Error(t, err)
	e, ok := domain.AsError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, code, e.Code)
}
func archiveUpdate() domain.ArchiveTaskUpdate {
	return domain.ArchiveTaskUpdate{ExpectedStateVersion: 1, Stage: "extracted", StateJSON: json.RawMessage(`{"pages":2}`), ResultJSON: json.RawMessage(`{"document_id":"doc-1"}`)}
}

func TestArchiveTasksConcurrentClaimHasOneWinner(t *testing.T) {
	db := testutil.NewDB(t)
	task := enqueueArchive(t, db, "request-1")
	ctx := context.Background()
	type outcome struct {
		task *domain.ArchiveTask
		err  error
	}
	results := make(chan outcome, 12)
	for i := 0; i < 12; i++ {
		go func() {
			task, err := db.ArchiveTasksWithClock(&archiveClock{now: archiveNow}).Claim(ctx, "home-1", time.Minute)
			results <- outcome{task, err}
		}()
	}
	winners := 0
	for i := 0; i < 12; i++ {
		out := <-results
		require.NoError(t, out.err)
		if out.task != nil {
			winners++
			require.Equal(t, task.ID, out.task.ID)
			require.Equal(t, 1, out.task.Attempts)
		}
	}
	require.Equal(t, 1, winners)
	events, err := db.ArchiveTasks().Events(ctx, archiveActor(t, db), "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "claimed", events[1].Kind)
}

func TestArchiveTasksExpiryReclaimAndRenewal(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	ctx := context.Background()
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	first := claimArchive(t, db, archiveNow)
	fake.Set(archiveNow.Add(time.Minute))
	_, err := r.Renew(ctx, "home-1", first.ID, first.LeaseToken, time.Minute)
	archiveCode(t, err, domain.CodeConflict)
	fake.Set(archiveNow.Add(time.Minute))
	_, err = r.Finish(ctx, "home-1", first.ID, first.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
	archiveCode(t, err, domain.CodeConflict)
	fake.Set(archiveNow.Add(time.Minute))
	_, err = r.Retry(ctx, "home-1", first.ID, first.LeaseToken, archiveUpdate(), archiveNow.Add(2*time.Minute))
	archiveCode(t, err, domain.CodeConflict)
	fake.Set(archiveNow.Add(time.Minute))
	_, err = r.Wait(ctx, "home-1", first.ID, first.LeaseToken, archiveUpdate())
	archiveCode(t, err, domain.CodeConflict)
	second := claimArchive(t, db, archiveNow.Add(time.Minute))
	require.NotEqual(t, first.LeaseToken, second.LeaseToken)
	require.Equal(t, 2, second.Attempts)
	fake.Set(archiveNow.Add(time.Minute))
	_, err = r.Finish(ctx, "home-1", first.ID, first.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
	archiveCode(t, err, domain.CodeConflict)
	fake.Set(archiveNow.Add(90 * time.Second))
	renewed, err := r.Renew(ctx, "home-1", second.ID, second.LeaseToken, 2*time.Minute)
	require.NoError(t, err)
	require.Equal(t, archiveNow.Add(210*time.Second), *renewed.LeaseExpiresAt)
	fake.Set(archiveNow.Add(2 * time.Minute))
	none, err := r.Claim(ctx, "home-1", time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	fake.Set(archiveNow.Add(2 * time.Minute))
	done, err := r.Finish(ctx, "home-1", second.ID, second.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
	require.Empty(t, done.LeaseToken)
	require.Nil(t, done.LeaseExpiresAt)
	require.JSONEq(t, `{"document_id":"doc-1"}`, string(done.ResultJSON))
	require.JSONEq(t, `{"pages":2}`, string(done.StateJSON))
	events, err := r.Events(ctx, archiveActor(t, db), "home-1", done.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 5)
	require.Equal(t, done.Version, events[4].Version)
	require.Equal(t, done.ResultJSON, events[4].ResultJSON)
}

func TestArchiveTasksIdempotencyAndHouseholdIsolation(t *testing.T) {
	db := testutil.NewDB(t)
	original := enqueueArchive(t, db, "request-1")
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	ctx := context.Background()
	fake.Set(archiveNow.Add(time.Hour))
	duplicate, replay, err := r.Enqueue(ctx, archiveActor(t, db), "home-1", "document.extract", "request-1", json.RawMessage(` { "member_id":"child-1", "source_id":"scan-1" } `))
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, original.ID, duplicate.ID)
	for _, in := range []struct {
		kind  string
		input json.RawMessage
	}{{"document.index", archiveInput}, {"document.extract", json.RawMessage(`{"source_id":"scan-2"}`)}} {
		fake.Set(archiveNow)
		_, _, err = r.Enqueue(ctx, archiveActor(t, db), "home-1", in.kind, "request-1", in.input)
		archiveCode(t, err, domain.CodeIdempotencyConflict)
	}
	fake.Set(archiveNow)
	other, _, err := r.Enqueue(ctx, archiveActor(t, db), "home-2", "document.extract", "request-1", archiveInput)
	require.NoError(t, err)
	require.NotEqual(t, original.ID, other.ID)
	claimed := claimArchive(t, db, archiveNow)
	_, err = r.Get(ctx, archiveActor(t, db), "home-2", original.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = r.Events(ctx, archiveActor(t, db), "home-2", original.ID, 0, 10)
	require.ErrorIs(t, err, domain.ErrNotFound)
	fake.Set(archiveNow)
	_, err = r.Renew(ctx, "home-2", original.ID, claimed.LeaseToken, time.Minute)
	require.ErrorIs(t, err, domain.ErrNotFound)
	fake.Set(archiveNow)
	_, err = r.Finish(ctx, "home-2", original.ID, claimed.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
	require.ErrorIs(t, err, domain.ErrNotFound)
	fake.Set(archiveNow)
	_, err = r.Retry(ctx, "home-2", original.ID, claimed.LeaseToken, archiveUpdate(), archiveNow.Add(time.Minute))
	require.ErrorIs(t, err, domain.ErrNotFound)
	fake.Set(archiveNow)
	_, err = r.Wait(ctx, "home-2", original.ID, claimed.LeaseToken, archiveUpdate())
	require.ErrorIs(t, err, domain.ErrNotFound)
	fake.Set(archiveNow)
	_, err = r.Cancel(ctx, archiveActor(t, db), "home-2", original.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	fake.Set(archiveNow)
	_, _, err = r.ResolveWaiting(ctx, archiveActor(t, db), "home-2", original.ID, claimed.Version, "answer-1", json.RawMessage(`{}`))
	require.ErrorIs(t, err, domain.ErrNotFound)
	fake.Set(archiveNow)
	otherClaim, err := r.Claim(ctx, "home-2", time.Minute)
	require.NoError(t, err)
	require.Equal(t, other.ID, otherClaim.ID)
}

func TestArchiveTasksCancellationInvalidatesLease(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	fake.Set(archiveNow.Add(time.Second))
	cancelled, err := r.Cancel(ctx, archiveActor(t, db), "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskCancelled, cancelled.Status)
	require.Empty(t, cancelled.LeaseToken)
	fake.Set(archiveNow.Add(2 * time.Second))
	_, err = r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
	archiveCode(t, err, domain.CodeConflict)
	fake.Set(archiveNow.Add(2 * time.Second))
	_, err = r.Renew(ctx, "home-1", task.ID, task.LeaseToken, time.Minute)
	archiveCode(t, err, domain.CodeConflict)
	fake.Set(archiveNow.Add(2 * time.Second))
	again, err := r.Cancel(ctx, archiveActor(t, db), "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, cancelled.Version, again.Version)
	fake.Set(archiveNow.Add(time.Hour))
	none, err := r.Claim(ctx, "home-1", time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestArchiveTasksWaitingResolutionAndReplay(t *testing.T) {
	db := testutil.NewDB(t)
	original := enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	fake.Set(archiveNow.Add(time.Second))
	waiting, err := r.Wait(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate())
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskWaitingInput, waiting.Status)
	require.Equal(t, task.StateVersion+1, waiting.StateVersion)
	require.Empty(t, waiting.LeaseToken)
	fake.Set(archiveNow.Add(time.Hour))
	none, err := r.Claim(ctx, "home-1", time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	answer := json.RawMessage(`{"member_id":"child-2"}`)
	fake.Set(archiveNow.Add(2 * time.Second))
	_, _, err = r.ResolveWaiting(ctx, archiveActor(t, db), "home-1", task.ID, task.Version, "answer-1", answer)
	archiveCode(t, err, domain.CodeConflict)
	fake.Set(archiveNow.Add(2 * time.Second))
	resolved, replay, err := r.ResolveWaiting(ctx, archiveActor(t, db), "home-1", task.ID, waiting.Version, "answer-1", answer)
	require.NoError(t, err)
	require.False(t, replay)
	require.Equal(t, original.ID, resolved.ID)
	require.Equal(t, original.InputJSON, resolved.InputJSON)
	require.Equal(t, original.RequestKey, resolved.RequestKey)
	require.Equal(t, domain.ArchiveTaskQueued, resolved.Status)
	require.Equal(t, waiting.StateVersion+1, resolved.StateVersion)
	require.JSONEq(t, string(answer), string(resolved.ClarificationJSON))
	fake.Set(archiveNow.Add(3 * time.Second))
	_, _, err = r.ResolveWaiting(ctx, archiveActor(t, db), "home-1", task.ID, waiting.Version, "answer-2", answer)
	archiveCode(t, err, domain.CodeConflict)
	fake.Set(archiveNow.Add(3 * time.Second))
	_, _, err = r.ResolveWaiting(ctx, archiveActor(t, db), "home-1", task.ID, waiting.Version, "answer-1", json.RawMessage(`{"member_id":"child-3"}`))
	archiveCode(t, err, domain.CodeIdempotencyConflict)
	fake.Set(archiveNow.Add(3 * time.Second))
	_, _, err = r.ResolveWaiting(ctx, archiveActor(t, db), "home-1", task.ID, waiting.Version+1, "answer-1", answer)
	archiveCode(t, err, domain.CodeIdempotencyConflict)
	resumed := claimArchive(t, db, archiveNow.Add(3*time.Second))
	require.Equal(t, 2, resumed.Attempts)
	fake.Set(archiveNow.Add(4 * time.Second))
	replayed, replay, err := r.ResolveWaiting(ctx, archiveActor(t, db), "home-1", task.ID, waiting.Version, "answer-1", answer)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, resumed.Version, replayed.Version)
	require.Equal(t, resolved.StateVersion, replayed.StateVersion)
	fake.Set(archiveNow.Add(5 * time.Second))
	intake, replay, err := r.Enqueue(ctx, archiveActor(t, db), "home-1", "document.extract", "request-1", archiveInput)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, resumed.ID, intake.ID)
	events, err := r.Events(ctx, archiveActor(t, db), "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 5)
	require.Equal(t, "resolved", events[3].Kind)
	require.Equal(t, answer, events[3].ClarificationJSON)
	require.Equal(t, resolved.StateVersion, events[3].StateVersion)
	page, err := r.Events(ctx, archiveActor(t, db), "home-1", task.ID, 2, 2)
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.EqualValues(t, 3, page[0].Version)
}

func TestArchiveTasksRetryAndDiskReopenRetainEvents(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	update := archiveUpdate()
	update.LastErrorCode = "provider_unavailable"
	fake.Set(archiveNow.Add(time.Second))
	retry, err := r.Retry(ctx, "home-1", task.ID, task.LeaseToken, update, archiveNow.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskRetryWait, retry.Status)
	require.Empty(t, retry.LeaseToken)
	require.NoError(t, db.Close())
	reopened, err := store.Open(ctx, db.Path())
	require.NoError(t, err)
	defer reopened.Close()
	r = reopened.ArchiveTasksWithClock(fake)
	db = reopened
	got, err := r.Get(ctx, archiveActor(t, db), "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, retry, got)
	fake.Set(archiveNow.Add(59 * time.Minute))
	none, err := r.Claim(ctx, "home-1", time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	resumed := claimArchive(t, reopened, archiveNow.Add(time.Hour))
	require.Equal(t, 2, resumed.Attempts)
	update.LastErrorCode = "unreadable_document"
	update.ExpectedStateVersion = resumed.StateVersion
	fake.Set(archiveNow.Add(time.Hour + time.Second))
	failed, err := r.Finish(ctx, "home-1", resumed.ID, resumed.LeaseToken, domain.ArchiveTaskFailed, update)
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskFailed, failed.Status)
	events, err := r.Events(ctx, archiveActor(t, db), "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 5)
	require.Equal(t, "provider_unavailable", events[2].LastErrorCode)
	fake.Set(archiveNow.Add(2 * time.Hour))
	_, err = r.Cancel(ctx, archiveActor(t, db), "home-1", task.ID)
	archiveCode(t, err, domain.CodeConflict)
}

func TestArchiveTasksRejectInvalidRequestsWithoutMutation(t *testing.T) {
	db := testutil.NewDB(t)
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	ctx := context.Background()
	for i, in := range []struct {
		household, kind, key, input string
		now                         time.Time
	}{
		{"", "extract", "key", "{}", archiveNow}, {"home-1", "../extract", "key", "{}", archiveNow}, {"home-1", "extract", " ", "{}", archiveNow},
		{"home-1", "extract", strings.Repeat("k", 129), "{}", archiveNow}, {"home-1", "extract", "key", "[]", archiveNow}, {"home-1", "extract", "key", "{}", time.Time{}},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			fake.Set(in.now)
			_, _, err := r.Enqueue(ctx, archiveActor(t, db), in.household, in.kind, in.key, json.RawMessage(in.input))
			archiveCode(t, err, domain.CodeInvalidRequest)
		})
	}
	original := enqueueArchive(t, db, "request-1")
	for _, ttl := range []time.Duration{0, -time.Second, 11 * time.Minute} {
		fake.Set(archiveNow)
		_, err := r.Claim(ctx, "home-1", ttl)
		archiveCode(t, err, domain.CodeInvalidRequest)
	}
	task := claimArchive(t, db, archiveNow)
	fake.Set(archiveNow)
	_, err := r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskQueued, archiveUpdate())
	archiveCode(t, err, domain.CodeInvalidRequest)
	invalid := archiveUpdate()
	invalid.StateJSON = json.RawMessage(`[]`)
	fake.Set(archiveNow)
	_, err = r.Wait(ctx, "home-1", task.ID, task.LeaseToken, invalid)
	archiveCode(t, err, domain.CodeInvalidRequest)
	fake.Set(archiveNow)
	_, err = r.Retry(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate(), archiveNow.Add(-time.Second))
	archiveCode(t, err, domain.CodeInvalidRequest)
	fake.Set(archiveNow)
	_, err = r.Retry(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate(), archiveNow.Add(31*24*time.Hour))
	archiveCode(t, err, domain.CodeInvalidRequest)
	fake.Set(archiveNow)
	_, err = r.Renew(ctx, "home-1", task.ID, "", time.Minute)
	archiveCode(t, err, domain.CodeInvalidRequest)
	_, err = r.Events(ctx, archiveActor(t, db), "home-1", original.ID, -1, 10)
	archiveCode(t, err, domain.CodeInvalidRequest)
	_, err = r.Events(ctx, archiveActor(t, db), "home-1", original.ID, 0, 1001)
	archiveCode(t, err, domain.CodeInvalidRequest)
	got, err := r.Get(ctx, archiveActor(t, db), "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, task, got)
	events, err := r.Events(ctx, archiveActor(t, db), "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 2)
}

func TestArchiveTasksEventFailureRollsBackStateAndResult(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	_, err := db.SQL().ExecContext(ctx, `CREATE TRIGGER reject_archive_events BEFORE INSERT ON archive_task_events BEGIN SELECT RAISE(ABORT,'event write failed'); END`)
	require.NoError(t, err)
	fake.Set(archiveNow.Add(time.Second))
	_, err = r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
	require.ErrorContains(t, err, "event write failed")
	unchanged, err := r.Get(ctx, archiveActor(t, db), "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, task, unchanged)
}

func TestArchiveTasksCheckpointAndAttemptHistorySurviveReclaim(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	first := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	require.NotEmpty(t, first.AttemptID)
	require.NotEqual(t, first.LeaseToken, first.AttemptID)
	fake.Set(archiveNow.Add(time.Second))
	checkpoint, err := r.Checkpoint(ctx, "home-1", first.ID, first.LeaseToken, archiveUpdate())
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskRunning, checkpoint.Status)
	require.Equal(t, first.AttemptID, checkpoint.AttemptID)
	require.NoError(t, db.Close())
	reopened, err := store.Open(ctx, db.Path())
	require.NoError(t, err)
	defer reopened.Close()
	r = reopened.ArchiveTasksWithClock(fake)
	db = reopened
	next := claimArchive(t, reopened, archiveNow.Add(time.Minute))
	require.NotEqual(t, first.AttemptID, next.AttemptID)
	require.Equal(t, checkpoint.StateJSON, next.StateJSON)
	require.Equal(t, checkpoint.StateVersion, next.StateVersion)
	require.Equal(t, checkpoint.ResultJSON, next.ResultJSON)
	require.Equal(t, "extracted", next.Stage)
	fake.Set(archiveNow.Add(time.Minute))
	_, err = r.Checkpoint(ctx, "home-1", first.ID, first.LeaseToken, archiveUpdate())
	archiveCode(t, err, domain.CodeConflict)
	fake.Set(archiveNow.Add(time.Minute))
	_, err = r.Checkpoint(ctx, "home-2", next.ID, next.LeaseToken, archiveUpdate())
	require.ErrorIs(t, err, domain.ErrNotFound)
	events, err := r.Events(ctx, archiveActor(t, db), "home-1", next.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 4)
	require.Equal(t, first.AttemptID, events[1].AttemptID)
	require.Equal(t, first.AttemptID, events[2].AttemptID)
	require.Equal(t, next.AttemptID, events[3].AttemptID)
	require.Equal(t, checkpoint.StateJSON, events[2].StateJSON)
	require.Equal(t, next.StateJSON, events[3].StateJSON)
	require.Equal(t, checkpoint.InputJSON, events[2].InputJSON)
}

func TestArchiveTasksStaleSameLeaseUpdateCannotReplaceNewerCheckpoint(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	fake := &archiveClock{now: archiveNow}
	r := db.ArchiveTasksWithClock(fake)
	ctx := context.Background()
	newer := archiveUpdate()
	newer.StateJSON = json.RawMessage(`{"completed":["parse","extract"]}`)
	fake.Set(archiveNow.Add(time.Second))
	committed, err := r.Checkpoint(ctx, "home-1", task.ID, task.LeaseToken, newer)
	require.NoError(t, err)
	stale := archiveUpdate()
	stale.StateJSON = json.RawMessage(`{"completed":["parse"]}`)
	fake.Set(archiveNow.Add(2 * time.Second))
	for _, operation := range []string{"checkpoint", "finish", "retry", "wait"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			switch operation {
			case "checkpoint":
				_, err = r.Checkpoint(ctx, "home-1", task.ID, task.LeaseToken, stale)
			case "finish":
				_, err = r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskSucceeded, stale)
			case "retry":
				_, err = r.Retry(ctx, "home-1", task.ID, task.LeaseToken, stale, archiveNow.Add(time.Hour))
			case "wait":
				_, err = r.Wait(ctx, "home-1", task.ID, task.LeaseToken, stale)
			}
			archiveCode(t, err, domain.CodeConflict)
		})
	}
	current, err := r.Get(ctx, archiveActor(t, db), "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, committed, current)
}

func TestArchiveTasksWaitForWriteLockRechecksLeaseExpiry(t *testing.T) {
	for _, operation := range []string{"finish", "renew"} {
		t.Run(operation, func(t *testing.T) {
			db := testutil.NewDB(t)
			enqueueArchive(t, db, "request-1")
			task := claimArchive(t, db, archiveNow)
			fake := &archiveClock{now: archiveNow}
			r := db.ArchiveTasksWithClock(fake)
			ctx := context.Background()
			db.SQL().SetMaxOpenConns(1)
			lock, err := db.BeginWrite(ctx)
			require.NoError(t, err)
			defer lock.Rollback()
			before := db.SQL().Stats().WaitCount
			result := make(chan error, 1)
			go func() {
				var err error
				if operation == "finish" {
					_, err = r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
				} else {
					_, err = r.Renew(ctx, "home-1", task.ID, task.LeaseToken, time.Minute)
				}
				result <- err
			}()
			require.Eventually(t, func() bool { return db.SQL().Stats().WaitCount > before }, time.Second, time.Millisecond, "writer must be blocked behind the held write transaction")
			// The caller's method-start timestamp is now stale: transaction acquisition
			// can take longer than the remaining lease, even with no competing claimant.
			fake.Set(archiveNow.Add(2 * time.Minute))
			require.NoError(t, lock.Rollback())
			archiveCode(t, <-result, domain.CodeConflict)
			current, err := r.Get(ctx, archiveActor(t, db), "home-1", task.ID)
			require.NoError(t, err)
			require.Equal(t, task, current, "expired work must not change the checkpoint or lease")
		})
	}
}

// archiveClock is safe to advance while a worker is waiting for a database lock.
type archiveClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *archiveClock) Now() time.Time    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *archiveClock) Set(now time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.now = now }

func TestArchiveTasksConcurrentSameLeaseUpdatesHaveOneWinner(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	r := db.ArchiveTasksWithClock(&archiveClock{now: archiveNow.Add(time.Second)})
	update := archiveUpdate()
	update.StateJSON = json.RawMessage(`{"completed":["parse","extract"]}`)
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func() { _, err := r.Checkpoint(ctx, "home-1", task.ID, task.LeaseToken, update); errors <- err }()
	}
	winners := 0
	for i := 0; i < 12; i++ {
		err := <-errors
		if err == nil {
			winners++
		} else {
			archiveCode(t, err, domain.CodeConflict)
		}
	}
	require.Equal(t, 1, winners)
	current, err := r.Get(ctx, archiveActor(t, db), "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, task.StateVersion+1, current.StateVersion)
	require.Equal(t, update.StateJSON, current.StateJSON)
	events, err := r.Events(ctx, archiveActor(t, db), "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, current.StateVersion, events[2].StateVersion)
}

func TestArchiveTasksRenewalPreservesInFlightStateRevision(t *testing.T) {
	db := testutil.NewDB(t)
	initial := enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	fake := &archiveClock{now: archiveNow.Add(time.Second)}
	r := db.ArchiveTasksWithClock(fake)
	require.EqualValues(t, 1, initial.StateVersion)
	require.Equal(t, initial.StateVersion, task.StateVersion)
	update := archiveUpdate()
	update.ExpectedStateVersion = task.StateVersion
	renewed, err := r.Renew(ctx, "home-1", task.ID, task.LeaseToken, 2*time.Minute)
	require.NoError(t, err)
	require.Greater(t, renewed.Version, task.Version)
	require.Equal(t, task.StateVersion, renewed.StateVersion)
	checkpoint, err := r.Checkpoint(ctx, "home-1", task.ID, task.LeaseToken, update)
	require.NoError(t, err)
	require.Equal(t, task.StateVersion+1, checkpoint.StateVersion)
	cancelled, err := r.Cancel(ctx, archiveActor(t, db), "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, checkpoint.StateVersion, cancelled.StateVersion)
	events, err := r.Events(ctx, archiveActor(t, db), "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 5)
	for i, want := range []int64{1, 1, 1, 2, 2} {
		require.Equal(t, want, events[i].StateVersion)
	}
	raw, err := json.Marshal(cancelled)
	require.NoError(t, err)
	var public map[string]any
	require.NoError(t, json.Unmarshal(raw, &public))
	require.Equal(t, float64(2), public["state_version"])
	raw, err = json.Marshal(events[4])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &public))
	require.Equal(t, float64(2), public["state_version"])
}

type archiveClockFunc func() time.Time

func (c archiveClockFunc) Now() time.Time { return c() }

func TestArchiveTasksWriteRetrySamplesClockAgain(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	// Simulate a retryable writer failure after the first callback acquired its
	// transaction. RetryBusy must invoke a fresh clock sample on the next attempt.
	_, err := db.SQL().ExecContext(ctx, `CREATE TRIGGER busy_archive_update BEFORE UPDATE ON archive_tasks BEGIN SELECT RAISE(ABORT,'database is locked'); END`)
	require.NoError(t, err)
	calls := 0
	r := db.ArchiveTasksWithClock(archiveClockFunc(func() time.Time {
		calls++
		if calls == 1 {
			return archiveNow.Add(time.Second)
		}
		return archiveNow.Add(2 * time.Minute)
	}))
	_, err = r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate())
	archiveCode(t, err, domain.CodeConflict)
	require.Equal(t, 2, calls)
	current, err := r.Get(ctx, archiveActor(t, db), "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, task, current)
}
