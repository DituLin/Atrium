package media_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/jobs"
	"github.com/DituLin/Atrium/internal/media"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
)

const testSourceID = "family_photos"

// fakeSources is the pipeline's view of the source manager.
type fakeSources struct {
	fs     source.FS
	online bool
}

func (f *fakeSources) FS(string) (source.FS, bool) { return f.fs, f.fs != nil }
func (f *fakeSources) Online(string) bool          { return f.online }

type fixture struct {
	db       *store.DB
	pipeline *media.Pipeline
	queue    *jobs.Queue
	cache    *media.Cache
	fs       *source.FakeFS
	sources  *fakeSources
	disk     *media.FakeDiskStats
	bus      *events.Bus
	now      time.Time
}

func newFixture(t *testing.T, mutate func(*media.Options)) *fixture {
	t.Helper()
	db := testutil.NewDB(t)
	ctx := context.Background()
	require.NoError(t, db.Sources().Upsert(ctx, testSourceID, "Family", "/mnt/photos", time.Now()))

	fx := &fixture{
		db:    db,
		cache: media.NewCache(t.TempDir()),
		fs:    source.NewFakeFS(),
		queue: jobs.NewQueue(db, time.Now),
		bus:   events.NewBus(),
		disk:  &media.FakeDiskStats{FreeBytes: 1 << 40, TotalBytes: 1 << 41},
		now:   time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
	fx.sources = &fakeSources{fs: fx.fs, online: true}

	opts := media.Options{
		DB: db, Cache: fx.cache, Queue: fx.queue, Sources: fx.sources,
		Home: homeSG(t), Bus: fx.bus, Disk: fx.disk,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Media: config.Media{
			PreviewMaxEdge: 512, PreviewMaxBytes: 1 << 20, ThumbMaxEdge: 128,
			MaxPixels: 40_000_000, MaxSourceBytes: 60 << 20,
			HEIC: config.HEIC{Converter: media.ConverterOff},
		},
		Storage:   config.Storage{CacheBudgetBytes: 10 << 30, MinFreeBytes: 1 << 20},
		Converter: media.OffConverter{},
		Now:       func() time.Time { return fx.now },
		TempDir:   t.TempDir(),
	}
	if mutate != nil {
		mutate(&opts)
	}
	p, err := media.NewPipeline(opts)
	require.NoError(t, err)
	fx.pipeline = p
	return fx
}

// seed inserts a photo row and its bytes into the fake filesystem.
func (f *fixture) seed(t *testing.T, relPath, ext string, data []byte) *domain.Photo {
	t.Helper()
	ctx := context.Background()
	mod := time.Unix(1_760_000_000, 0)
	f.fs.AddFile(relPath, data, mod)
	photo := &domain.Photo{
		SourceID: testSourceID, RelPath: relPath, Ext: ext,
		SizeBytes: int64(len(data)), MtimeUnix: mod.Unix(),
		Status:      domain.PhotoPending,
		FirstSeenAt: f.now, LastSeenAt: f.now, LastSeenGeneration: 1,
		CreatedAt: f.now, UpdatedAt: f.now,
	}
	require.NoError(t, f.db.Photos().Insert(ctx, photo))
	return photo
}

func (f *fixture) reload(t *testing.T, id string) *domain.Photo {
	t.Helper()
	p, err := f.db.Photos().Get(context.Background(), id)
	require.NoError(t, err)
	return p
}

func (f *fixture) job(kind domain.JobKind, photoID string) *domain.Job {
	return &domain.Job{Kind: kind, PhotoID: photoID, SourceID: testSourceID}
}

// storeMeta builds a metadata result carrying only a capture instant, used by
// tests that exercise day recomputation.
func storeMeta(at time.Time) store.MetaResult {
	return store.MetaResult{
		CapturedAt: &at, CapturedConfidence: domain.CapturedExact, CapturedDay: "wrong",
	}
}
