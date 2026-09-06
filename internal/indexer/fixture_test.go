package indexer_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/app/events"
	"github.com/DituLin/Atritum/internal/clock"
	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/indexer"
	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/media"
	"github.com/DituLin/Atritum/internal/source"
	"github.com/DituLin/Atritum/internal/store"
	"github.com/DituLin/Atritum/internal/store/testutil"
)

const srcID = "family_photos"

type fixture struct {
	db      *store.DB
	fs      *source.FakeFS
	manager *source.Manager
	sched   *indexer.Scheduler
	bus     *events.Bus
	now     time.Time
}

// newFixture wires an indexer over an in-memory source. Stability is set to a
// single check by default so most tests do not have to wait; the stability
// tests override it.
func newFixture(t *testing.T, mutate func(*config.Source)) *fixture {
	t.Helper()
	ctx := context.Background()
	db := testutil.NewDB(t)
	fake := source.NewFakeFS()

	sc := config.Source{
		ID: srcID, Name: "Family photos", Root: "/mnt/photos",
		IncludeExtensions: []string{"jpg", "jpeg", "png", "heic"},
		Identity:          config.Identity{AllowLocal: true},
		Scan: config.Scan{
			Interval:          config.Duration(time.Hour),
			StabilityInterval: config.Duration(time.Millisecond),
			StabilityChecks:   1,
			StabilityMaxRound: 3,
		},
	}
	if mutate != nil {
		mutate(&sc)
	}
	cfg := &config.Config{Sources: []config.Source{sc}}
	require.NoError(t, db.Sources().Upsert(ctx, srcID, sc.Name, sc.Root, time.Now()))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewBus()
	mgr, err := source.NewManager(source.ManagerOptions{
		Config: cfg, DB: db, Logger: log, Bus: bus,
		NewFS: func(config.Source) (source.FS, error) { return fake, nil },
	})
	require.NoError(t, err)
	fake.SetVolume(source.VolumeStats{FSType: "apfs", MountFrom: "/dev/disk1"})
	require.Equal(t, domain.HealthOnline, mgr.Probe(ctx, mgr.Get(srcID)).Health)

	home, err := clock.NewHome("Asia/Singapore", clock.SystemClock{})
	require.NoError(t, err)

	fx := &fixture{db: db, fs: fake, manager: mgr, bus: bus, now: time.Now()}
	fx.sched = indexer.NewScheduler(indexer.SchedulerOptions{
		Sources: mgr, DB: db, Queue: jobs.NewQueue(db, time.Now),
		Cache: media.NewCache(t.TempDir()), Home: home, Bus: bus, Logger: log,
	})
	return fx
}

func (f *fixture) scan(t *testing.T, mode domain.ScanMode) *domain.ScanRun {
	t.Helper()
	run, err := f.sched.ScanNow(context.Background(), srcID, mode)
	require.NoError(t, err)
	require.NotNil(t, run)
	return run
}

func (f *fixture) photo(t *testing.T, rel string) *domain.Photo {
	t.Helper()
	p, err := f.db.Photos().GetByPath(context.Background(), srcID, rel)
	require.NoError(t, err)
	return p
}

func (f *fixture) add(rel string, size int, mod time.Time) {
	f.fs.AddFile(rel, make([]byte, size), mod)
}

// sourceVolume builds a distinct mount identity for identity-mismatch tests.
func sourceVolume(name string) source.VolumeStats {
	return source.VolumeStats{FSType: "apfs", MountFrom: "/dev/" + name}
}
