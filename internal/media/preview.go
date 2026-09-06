package media

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/source"
)

// MaxPreviewAttempts is the number of decode attempts before a photo's preview
// is declared permanently failed (design §6.3 step 5).
const MaxPreviewAttempts = 5

// BuildPreview implements the build_preview job: it turns one source file into
// a preview and a thumbnail in the local cache.
func (p *Pipeline) BuildPreview(ctx context.Context, job *domain.Job) error {
	photo, err := p.loadPhoto(ctx, job)
	if err != nil || photo == nil {
		return err
	}
	switch photo.Status {
	case domain.PhotoExcluded, domain.PhotoRemoved:
		return nil
	default:
	}
	if err := p.previewGuards(ctx); err != nil {
		return err
	}

	now := p.now()
	if err := p.opts.DB.Photos().SetPreviewProcessing(ctx, photo.ID, now); err != nil &&
		!errors.Is(err, domain.ErrNotFound) {
		return err
	}

	if err := p.renderAndStore(ctx, photo); err != nil {
		return p.recordPreviewFailure(ctx, photo, err)
	}
	if err := p.opts.DB.Photos().SetPreviewReady(ctx, photo.ID, p.now()); err != nil &&
		!errors.Is(err, domain.ErrNotFound) {
		return err
	}
	p.publish(domain.TopicPhotos, domain.TopicHome)
	// A fresh file may have pushed the cache over budget.
	p.RunJanitor(ctx)
	return nil
}

// previewGuards refuses to start work the system cannot afford right now.
// These are deferrals, not failures: nothing is wrong with the photo.
func (p *Pipeline) previewGuards(ctx context.Context) error {
	if min := p.opts.Storage.MinFreeBytes; min > 0 {
		free, _, err := p.opts.Disk.Free(p.opts.Cache.Root())
		if err == nil && free < min {
			return jobs.Defer(DeferInterval, errors.New("low_disk"))
		}
	}
	if budget := p.opts.Storage.CacheBudgetBytes; budget > 0 {
		used, err := p.opts.DB.Previews().TotalBytes(ctx)
		if err == nil && used > budget {
			// Try to make room before giving up on this round.
			p.RunJanitor(ctx)
			if used, err = p.opts.DB.Previews().TotalBytes(ctx); err == nil && used > budget {
				return jobs.Defer(DeferInterval, errors.New("cache_full"))
			}
		}
	}
	return nil
}

// renderAndStore decodes the source and writes both variants.
func (p *Pipeline) renderAndStore(ctx context.Context, photo *domain.Photo) error {
	if max := p.opts.Media.MaxSourceBytes; max > 0 && photo.SizeBytes > max {
		return jobs.Permanent(Coded(ErrCodeTooLarge,
			fmt.Errorf("%d bytes exceeds max_source_bytes %d", photo.SizeBytes, max)))
	}
	img, err := p.decodePhoto(ctx, photo)
	if err != nil {
		return err
	}
	// EXIF pixel dimensions are optional and absent from every PNG, so the
	// decoded size is the authoritative one. It is recorded after the
	// orientation transform, which is what a client would display.
	if bounds := img.Bounds(); photo.Width != bounds.Dx() || photo.Height != bounds.Dy() {
		if err := p.opts.DB.Photos().SetDimensions(ctx, photo.ID, bounds.Dx(), bounds.Dy(), p.now()); err != nil &&
			!errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}

	preview, err := Render(img, RenderOptions{
		MaxEdge:  p.opts.Media.PreviewMaxEdge,
		MaxBytes: p.opts.Media.PreviewMaxBytes,
	})
	if err != nil {
		return err
	}
	thumb, err := Render(img, RenderOptions{MaxEdge: p.opts.Media.ThumbMaxEdge, Quality: 80})
	if err != nil {
		return err
	}
	if err := p.storeVariant(ctx, photo, domain.VariantPreview, preview); err != nil {
		return err
	}
	return p.storeVariant(ctx, photo, domain.VariantThumb, thumb)
}

// decodePhoto returns the decoded pixels, routing HEIC through the converter.
func (p *Pipeline) decodePhoto(ctx context.Context, photo *domain.Photo) (image.Image, error) {
	fsys, ok := p.opts.Sources.FS(photo.SourceID)
	if !ok {
		return nil, jobs.Permanent(fmt.Errorf("media: source %q is not configured", photo.SourceID))
	}
	if IsHEIC(photo.Ext) {
		return p.decodeHEIC(ctx, fsys, photo)
	}
	f, _, err := p.openSource(ctx, photo)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if _, _, err := InspectConfig(f, p.opts.Media.MaxPixels); err != nil {
		if CodeOf(err) == ErrCodeTooLarge || CodeOf(err) == ErrCodeUnsupported {
			return nil, jobs.Permanent(err)
		}
		return nil, err
	}
	return Decode(f)
}

// decodeHEIC converts through sips under the job deadline and decodes the
// resulting JPEG, whose orientation is already normalised by the converter.
func (p *Pipeline) decodeHEIC(ctx context.Context, fsys source.FS, photo *domain.Photo) (image.Image, error) {
	if !p.opts.Converter.Enabled() {
		return nil, jobs.Permanent(Coded(ErrCodeUnsupported, ErrHEICDisabled))
	}
	if !p.opts.Sources.Online(photo.SourceID) {
		return nil, jobs.Defer(DeferInterval, errors.New("source_offline"))
	}
	src, cleanup, err := p.localPath(ctx, fsys, photo)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	dir, err := os.MkdirTemp(p.opts.TempDir, "atrium-heic-")
	if err != nil {
		return nil, Coded(ErrCodeIO, err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	out, err := p.opts.Converter.Convert(ctx, src, dir, p.opts.Media.PreviewMaxEdge)
	if err != nil {
		if CodeOf(err) == ErrCodeUnsupported {
			return nil, jobs.Permanent(err)
		}
		return nil, err
	}
	f, err := os.Open(out) //nolint:gosec // path produced by the converter inside our temp dir
	if err != nil {
		return nil, Coded(ErrCodeIO, err)
	}
	defer func() { _ = f.Close() }()
	if _, _, err := InspectConfig(f, p.opts.Media.MaxPixels); err != nil {
		return nil, jobs.Permanent(err)
	}
	return Decode(f)
}

// storeVariant writes one derived image and records it in preview_files.
func (p *Pipeline) storeVariant(ctx context.Context, photo *domain.Photo, variant domain.Variant, r Rendered) error {
	rel, err := CacheRelPath(variant, photo.ID)
	if err != nil {
		return jobs.Permanent(err)
	}
	if err := p.opts.Cache.Write(rel, r.Data); err != nil {
		return Coded(ErrCodeIO, err)
	}
	now := p.now()
	return p.opts.DB.Previews().Put(ctx, &domain.PreviewFile{
		PhotoID: photo.ID, Variant: variant, RelPath: rel,
		Bytes: int64(len(r.Data)), Width: r.Width, Height: r.Height,
		Fingerprint: photo.Fingerprint, CreatedAt: now, LastAccessAt: now,
	})
}

// recordPreviewFailure applies the retry policy of design §6.3 step 5 and
// promotes a format problem to a permanent `unsupported` verdict.
func (p *Pipeline) recordPreviewFailure(ctx context.Context, photo *domain.Photo, cause error) error {
	repo := p.opts.DB.Photos()
	now := p.now()

	// A deferral says the system is busy, not that the photo is broken, so the
	// retry budget and the error code are left untouched.
	if jobs.IsDeferred(cause) {
		if err := repo.SetPreviewStatus(ctx, photo.ID, domain.PreviewPending, "", now); err != nil &&
			!errors.Is(err, domain.ErrNotFound) {
			return err
		}
		return cause
	}

	code := CodeOf(cause)
	attempts := photo.PreviewAttempts + 1

	switch {
	case code == ErrCodeUnsupported:
		if err := repo.SetUnsupported(ctx, photo.ID, code, now); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		p.opts.Logger.Info("photo format unsupported", "component", "media",
			"event", "preview_unsupported", "photo_id", photo.ID, "code", code)
		p.publish(domain.TopicPhotos, domain.TopicHome)
		return jobs.Permanent(cause)
	case jobs.IsPermanent(cause) || attempts >= MaxPreviewAttempts:
		if err := repo.SetPreviewFailed(ctx, photo.ID, code, now); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		p.opts.Logger.Warn("preview permanently failed", "component", "media",
			"event", "preview_failed", "photo_id", photo.ID, "code", code, "attempts", attempts)
		p.publish(domain.TopicPhotos, domain.TopicHome)
		return jobs.Permanent(cause)
	default:
		next := now.Add(jobs.Backoff(attempts))
		if err := repo.SetPreviewRetry(ctx, photo.ID, code, next, now); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		return cause
	}
}
