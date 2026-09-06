package media

import (
	"context"
	"errors"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/jobs"
	"github.com/DituLin/Atritum/internal/source"
	"github.com/DituLin/Atritum/internal/store"
)

// ExtractMeta implements the extract_meta job (design §6.3).
//
// It reads EXIF, resolves the capture time, computes the fingerprint and then
// chains build_preview. A file with no metadata is a normal outcome: the photo
// is still displayable, it simply has an unknown capture time.
func (p *Pipeline) ExtractMeta(ctx context.Context, job *domain.Job) error {
	photo, err := p.loadPhoto(ctx, job)
	if err != nil || photo == nil {
		return err
	}
	switch photo.Status {
	case domain.PhotoExcluded, domain.PhotoRemoved:
		return nil
	default:
	}

	f, fsys, err := p.openSource(ctx, photo)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	now := p.now()
	fingerprint, err := Fingerprint(f, photo.SizeBytes, photo.MtimeUnix)
	if err != nil {
		return Coded(ErrCodeIO, err)
	}

	md, readErr := p.opts.Reader.Read(ctx, f)
	if readErr != nil && !errors.Is(readErr, ErrNoMetadata) {
		// A malformed EXIF block is not a reason to lose the photo: the
		// preview job still gets its chance to decode the pixels.
		p.opts.Logger.Debug("metadata read failed", "component", "media",
			"event", "meta_read_failed", "photo_id", photo.ID, "error", readErr.Error())
		md = Metadata{}
	}
	if md.DateTimeOriginal.IsZero() && IsHEIC(photo.Ext) {
		md = p.heicCreationFallback(ctx, fsys, photo, md)
	}

	captured := ResolveCaptured(md, p.opts.Home, now)
	res := store.MetaResult{
		Fingerprint:           fingerprint,
		Width:                 md.Width,
		Height:                md.Height,
		Orientation:           md.Orientation,
		CapturedAt:            captured.At,
		CapturedOffsetSeconds: captured.OffsetSeconds,
		CapturedConfidence:    captured.Confidence,
		CapturedDay:           captured.Day,
		Error:                 captured.Error,
	}
	if err := p.opts.DB.Photos().SetMeta(ctx, photo.ID, res, now); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}

	// Metadata first, pixels second: the chain keeps preview work behind the
	// cheap read so a slow share cannot starve the index.
	if err := p.opts.Queue.EnqueuePhoto(ctx, domain.JobBuildPreview,
		photo.ID, photo.SourceID, jobs.PriorityBuildPreview); err != nil {
		return err
	}
	p.publish(domain.TopicPhotos, domain.TopicHome)
	return nil
}

// heicCreationFallback asks the converter for a capture time when the HEIC
// container carried none Atrium could parse (design D8). The value has no
// timezone, so it is treated exactly like an EXIF date without an offset.
func (p *Pipeline) heicCreationFallback(ctx context.Context, fsys source.FS, photo *domain.Photo, md Metadata) Metadata {
	if !p.opts.Converter.Enabled() {
		return md
	}
	path, cleanup, err := p.localPath(ctx, fsys, photo)
	if err != nil {
		return md
	}
	defer cleanup()
	at, err := p.opts.Converter.CreationTime(ctx, path)
	if err != nil {
		p.opts.Logger.Debug("heic creation time unavailable", "component", "media",
			"event", "heic_creation_missing", "photo_id", photo.ID)
		return md
	}
	md.DateTimeOriginal = time.Date(at.Year(), at.Month(), at.Day(),
		at.Hour(), at.Minute(), at.Second(), 0, time.UTC)
	md.HasOffset = false
	return md
}
