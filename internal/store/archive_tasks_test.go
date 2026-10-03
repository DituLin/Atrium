package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	task, replay, err := db.ArchiveTasks().Enqueue(context.Background(), "home-1", "document.extract", key, archiveInput, archiveNow)
	require.NoError(t, err)
	require.False(t, replay)
	require.NotEmpty(t, task.ID)
	require.Equal(t, domain.ArchiveTaskQueued, task.Status)
	require.EqualValues(t, 1, task.Version)
	return task
}
func claimArchive(t *testing.T, db *store.DB, now time.Time) *domain.ArchiveTask {
	t.Helper()
	task, err := db.ArchiveTasks().Claim(context.Background(), "home-1", now, time.Minute)
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
	return domain.ArchiveTaskUpdate{Stage: "extracted", StateJSON: json.RawMessage(`{"pages":2}`), ResultJSON: json.RawMessage(`{"document_id":"doc-1"}`)}
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
			task, err := db.ArchiveTasks().Claim(ctx, "home-1", archiveNow, time.Minute)
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
	events, err := db.ArchiveTasks().Events(ctx, "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "claimed", events[1].Kind)
}

func TestArchiveTasksExpiryReclaimAndRenewal(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	ctx := context.Background()
	r := db.ArchiveTasks()
	first := claimArchive(t, db, archiveNow)
	_, err := r.Renew(ctx, "home-1", first.ID, first.LeaseToken, archiveNow.Add(time.Minute), time.Minute)
	archiveCode(t, err, domain.CodeConflict)
	_, err = r.Finish(ctx, "home-1", first.ID, first.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate(), archiveNow.Add(time.Minute))
	archiveCode(t, err, domain.CodeConflict)
	_, err = r.Retry(ctx, "home-1", first.ID, first.LeaseToken, archiveUpdate(), archiveNow.Add(2*time.Minute), archiveNow.Add(time.Minute))
	archiveCode(t, err, domain.CodeConflict)
	_, err = r.Wait(ctx, "home-1", first.ID, first.LeaseToken, archiveUpdate(), archiveNow.Add(time.Minute))
	archiveCode(t, err, domain.CodeConflict)
	second := claimArchive(t, db, archiveNow.Add(time.Minute))
	require.NotEqual(t, first.LeaseToken, second.LeaseToken)
	require.Equal(t, 2, second.Attempts)
	_, err = r.Finish(ctx, "home-1", first.ID, first.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate(), archiveNow.Add(time.Minute))
	archiveCode(t, err, domain.CodeConflict)
	renewed, err := r.Renew(ctx, "home-1", second.ID, second.LeaseToken, archiveNow.Add(90*time.Second), 2*time.Minute)
	require.NoError(t, err)
	require.Equal(t, archiveNow.Add(210*time.Second), *renewed.LeaseExpiresAt)
	none, err := r.Claim(ctx, "home-1", archiveNow.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	done, err := r.Finish(ctx, "home-1", second.ID, second.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate(), archiveNow.Add(2*time.Minute))
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskSucceeded, done.Status)
	require.Empty(t, done.LeaseToken)
	require.Nil(t, done.LeaseExpiresAt)
	require.JSONEq(t, `{"document_id":"doc-1"}`, string(done.ResultJSON))
	require.JSONEq(t, `{"pages":2}`, string(done.StateJSON))
	events, err := r.Events(ctx, "home-1", done.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 5)
	require.Equal(t, done.Version, events[4].Version)
	require.Equal(t, done.ResultJSON, events[4].ResultJSON)
}

func TestArchiveTasksIdempotencyAndHouseholdIsolation(t *testing.T) {
	db := testutil.NewDB(t)
	original := enqueueArchive(t, db, "request-1")
	r := db.ArchiveTasks()
	ctx := context.Background()
	duplicate, replay, err := r.Enqueue(ctx, "home-1", "document.extract", "request-1", json.RawMessage(` { "member_id":"child-1", "source_id":"scan-1" } `), archiveNow.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, original.ID, duplicate.ID)
	for _, in := range []struct {
		kind  string
		input json.RawMessage
	}{{"document.index", archiveInput}, {"document.extract", json.RawMessage(`{"source_id":"scan-2"}`)}} {
		_, _, err = r.Enqueue(ctx, "home-1", in.kind, "request-1", in.input, archiveNow)
		archiveCode(t, err, domain.CodeIdempotencyConflict)
	}
	other, _, err := r.Enqueue(ctx, "home-2", "document.extract", "request-1", archiveInput, archiveNow)
	require.NoError(t, err)
	require.NotEqual(t, original.ID, other.ID)
	claimed := claimArchive(t, db, archiveNow)
	_, err = r.Get(ctx, "home-2", original.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = r.Events(ctx, "home-2", original.ID, 0, 10)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = r.Renew(ctx, "home-2", original.ID, claimed.LeaseToken, archiveNow, time.Minute)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = r.Finish(ctx, "home-2", original.ID, claimed.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate(), archiveNow)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = r.Retry(ctx, "home-2", original.ID, claimed.LeaseToken, archiveUpdate(), archiveNow.Add(time.Minute), archiveNow)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = r.Wait(ctx, "home-2", original.ID, claimed.LeaseToken, archiveUpdate(), archiveNow)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = r.Cancel(ctx, "home-2", original.ID, archiveNow)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, _, err = r.ResolveWaiting(ctx, "home-2", original.ID, claimed.Version, "answer-1", json.RawMessage(`{}`), archiveNow)
	require.ErrorIs(t, err, domain.ErrNotFound)
	otherClaim, err := r.Claim(ctx, "home-2", archiveNow, time.Minute)
	require.NoError(t, err)
	require.Equal(t, other.ID, otherClaim.ID)
}

func TestArchiveTasksCancellationInvalidatesLease(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	r := db.ArchiveTasks()
	cancelled, err := r.Cancel(ctx, "home-1", task.ID, archiveNow.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskCancelled, cancelled.Status)
	require.Empty(t, cancelled.LeaseToken)
	_, err = r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate(), archiveNow.Add(2*time.Second))
	archiveCode(t, err, domain.CodeConflict)
	_, err = r.Renew(ctx, "home-1", task.ID, task.LeaseToken, archiveNow.Add(2*time.Second), time.Minute)
	archiveCode(t, err, domain.CodeConflict)
	again, err := r.Cancel(ctx, "home-1", task.ID, archiveNow.Add(2*time.Second))
	require.NoError(t, err)
	require.Equal(t, cancelled.Version, again.Version)
	none, err := r.Claim(ctx, "home-1", archiveNow.Add(time.Hour), time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestArchiveTasksWaitingResolutionAndReplay(t *testing.T) {
	db := testutil.NewDB(t)
	original := enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	r := db.ArchiveTasks()
	waiting, err := r.Wait(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate(), archiveNow.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskWaitingInput, waiting.Status)
	require.Empty(t, waiting.LeaseToken)
	none, err := r.Claim(ctx, "home-1", archiveNow.Add(time.Hour), time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	answer := json.RawMessage(`{"member_id":"child-2"}`)
	_, _, err = r.ResolveWaiting(ctx, "home-1", task.ID, task.Version, "answer-1", answer, archiveNow.Add(2*time.Second))
	archiveCode(t, err, domain.CodeConflict)
	resolved, replay, err := r.ResolveWaiting(ctx, "home-1", task.ID, waiting.Version, "answer-1", answer, archiveNow.Add(2*time.Second))
	require.NoError(t, err)
	require.False(t, replay)
	require.Equal(t, original.ID, resolved.ID)
	require.Equal(t, original.InputJSON, resolved.InputJSON)
	require.Equal(t, original.RequestKey, resolved.RequestKey)
	require.Equal(t, domain.ArchiveTaskQueued, resolved.Status)
	require.JSONEq(t, string(answer), string(resolved.ClarificationJSON))
	_, _, err = r.ResolveWaiting(ctx, "home-1", task.ID, waiting.Version, "answer-2", answer, archiveNow.Add(3*time.Second))
	archiveCode(t, err, domain.CodeConflict)
	_, _, err = r.ResolveWaiting(ctx, "home-1", task.ID, waiting.Version, "answer-1", json.RawMessage(`{"member_id":"child-3"}`), archiveNow.Add(3*time.Second))
	archiveCode(t, err, domain.CodeIdempotencyConflict)
	_, _, err = r.ResolveWaiting(ctx, "home-1", task.ID, waiting.Version+1, "answer-1", answer, archiveNow.Add(3*time.Second))
	archiveCode(t, err, domain.CodeIdempotencyConflict)
	resumed := claimArchive(t, db, archiveNow.Add(3*time.Second))
	require.Equal(t, 2, resumed.Attempts)
	replayed, replay, err := r.ResolveWaiting(ctx, "home-1", task.ID, waiting.Version, "answer-1", answer, archiveNow.Add(4*time.Second))
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, resumed.Version, replayed.Version)
	intake, replay, err := r.Enqueue(ctx, "home-1", "document.extract", "request-1", archiveInput, archiveNow.Add(5*time.Second))
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, resumed.ID, intake.ID)
	events, err := r.Events(ctx, "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 5)
	require.Equal(t, "resolved", events[3].Kind)
	require.Equal(t, answer, events[3].ClarificationJSON)
	page, err := r.Events(ctx, "home-1", task.ID, 2, 2)
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.EqualValues(t, 3, page[0].Version)
}

func TestArchiveTasksRetryAndDiskReopenRetainEvents(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	r := db.ArchiveTasks()
	update := archiveUpdate()
	update.LastErrorCode = "provider_unavailable"
	retry, err := r.Retry(ctx, "home-1", task.ID, task.LeaseToken, update, archiveNow.Add(time.Hour), archiveNow.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskRetryWait, retry.Status)
	require.Empty(t, retry.LeaseToken)
	require.NoError(t, db.Close())
	reopened, err := store.Open(ctx, db.Path())
	require.NoError(t, err)
	defer reopened.Close()
	r = reopened.ArchiveTasks()
	got, err := r.Get(ctx, "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, retry, got)
	none, err := r.Claim(ctx, "home-1", archiveNow.Add(59*time.Minute), time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	resumed := claimArchive(t, reopened, archiveNow.Add(time.Hour))
	require.Equal(t, 2, resumed.Attempts)
	update.LastErrorCode = "unreadable_document"
	failed, err := r.Finish(ctx, "home-1", resumed.ID, resumed.LeaseToken, domain.ArchiveTaskFailed, update, archiveNow.Add(time.Hour+time.Second))
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskFailed, failed.Status)
	events, err := r.Events(ctx, "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 5)
	require.Equal(t, "provider_unavailable", events[2].LastErrorCode)
	_, err = r.Cancel(ctx, "home-1", task.ID, archiveNow.Add(2*time.Hour))
	archiveCode(t, err, domain.CodeConflict)
}

func TestArchiveTasksRejectInvalidRequestsWithoutMutation(t *testing.T) {
	db := testutil.NewDB(t)
	r := db.ArchiveTasks()
	ctx := context.Background()
	for i, in := range []struct {
		household, kind, key, input string
		now                         time.Time
	}{
		{"", "extract", "key", "{}", archiveNow}, {"home-1", "../extract", "key", "{}", archiveNow}, {"home-1", "extract", " ", "{}", archiveNow},
		{"home-1", "extract", strings.Repeat("k", 129), "{}", archiveNow}, {"home-1", "extract", "key", "[]", archiveNow}, {"home-1", "extract", "key", "{}", time.Time{}},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			_, _, err := r.Enqueue(ctx, in.household, in.kind, in.key, json.RawMessage(in.input), in.now)
			archiveCode(t, err, domain.CodeInvalidRequest)
		})
	}
	original := enqueueArchive(t, db, "request-1")
	for _, ttl := range []time.Duration{0, -time.Second, 11 * time.Minute} {
		_, err := r.Claim(ctx, "home-1", archiveNow, ttl)
		archiveCode(t, err, domain.CodeInvalidRequest)
	}
	task := claimArchive(t, db, archiveNow)
	_, err := r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskQueued, archiveUpdate(), archiveNow)
	archiveCode(t, err, domain.CodeInvalidRequest)
	invalid := archiveUpdate()
	invalid.StateJSON = json.RawMessage(`[]`)
	_, err = r.Wait(ctx, "home-1", task.ID, task.LeaseToken, invalid, archiveNow)
	archiveCode(t, err, domain.CodeInvalidRequest)
	_, err = r.Retry(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate(), archiveNow.Add(-time.Second), archiveNow)
	archiveCode(t, err, domain.CodeInvalidRequest)
	_, err = r.Retry(ctx, "home-1", task.ID, task.LeaseToken, archiveUpdate(), archiveNow.Add(31*24*time.Hour), archiveNow)
	archiveCode(t, err, domain.CodeInvalidRequest)
	_, err = r.Renew(ctx, "home-1", task.ID, "", archiveNow, time.Minute)
	archiveCode(t, err, domain.CodeInvalidRequest)
	_, err = r.Events(ctx, "home-1", original.ID, -1, 10)
	archiveCode(t, err, domain.CodeInvalidRequest)
	_, err = r.Events(ctx, "home-1", original.ID, 0, 1001)
	archiveCode(t, err, domain.CodeInvalidRequest)
	got, err := r.Get(ctx, "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, task, got)
	events, err := r.Events(ctx, "home-1", task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 2)
}

func TestArchiveTasksEventFailureRollsBackStateAndResult(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	task := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	r := db.ArchiveTasks()
	_, err := db.SQL().ExecContext(ctx, `CREATE TRIGGER reject_archive_events BEFORE INSERT ON archive_task_events BEGIN SELECT RAISE(ABORT,'event write failed'); END`)
	require.NoError(t, err)
	_, err = r.Finish(ctx, "home-1", task.ID, task.LeaseToken, domain.ArchiveTaskSucceeded, archiveUpdate(), archiveNow.Add(time.Second))
	require.ErrorContains(t, err, "event write failed")
	unchanged, err := r.Get(ctx, "home-1", task.ID)
	require.NoError(t, err)
	require.Equal(t, task, unchanged)
}

func TestArchiveTasksCheckpointAndAttemptHistorySurviveReclaim(t *testing.T) {
	db := testutil.NewDB(t)
	enqueueArchive(t, db, "request-1")
	first := claimArchive(t, db, archiveNow)
	ctx := context.Background()
	r := db.ArchiveTasks()
	require.NotEmpty(t, first.AttemptID)
	require.NotEqual(t, first.LeaseToken, first.AttemptID)
	checkpoint, err := r.Checkpoint(ctx, "home-1", first.ID, first.LeaseToken, archiveUpdate(), archiveNow.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, domain.ArchiveTaskRunning, checkpoint.Status)
	require.Equal(t, first.AttemptID, checkpoint.AttemptID)
	require.NoError(t, db.Close())
	reopened, err := store.Open(ctx, db.Path())
	require.NoError(t, err)
	defer reopened.Close()
	r = reopened.ArchiveTasks()
	next := claimArchive(t, reopened, archiveNow.Add(time.Minute))
	require.NotEqual(t, first.AttemptID, next.AttemptID)
	require.Equal(t, checkpoint.StateJSON, next.StateJSON)
	require.Equal(t, checkpoint.ResultJSON, next.ResultJSON)
	require.Equal(t, "extracted", next.Stage)
	_, err = r.Checkpoint(ctx, "home-1", first.ID, first.LeaseToken, archiveUpdate(), archiveNow.Add(time.Minute))
	archiveCode(t, err, domain.CodeConflict)
	_, err = r.Checkpoint(ctx, "home-2", next.ID, next.LeaseToken, archiveUpdate(), archiveNow.Add(time.Minute))
	require.ErrorIs(t, err, domain.ErrNotFound)
	events, err := r.Events(ctx, "home-1", next.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 4)
	require.Equal(t, first.AttemptID, events[1].AttemptID)
	require.Equal(t, first.AttemptID, events[2].AttemptID)
	require.Equal(t, next.AttemptID, events[3].AttemptID)
	require.Equal(t, checkpoint.StateJSON, events[2].StateJSON)
	require.Equal(t, next.StateJSON, events[3].StateJSON)
	require.Equal(t, checkpoint.InputJSON, events[2].InputJSON)
}
