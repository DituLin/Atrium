package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestArchiveClaimKindsDoesNotStealIndexWork(t *testing.T) {
	d := testutil.NewDB(t)
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	ctx := context.Background()
	_, _, e := r.Enqueue(ctx, a, "home-1", "document.index", "index", json.RawMessage(`{}`))
	require.NoError(t, e)
	wanted := enqueueArchive(t, d, "extract")
	got, e := r.ClaimKinds(ctx, "home-1", time.Minute, []string{"document.extract"})
	require.NoError(t, e)
	require.Equal(t, wanted.ID, got.ID)
	got, e = r.ClaimKinds(ctx, "home-1", time.Minute, []string{"document.extract"})
	require.NoError(t, e)
	require.Nil(t, got)
}
func TestArchiveProductsAndBudgetSurviveLeaseLoss(t *testing.T) {
	d := testutil.NewDB(t)
	enqueueArchive(t, d, "extract")
	clock := &archiveClock{now: archiveNow}
	r := d.ArchiveTasksWithClock(clock)
	ctx := context.Background()
	task := claimArchive(t, d, archiveNow)
	fence := store.ArchiveWorkFence{HouseholdID: "home-1", TaskID: task.ID, Token: task.LeaseToken, StateVersion: task.StateVersion}
	product := store.ArchiveProduct{Key: "parse", Fingerprint: "pipeline-v1", Body: json.RawMessage(`{"large":"candidate"}`)}
	next, e := r.SaveProductAndCheckpoint(ctx, fence, product)
	require.NoError(t, e)
	require.EqualValues(t, task.StateVersion+1, next.StateVersion)
	_, e = r.SaveProductAndCheckpoint(ctx, fence, product)
	require.ErrorIs(t, e, domain.ErrConflict)
	fence.StateVersion = next.StateVersion
	inv, _, e := r.ReserveArchiveInvocation(ctx, fence, "chunk-0", 100, 150)
	require.NoError(t, e)
	require.NotEmpty(t, inv.ID)
	clock.Set(archiveNow.Add(time.Minute))
	reclaimed, e := r.ClaimKinds(ctx, "home-1", time.Minute, []string{"document.extract"})
	require.NoError(t, e)
	fence.Token = reclaimed.LeaseToken
	fence.StateVersion = reclaimed.StateVersion
	_, _, e = r.ReserveArchiveInvocation(ctx, fence, "chunk-0", 100, 150)
	require.ErrorIs(t, e, domain.ErrConflict)
	got, e := r.ArchiveProducts(ctx, fence, "pipeline-v1")
	require.NoError(t, e)
	require.Len(t, got, 1)
	got, e = r.ArchiveProducts(ctx, fence, "pipeline-v2")
	require.NoError(t, e)
	require.Empty(t, got)
}

func TestArchiveExtractionBoundToSourceAndDocumentRevision(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	a := archiveActor(t, d)
	m := domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: domain.ArchiveInputText, Channel: "test", Account: "a", Conversation: "c", Sender: "s", ProviderMessageID: "one"}, OriginalName: "note.txt"}
	receipt, e := r.AcceptIntake(ctx, a, "home-1", 1, m, domain.ArchiveBlobRef{HouseholdID: "home-1", SHA256: strings.Repeat("a", 64), SizeBytes: 1, MIME: "text/plain", StorageVersion: 1})
	require.NoError(t, e)
	task, e := r.ClaimKinds(ctx, "home-1", time.Minute, []string{"document.extract"})
	require.NoError(t, e)
	require.Equal(t, receipt.TaskID, task.ID)
	fence := store.ArchiveWorkFence{HouseholdID: "home-1", TaskID: task.ID, Token: task.LeaseToken, StateVersion: task.StateVersion}
	x, next, e := r.BeginArchiveExtraction(ctx, fence, "fingerprint-v1")
	require.NoError(t, e)
	require.Equal(t, receipt.DocumentID, x.DocumentID)
	require.EqualValues(t, 1, x.DocumentRevision)
	fence.StateVersion = next.StateVersion
	same, _, e := r.BeginArchiveExtraction(ctx, fence, "fingerprint-v1")
	require.NoError(t, e)
	require.Equal(t, x.ID, same.ID)
	_, e = d.SQL().Exec(`UPDATE archive_documents SET document_revision=document_revision+1 WHERE id=?`, x.DocumentID)
	require.NoError(t, e)
	_, _, e = r.BeginArchiveExtraction(ctx, fence, "fingerprint-v1")
	require.ErrorIs(t, e, domain.ErrConflict)
}
func TestArchiveInvocationHasPersistentPerOperationBound(t *testing.T) {
	d := testutil.NewDB(t)
	enqueueArchive(t, d, "bound")
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	task := claimArchive(t, d, archiveNow)
	f := store.ArchiveWorkFence{HouseholdID: "home-1", TaskID: task.ID, Token: task.LeaseToken, StateVersion: task.StateVersion}
	for i := 0; i < 2; i++ {
		_, next, e := r.ReserveArchiveInvocation(context.Background(), f, "chunk-0", 1, 512)
		require.NoError(t, e)
		f.StateVersion = next.StateVersion
	}
	_, _, e := r.ReserveArchiveInvocation(context.Background(), f, "chunk-0", 1, 512)
	require.ErrorIs(t, e, domain.ErrConflict)
}
func TestArchiveProductWriteRejectsExcessTotalWithoutAdvancingCheckpoint(t *testing.T) {
	d := testutil.NewDB(t)
	enqueueArchive(t, d, "bounded-products")
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	task := claimArchive(t, d, archiveNow)
	f := store.ArchiveWorkFence{HouseholdID: "home-1", TaskID: task.ID, Token: task.LeaseToken, StateVersion: task.StateVersion}
	// Seed metadata-sized products to exercise count without huge allocations.
	for i := 0; i < 260; i++ {
		_, e := d.SQL().Exec(`INSERT INTO archive_products VALUES(?,?,?,?,?,?)`, f.HouseholdID, f.TaskID, fmt.Sprint(i), "fp", "digest", []byte(`{}`))
		require.NoError(t, e)
	}
	_, e := r.SaveProductAndCheckpoint(context.Background(), f, store.ArchiveProduct{Key: "one-too-many", Fingerprint: "fp", Body: json.RawMessage(`{}`)})
	require.ErrorIs(t, e, domain.ErrConflict)
	var state int64
	var count int
	require.NoError(t, d.SQL().QueryRow(`SELECT state_version FROM archive_tasks WHERE id=?`, f.TaskID).Scan(&state))
	require.Equal(t, f.StateVersion, state)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_products WHERE task_id=?`, f.TaskID).Scan(&count))
	require.Equal(t, 260, count)

}

func TestArchiveProductByteLimitRollsBackCheckpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("128MiB persisted boundary fixture")
	}
	d := testutil.NewDB(t)
	enqueueArchive(t, d, "byte-bound")
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	task := claimArchive(t, d, archiveNow)
	f := store.ArchiveWorkFence{HouseholdID: "home-1", TaskID: task.ID, Token: task.LeaseToken, StateVersion: task.StateVersion}
	_, e := d.SQL().Exec(`INSERT INTO archive_products VALUES(?,?, 'seed','fp','digest',zeroblob(?))`, f.HouseholdID, f.TaskID, (128<<20)-1)
	require.NoError(t, e)
	_, e = r.SaveProductAndCheckpoint(context.Background(), f, store.ArchiveProduct{Key: "overflow", Fingerprint: "fp", Body: json.RawMessage(`{}`)})
	require.ErrorIs(t, e, domain.ErrConflict)
	var state int64
	var count int
	require.NoError(t, d.SQL().QueryRow(`SELECT state_version FROM archive_tasks WHERE id=?`, f.TaskID).Scan(&state))
	require.Equal(t, f.StateVersion, state)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_products WHERE task_id=?`, f.TaskID).Scan(&count))
	require.Equal(t, 1, count)
}
