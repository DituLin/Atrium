package store_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestVideoWorkLeaseRecoveryAndStalePublication(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	o := domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: 10, Generation: 1}
	_, err := db.Videos().Observe(ctx, o, now)
	require.NoError(t, err)
	first, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, first)
	none, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	second, err := db.VideoWork().Claim(ctx, now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.NotEqual(t, first.Token, second.Token)
	m := domain.VideoMetadata{Container: "mp4", VideoCodec: "h264", Width: 100, Height: 100, DurationMS: 1000}
	ok, err := db.VideoWork().Publish(ctx, *first, m, 100, 100, 100, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = db.VideoWork().Publish(ctx, *second, m, 100, 100, 100, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	none, err = db.VideoWork().Claim(ctx, now.Add(3*time.Minute), time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	o.SizeBytes = 20
	o.Generation = 2
	_, err = db.Videos().Observe(ctx, o, now.Add(4*time.Minute))
	require.NoError(t, err)
	third, err := db.VideoWork().Claim(ctx, now.Add(4*time.Minute), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, third)
	require.EqualValues(t, 1, third.Attempts)
}

func TestVideoWorkReauthorizationRejectsOldResult(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	_, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mov", SizeBytes: 1, Generation: 1}, now)
	require.NoError(t, err)
	task, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
	require.NoError(t, db.Sources().Restore(ctx, "mixed", now))
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	ok, err := db.VideoWork().Publish(ctx, *task, domain.VideoMetadata{Container: "mov", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}, 100, 10, 10, now)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestVideoWorkRetryAndChangedFileReset(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	_, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: 1, Generation: 1}, now)
	require.NoError(t, err)
	task, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	ok, err := db.VideoWork().Retry(ctx, *task, "io_error", now.Add(time.Minute), now)
	require.NoError(t, err)
	require.True(t, ok)
	none, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	again, err := db.VideoWork().Claim(ctx, now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, again)
	require.EqualValues(t, 2, again.Attempts)
}

func TestVideoWorkConcurrentClaimAndFileChange(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	o := domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: 1, Generation: 1}
	_, err := db.Videos().Observe(ctx, o, now)
	require.NoError(t, err)
	none, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.Nil(t, none, "unknown source cannot be read")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	var wg sync.WaitGroup
	claims := make(chan bool, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := db.VideoWork().Claim(ctx, now, time.Minute)
			claims <- task != nil
			errs <- err
		}()
	}
	wg.Wait()
	close(claims)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	n := 0
	for claimed := range claims {
		if claimed {
			n++
		}
	}
	require.Equal(t, 1, n)
	old, err := db.VideoWork().Claim(ctx, now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, old)
	o.SizeBytes = 2
	o.Generation = 2
	_, err = db.Videos().Observe(ctx, o, now.Add(2*time.Minute))
	require.NoError(t, err)
	ok, err := db.VideoWork().Publish(ctx, *old, domain.VideoMetadata{Container: "mp4", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}, 100, 10, 10, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = db.VideoWork().Retry(ctx, *old, "io_error", now.Add(3*time.Minute), now.Add(2*time.Minute))
	require.NoError(t, err)
	require.False(t, ok)
	newTask, err := db.VideoWork().Claim(ctx, now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, newTask)
	require.EqualValues(t, 1, newTask.Attempts)
}
