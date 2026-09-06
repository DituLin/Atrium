package indexer_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
)

func TestSchedulerRunsOnItsIntervalAndStopsWithContext(t *testing.T) {
	fx := newFixture(t, func(s *config.Source) {
		s.Scan.Interval = config.Duration(20 * time.Millisecond)
	})
	fx.add("a.jpg", 10, mod)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { fx.sched.Run(ctx); close(done) }()

	require.Eventually(t, func() bool {
		_, err := fx.db.Photos().GetByPath(context.Background(), srcID, "a.jpg")
		return err == nil
	}, 3*time.Second, 5*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func TestManualRequestWakesTheLoopImmediately(t *testing.T) {
	fx := newFixture(t, func(s *config.Source) {
		s.Scan.Interval = config.Duration(time.Hour)
	})
	fx.add("a.jpg", 10, mod)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go fx.sched.Run(ctx)

	reqCtx, reqCancel := context.WithTimeout(ctx, 5*time.Second)
	defer reqCancel()
	res, err := fx.sched.Request(reqCtx, srcID, domain.ScanManual)
	require.NoError(t, err)
	require.NotNil(t, res.Run)
	assert.False(t, res.Merged)
	assert.Equal(t, domain.ScanCompleted, res.Run.Status)
	assert.EqualValues(t, 1, res.Run.FilesNew)
}

func TestRequestForUnknownSourceIsNotFound(t *testing.T) {
	fx := newFixture(t, nil)
	_, err := fx.sched.Request(context.Background(), "nope", domain.ScanManual)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestScanNowMergesIntoARunningScan(t *testing.T) {
	fx := newFixture(t, func(s *config.Source) {
		s.Scan.Interval = config.Duration(time.Hour)
		s.Scan.StabilityChecks = 2
		s.Scan.StabilityInterval = config.Duration(150 * time.Millisecond)
		s.Scan.StabilityMaxRound = 2
	})
	fx.add("a.jpg", 10, mod)

	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = fx.sched.ScanNow(context.Background(), srcID, domain.ScanScheduled)
	}()
	<-started
	time.Sleep(30 * time.Millisecond)

	run, err := fx.sched.ScanNow(context.Background(), srcID, domain.ScanManual)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, domain.ScanRunning, run.Status, "a second request joins the running scan")
}
