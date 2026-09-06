package httpapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/httpapi"
	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/media"
	"github.com/DituLin/Atritum/internal/source"
	"github.com/DituLin/Atritum/internal/widget"
)

const photoSourceID = "family_photos"

// photoHarness wires the collections and media routes over a temporary cache.
type photoHarness struct {
	*harness
	cache   *media.Cache
	fakeFS  *source.FakeFS
	manager *source.Manager
}

func newPhotoHarness(t *testing.T) *photoHarness {
	t.Helper()
	ph := &photoHarness{}
	cacheDir := t.TempDir()

	h := newHarnessWith(t, func(h *harness, deps *httpapi.Deps) {
		ph.cache = media.NewCache(cacheDir)
		ph.fakeFS = source.NewFakeFS()
		mgr, err := source.NewManager(source.ManagerOptions{
			Config: h.cfg, DB: h.db, Logger: deps.Logger,
			NewFS: func(config.Source) (source.FS, error) { return ph.fakeFS, nil },
		})
		require.NoError(t, err)
		ph.manager = mgr
		deps.Cache = ph.cache
		deps.Sources = mgr
		deps.Queue = jobs.NewQueue(h.db, deps.Now)
		h.composer.WithSourceStats(func(id string) widget.SourceStats {
			entry := mgr.Get(id)
			if entry == nil {
				return widget.SourceStats{IdentityConfirmed: true}
			}
			return widget.SourceStats{
				StuckOps:          entry.Stats().StuckOps,
				IdentityConfirmed: !entry.IdentityMismatch(),
			}
		})
	}, func(cfg *config.Config) {
		cfg.Sources = []config.Source{{
			ID: photoSourceID, Name: "Family photos", Root: "/mnt/photos",
			IncludeExtensions: []string{"jpg"},
			Identity:          config.Identity{AllowLocal: true},
			Scan: config.Scan{
				Interval: config.Duration(time.Minute), StabilityInterval: config.Duration(time.Second),
				StabilityChecks: 2, StabilityMaxRound: 3,
			},
			IOTimeout: config.Duration(5 * time.Second), MaxInflight: 4,
		}}
	})
	ph.harness = h
	ctx := context.Background()
	require.NoError(t, h.db.Sources().Upsert(ctx, photoSourceID, "Family photos", "/mnt/photos", h.clock.Now()))
	ph.manager.Probe(ctx, ph.manager.Get(photoSourceID))
	return ph
}

// seedOptions describes a photo row to insert.
type seedOptions struct {
	RelPath     string
	CapturedAt  *time.Time
	Confidence  domain.CapturedConfidence
	FirstSeenAt time.Time
	IsBaseline  bool
	Status      domain.PhotoStatus
	Preview     domain.PreviewStatus
	WithFile    bool
}

// seedPhoto inserts a photo and, when asked, a cached preview file.
func (p *photoHarness) seedPhoto(t *testing.T, o seedOptions) *domain.Photo {
	t.Helper()
	ctx := context.Background()
	now := p.clock.Now()
	if o.FirstSeenAt.IsZero() {
		o.FirstSeenAt = now
	}
	if o.Status == "" {
		o.Status = domain.PhotoReady
	}
	if o.Preview == "" {
		o.Preview = domain.PreviewReady
	}
	if o.Confidence == "" {
		o.Confidence = domain.CapturedUnknown
	}
	photo := &domain.Photo{
		SourceID: photoSourceID, RelPath: o.RelPath, Ext: "jpg",
		SizeBytes: 1024, MtimeUnix: now.Unix(), Fingerprint: "fp-" + o.RelPath,
		Status: o.Status, Width: 1600, Height: 1200,
		CapturedAt: o.CapturedAt, CapturedConfidence: o.Confidence,
		FirstSeenAt: o.FirstSeenAt, LastSeenAt: now, LastSeenGeneration: 1,
		IsBaseline: o.IsBaseline, MetaStatus: domain.MetaReady, PreviewStatus: o.Preview,
		CreatedAt: now, UpdatedAt: now,
	}
	if o.CapturedAt != nil {
		photo.CapturedDay = p.deps.Home.HomeDay(*o.CapturedAt)
	}
	require.NoError(t, p.db.Photos().Insert(ctx, photo))

	if o.WithFile {
		rel, err := media.CacheRelPath(domain.VariantPreview, photo.ID)
		require.NoError(t, err)
		require.NoError(t, p.cache.Write(rel, []byte("\xff\xd8\xff\xe0jpeg-bytes")))
		require.NoError(t, p.db.Previews().Put(ctx, &domain.PreviewFile{
			PhotoID: photo.ID, Variant: domain.VariantPreview, RelPath: rel,
			Bytes: 16, Width: 800, Height: 600, Fingerprint: photo.Fingerprint,
			CreatedAt: now, LastAccessAt: now,
		}))
	}
	return photo
}

// sourceVolume returns a mount identity that differs from the fake default,
// which is what an unexpectedly swapped share looks like.
func sourceVolume() source.VolumeStats {
	return source.VolumeStats{
		FSType: "smbfs", MountFrom: "//guest@other/photos", IsMountPoint: true,
	}
}
