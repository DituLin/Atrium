package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/DituLin/Atritum/internal/app/events"
	"github.com/DituLin/Atritum/internal/clock"
	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/source"
	"github.com/DituLin/Atritum/internal/store"
)

// DeferInterval is how long a job waits when its preconditions are not met
// (source offline, disk low, cache over budget) — design §6.3 step 1.
const DeferInterval = 2 * time.Minute

// SourceAccess is the slice of the source manager the pipeline needs.
type SourceAccess interface {
	FS(sourceID string) (source.FS, bool)
	Online(sourceID string) bool
}

// Options configures a Pipeline.
type Options struct {
	DB        *store.DB
	Cache     *Cache
	Queue     *jobs.Queue
	Sources   SourceAccess
	Reader    MetadataReader
	Converter Converter
	Home      *clock.Home
	Bus       *events.Bus
	Logger    *slog.Logger
	Disk      DiskStats
	Media     config.Media
	Storage   config.Storage
	Now       func() time.Time
	// TempDir holds converter output; empty uses the OS temp directory.
	TempDir string
}

// Pipeline implements the extract_meta, build_preview and recompute_day job
// handlers and owns the cache janitor.
type Pipeline struct {
	opts    Options
	janitor janitorState
}

// NewPipeline builds the media pipeline, filling in defaults.
func NewPipeline(opts Options) (*Pipeline, error) {
	if opts.DB == nil || opts.Cache == nil || opts.Home == nil {
		return nil, errors.New("media: db, cache and home clock are required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Reader == nil {
		opts.Reader = NewExifReader()
	}
	if opts.Converter == nil {
		opts.Converter = NewConverter(opts.Media.HEIC.Converter)
	}
	if opts.Disk == nil {
		opts.Disk = OSDiskStats{}
	}
	return &Pipeline{opts: opts}, nil
}

// Register attaches the pipeline handlers to a worker pool.
func (p *Pipeline) Register(pool *jobs.Pool) {
	pool.Register(domain.JobExtractMeta, jobs.HandlerFunc(p.ExtractMeta))
	pool.Register(domain.JobBuildPreview, jobs.HandlerFunc(p.BuildPreview))
	pool.Register(domain.JobRecomputeDay, jobs.HandlerFunc(p.RecomputeDay))
}

func (p *Pipeline) now() time.Time { return p.opts.Now() }

// publish notifies clients that photo data changed.
func (p *Pipeline) publish(topics ...domain.Topic) {
	if p.opts.Bus != nil {
		p.opts.Bus.Publish(topics...)
	}
}

// loadPhoto fetches a job's photo, treating a vanished row as done rather than
// as a failure: the indexer may have removed it while the job waited.
func (p *Pipeline) loadPhoto(ctx context.Context, job *domain.Job) (*domain.Photo, error) {
	if job.PhotoID == "" {
		return nil, jobs.Permanent(errors.New("media: job has no photo"))
	}
	photo, err := p.opts.DB.Photos().Get(ctx, job.PhotoID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return photo, nil
}

// openSource opens a photo's bytes, translating source failures into the
// stable preview error codes.
func (p *Pipeline) openSource(ctx context.Context, photo *domain.Photo) (io.ReadSeekCloser, source.FS, error) {
	fsys, ok := p.opts.Sources.FS(photo.SourceID)
	if !ok {
		return nil, nil, jobs.Permanent(fmt.Errorf("media: source %q is not configured", photo.SourceID))
	}
	if !p.opts.Sources.Online(photo.SourceID) {
		return nil, nil, jobs.Defer(DeferInterval, errors.New("source_offline"))
	}
	f, err := fsys.Open(ctx, photo.RelPath)
	if err != nil {
		return nil, nil, classifyIOError(err)
	}
	return f, fsys, nil
}

// classifyIOError maps a source failure to a preview error code.
func classifyIOError(err error) error {
	switch {
	case errors.Is(err, source.ErrStuck), errors.Is(err, source.ErrDegraded):
		return Coded(ErrCodeStuck, err)
	case errors.Is(err, source.ErrSymlink), errors.Is(err, source.ErrUnsafePath):
		return jobs.Permanent(Coded(ErrCodeUnsupported, err))
	case errors.Is(err, os.ErrNotExist):
		// The indexer owns removal; the job simply stops caring.
		return jobs.Permanent(Coded(ErrCodeIO, err))
	default:
		return Coded(ErrCodeIO, err)
	}
}

// localPath makes a photo available as a real file for an external tool. It
// prefers the mount path and falls back to a temporary copy, so the pipeline
// works against both OSFS and an in-memory test filesystem.
func (p *Pipeline) localPath(ctx context.Context, fsys source.FS, photo *domain.Photo) (path string, cleanup func(), err error) {
	if resolver, ok := fsys.(source.PathResolver); ok {
		full, rerr := resolver.Realpath(photo.RelPath)
		if rerr == nil {
			return full, func() {}, nil
		}
	}
	f, err := fsys.Open(ctx, photo.RelPath)
	if err != nil {
		return "", nil, classifyIOError(err)
	}
	defer func() { _ = f.Close() }()

	tmp, err := os.CreateTemp(p.opts.TempDir, "atrium-src-*."+photo.Ext)
	if err != nil {
		return "", nil, Coded(ErrCodeIO, err)
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, f); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", nil, Coded(ErrCodeIO, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return "", nil, Coded(ErrCodeIO, err)
	}
	return name, func() { _ = os.Remove(name) }, nil
}
