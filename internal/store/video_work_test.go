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

func TestVideoWorkClassifiesLegacyReadyAndUnsupportedOnce(t *testing.T) {
	for _, status := range []string{"ready", "unsupported"} {
		t.Run(status, func(t *testing.T) {
			db := testutil.NewDB(t)
			ctx := context.Background()
			now := seedSource(t, db, "mixed")
			require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
			v, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "legacy.mov", SizeBytes: 1, Generation: 1}, now)
			require.NoError(t, err)
			task, err := db.VideoWork().Claim(ctx, now, time.Minute)
			require.NoError(t, err)
			require.NotNil(t, task)
			meta := domain.VideoMetadata{Container: "mov", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}
			ok, err := db.VideoWork().Publish(ctx, *task, meta, 100, 10, 10, now)
			require.NoError(t, err)
			require.True(t, ok)
			_, err = db.SQL().ExecContext(ctx, `UPDATE videos SET status=?,metadata_json=json_remove(metadata_json,'$.live_photo') WHERE id=?`, status, v.ID)
			require.NoError(t, err)
			task, err = db.VideoWork().Claim(ctx, now.Add(time.Second), time.Minute)
			require.NoError(t, err)
			require.NotNil(t, task, "old metadata must be classified even with an intact cover or unsupported state")
			if status == "unsupported" {
				ok, err = db.VideoWork().FailUnsupported(ctx, *task, now.Add(time.Second))
			} else {
				ok, err = db.VideoWork().Publish(ctx, *task, meta, 100, 10, 10, now.Add(time.Second))
			}
			require.NoError(t, err)
			require.True(t, ok)
			none, err := db.VideoWork().Claim(ctx, now.Add(time.Hour), time.Minute)
			require.NoError(t, err)
			require.Nil(t, none, "classification must not repeat indefinitely")
		})
	}
}

func TestLivePhotoPublicationRespectsClaimAndSourceAuthority(t *testing.T) {
	for _, scenario := range []string{"current", "changed", "revoked", "expired", "excluded"} {
		t.Run(scenario, func(t *testing.T) {
			db := testutil.NewDB(t)
			ctx := context.Background()
			now := seedSource(t, db, "mixed")
			require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
			obs := domain.VideoObservation{SourceID: "mixed", RelPath: "live.mov", SizeBytes: 1, Generation: 1}
			v, err := db.Videos().Observe(ctx, obs, now)
			require.NoError(t, err)
			task, err := db.VideoWork().Claim(ctx, now, time.Minute)
			require.NoError(t, err)
			require.NotNil(t, task)
			switch scenario {
			case "changed":
				obs.SizeBytes, obs.Generation = 2, 2
				_, err = db.Videos().Observe(ctx, obs, now)
				require.NoError(t, err)
			case "revoked":
				require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
			case "expired":
				now = now.Add(2 * time.Minute)
			case "excluded":
				require.NoError(t, db.Exclusions().Add(ctx, &domain.Exclusion{SourceID: "mixed", MatchKind: domain.MatchPath, Pattern: "live.mov", CreatedAt: now}))
			}
			ok, err := db.VideoWork().PublishLivePhoto(ctx, *task, now)
			require.NoError(t, err)
			require.Equal(t, scenario == "current", ok)
			got, err := db.Videos().Get(ctx, v.ID)
			require.NoError(t, err)
			require.Equal(t, scenario == "current", got.Metadata.LivePhoto)
			if scenario == "current" {
				none, err := db.VideoWork().Claim(ctx, now.Add(time.Hour), time.Minute)
				require.NoError(t, err)
				require.Nil(t, none, "classified Live Photo needs no video cover or repeat work")
				retained, err := db.VideoWork().RetainedTokens(ctx, now)
				require.NoError(t, err)
				require.Empty(t, retained)
			}
		})
	}
}

func TestLivePhotoReauthorizationReclassifiesInsteadOfKeepingOldFlag(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	v, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mov", SizeBytes: 1, Generation: 1}, now)
	require.NoError(t, err)
	old, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, old)
	ok, err := db.VideoWork().PublishLivePhoto(ctx, *old, now)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
	require.NoError(t, db.Sources().Restore(ctx, "mixed", now))
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	next, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, next, "old classification cannot block processing under a new authority")
	retained, err := db.VideoWork().RetainedTokens(ctx, now)
	require.NoError(t, err)
	require.True(t, retained[next.Token], "new authorized claim must protect an in-flight ordinary video cover")
	ok, err = db.VideoWork().PublishLivePhoto(ctx, *old, now)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = db.VideoWork().Publish(ctx, *next, domain.VideoMetadata{Container: "mov", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}, 100, 10, 10, now)
	require.NoError(t, err)
	require.True(t, ok)
	got, err := db.Videos().Get(ctx, v.ID)
	require.NoError(t, err)
	require.False(t, got.Metadata.LivePhoto)
}

func TestUnsupportedVideoReauthorizationAllowsLivePhotoClassification(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	_, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mov", SizeBytes: 1, Generation: 1}, now)
	require.NoError(t, err)
	old, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, old)
	ok, err := db.VideoWork().FailUnsupported(ctx, *old, now)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
	require.NoError(t, db.Sources().Restore(ctx, "mixed", now))
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	next, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, next)
	retained, err := db.VideoWork().RetainedTokens(ctx, now)
	require.NoError(t, err)
	require.True(t, retained[next.Token])
	ok, err = db.VideoWork().PublishLivePhoto(ctx, *next, now)
	require.NoError(t, err)
	require.True(t, ok)
	none, err := db.VideoWork().Claim(ctx, now.Add(time.Hour), time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestVideoReclassificationRecoversAfterRetryOrExpiredLease(t *testing.T) {
	for _, original := range []string{"live_photo", "unsupported"} {
		for _, interrupted := range []string{"retry", "expired"} {
			t.Run(original+"/"+interrupted, func(t *testing.T) {
				db := testutil.NewDB(t)
				ctx := context.Background()
				now := seedSource(t, db, "mixed")
				require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
				_, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mov", SizeBytes: 1, Generation: 1}, now)
				require.NoError(t, err)
				old, err := db.VideoWork().Claim(ctx, now, time.Minute)
				require.NoError(t, err)
				require.NotNil(t, old)
				if original == "live_photo" {
					ok, err := db.VideoWork().PublishLivePhoto(ctx, *old, now)
					require.NoError(t, err)
					require.True(t, ok)
				} else {
					ok, err := db.VideoWork().FailUnsupported(ctx, *old, now)
					require.NoError(t, err)
					require.True(t, ok)
				}
				require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
				require.NoError(t, db.Sources().Restore(ctx, "mixed", now))
				require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
				first, err := db.VideoWork().Claim(ctx, now, time.Minute)
				require.NoError(t, err)
				require.NotNil(t, first)
				if interrupted == "retry" {
					ok, err := db.VideoWork().Retry(ctx, *first, "timeout", now.Add(time.Minute), now)
					require.NoError(t, err)
					require.True(t, ok)
				}
				beforeDue, err := db.VideoWork().Claim(ctx, now, time.Minute)
				require.NoError(t, err)
				require.Nil(t, beforeDue)
				recovered, err := db.VideoWork().Claim(ctx, now.Add(2*time.Minute), time.Minute)
				require.NoError(t, err)
				require.NotNil(t, recovered, "interrupted classification must remain eligible after its lease/backoff")
				ok, err := db.VideoWork().PublishLivePhoto(ctx, *recovered, now.Add(2*time.Minute))
				require.NoError(t, err)
				require.True(t, ok)
			})
		}
	}
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

func TestVideoWorkNewExclusionBlocksBeforeScannerAppliesIt(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	_, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "private/a.mp4", SizeBytes: 1, Generation: 1}, now)
	require.NoError(t, err)
	task, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.NoError(t, db.Exclusions().Add(ctx, &domain.Exclusion{SourceID: "mixed", MatchKind: domain.MatchPrefix, Pattern: "private", CreatedAt: now}))
	retry, err := db.VideoWork().RequestRetry(ctx, task.Video.ID, task.Video.Revision, now)
	require.NoError(t, err)
	require.False(t, retry, "new exclusion blocks manual retry before scanner catches up")
	valid, err := db.VideoWork().Valid(ctx, *task, now)
	require.NoError(t, err)
	require.False(t, valid)
	ok, err := db.VideoWork().Publish(ctx, *task, domain.VideoMetadata{Container: "mp4", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}, 100, 10, 10, now)
	require.NoError(t, err)
	require.False(t, ok)
	next, err := db.VideoWork().Claim(ctx, now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.Nil(t, next)
	retained, err := db.VideoWork().RetainedTokens(ctx, now)
	require.NoError(t, err)
	require.Empty(t, retained)
}

func TestForgetVideoCoverProtectsActiveAndReplacementTokens(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	_, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: 10, Generation: 1}, now)
	require.NoError(t, err)
	first, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, first)
	ok, err := db.VideoWork().ForgetCover(ctx, first.Token, now)
	require.NoError(t, err)
	require.False(t, ok)
	meta := domain.VideoMetadata{Container: "mp4", VideoCodec: "h264", Width: 10, Height: 10, DurationMS: 1000}
	ok, err = db.VideoWork().Publish(ctx, *first, meta, 100, 10, 10, now)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = db.VideoWork().ForgetCover(ctx, first.Token, now)
	require.NoError(t, err)
	require.True(t, ok)
	second, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, second)
	ok, err = db.VideoWork().Publish(ctx, *second, meta, 100, 10, 10, now)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = db.VideoWork().ForgetCover(ctx, first.Token, now)
	require.NoError(t, err)
	require.False(t, ok)
	next, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.Nil(t, next)
}

func TestVideoUnsupportedAndExplicitRetryRespectVersions(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	now := seedSource(t, db, "mixed")
	require.NoError(t, db.Sources().SetHealth(ctx, "mixed", domain.HealthOnline, "", true, now))
	v, err := db.Videos().Observe(ctx, domain.VideoObservation{SourceID: "mixed", RelPath: "a.mp4", SizeBytes: 10, Generation: 1}, now)
	require.NoError(t, err)
	task, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	ok, err := db.VideoWork().FailUnsupported(ctx, *task, now)
	require.NoError(t, err)
	require.True(t, ok)
	got, err := db.Videos().Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VideoUnsupported, got.Status)
	none, err := db.VideoWork().Claim(ctx, now.Add(time.Hour), time.Minute)
	require.NoError(t, err)
	require.Nil(t, none)
	ok, err = db.VideoWork().RequestRetry(ctx, v.ID, v.Revision, now)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = db.VideoWork().FailUnsupported(ctx, *task, now)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = db.VideoWork().RequestRetry(ctx, v.ID, v.Revision, now)
	require.NoError(t, err)
	require.False(t, ok, "stale client retry must not interrupt a replacement")
	next, err := db.VideoWork().Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, next)
	require.Greater(t, next.Video.Revision, v.Revision)
	require.NoError(t, db.Sources().Revoke(ctx, "mixed", "test", now))
	ok, err = db.VideoWork().RequestRetry(ctx, v.ID, next.Video.Revision, now)
	require.NoError(t, err)
	require.False(t, ok)
}
