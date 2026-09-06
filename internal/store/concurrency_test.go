package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
)

// TestBatchedIndexWritesDoNotStarveJobClaims reproduces what a first scan of a
// large share does to the job pool: the indexer commits batches of inserts
// while two workers claim jobs. Before `_txlock=immediate` and the busy retry
// the claim transaction failed with SQLITE_BUSY_SNAPSHOT (517) within a few
// iterations; it must now never surface an error to the caller.
func TestBatchedIndexWritesDoNotStarveJobClaims(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "family_photos")

	const batches, perBatch = 12, 25

	var wg sync.WaitGroup
	var producerDone atomic.Bool
	errCh := make(chan error, 8)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer producerDone.Store(true)
		for b := 0; b < batches; b++ {
			err := db.InWriteTx(ctx, func(tx *sql.Tx) error {
				photos := db.Photos().WithTx(tx)
				jobsRepo := db.Jobs().WithTx(tx)
				for i := 0; i < perBatch; i++ {
					ph := &domain.Photo{
						SourceID: "family_photos", RelPath: fmt.Sprintf("%d/%d.jpg", b, i), Ext: "jpg",
						SizeBytes: 1000, MtimeUnix: now.Unix(), Status: domain.PhotoPending,
						FirstSeenAt: now, LastSeenAt: now, LastSeenGeneration: 1,
						MetaStatus: domain.MetaPending, PreviewStatus: domain.PreviewPending,
						CreatedAt: now, UpdatedAt: now,
					}
					if err := photos.Insert(ctx, ph); err != nil {
						return err
					}
					if err := jobsRepo.Enqueue(ctx, &domain.Job{
						Kind: domain.JobExtractMeta, PhotoID: ph.ID, SourceID: "family_photos",
						Status: domain.JobQueued, Priority: 10, NextRunAt: now, CreatedAt: now, UpdatedAt: now,
					}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				errCh <- fmt.Errorf("batch %d: %w", b, err)
				return
			}
		}
	}()

	claimed := make(chan int, 2)
	for w := 0; w < 2; w++ {
		worker := fmt.Sprintf("worker-%d", w)
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := 0
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				job, err := db.Jobs().Claim(ctx, worker, now.Add(time.Minute))
				if err != nil && !errors.Is(err, domain.ErrNotFound) {
					errCh <- fmt.Errorf("%s claim: %w", worker, err)
					return
				}
				if job == nil {
					if producerDone.Load() {
						break
					}
					time.Sleep(time.Millisecond)
					continue
				}
				n++
				if err := db.Jobs().Complete(ctx, job.ID, now); err != nil {
					errCh <- fmt.Errorf("%s complete: %w", worker, err)
					return
				}
			}
			claimed <- n
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	close(claimed)
	total := 0
	for n := range claimed {
		total += n
	}
	require.Positive(t, total, "workers should have claimed jobs while the indexer was committing")

	count, err := db.Photos().CountBySourceStatus(ctx, "family_photos", domain.PhotoPending)
	require.NoError(t, err)
	require.EqualValues(t, batches*perBatch, count)
}

// TestRetryBusyStopsOnSuccess documents the retry helper's contract.
func TestRetryBusyStopsOnSuccess(t *testing.T) {
	calls := 0
	err := store.RetryBusy(context.Background(), func() error {
		calls++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.True(t, store.IsBusy(errBusy{}))
	require.False(t, store.IsBusy(nil))
}

type errBusy struct{}

func (errBusy) Error() string { return "database is locked (5) (SQLITE_BUSY)" }
