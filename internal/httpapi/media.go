package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/jobs"
)

// PreviewRetryAfterSeconds is the Retry-After hint for a 202 response.
const PreviewRetryAfterSeconds = 5

type processingBody struct {
	Status           string `json:"status"`
	RetryAfterSecond int    `json:"retry_after_seconds"`
}

// handleMediaGet serves preview bytes by photo ID. It never accepts a path:
// the only thing a screen can name is an opaque ID, which is what keeps the
// authorized root un-addressable from the LAN (design §15).
func (a *API) handleMediaGet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 10*time.Second)
	defer cancel()

	variant := domain.Variant(r.URL.Query().Get("variant"))
	if variant == "" {
		variant = domain.VariantPreview
	}
	if !variant.Valid() {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "unknown variant"))
		return
	}
	id := r.PathValue("id")

	photo, err := a.deps.DB.Photos().Get(ctx, id)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "photo"))
		return
	}
	// Removed, excluded and revoked photos are indistinguishable from unknown
	// ones on this route: a screen learns nothing from a 404.
	if !a.mediaVisible(ctx, photo) {
		WriteError(w, r, domain.Errorf(domain.CodeNotFound, "photo not found"))
		return
	}

	file, err := a.deps.DB.Previews().Get(ctx, photo.ID, variant)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		WriteError(w, r, err)
		return
	}
	if file == nil || a.deps.Cache == nil || !a.deps.Cache.Exists(file.RelPath) {
		a.writeMediaPending(w, r, photo)
		return
	}

	etag := `"` + file.Fingerprint + `"`
	if file.Fingerprint == "" {
		etag = `"` + photo.Fingerprint + `"`
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=60")
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	data, err := a.deps.Cache.Read(file.RelPath)
	if err != nil {
		a.writeMediaPending(w, r, photo)
		return
	}
	if a.deps.Media != nil {
		a.deps.Media.Touch(ctx, photo.ID, variant)
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	http.ServeContent(w, r, "", file.CreatedAt, bytes.NewReader(data))
}

// mediaVisible applies the same eligibility rule as the collections, plus the
// pending states that legitimately have no bytes yet.
func (a *API) mediaVisible(ctx context.Context, photo *domain.Photo) bool {
	switch photo.Status {
	case domain.PhotoRemoved, domain.PhotoExcluded:
		return false
	default:
	}
	src, err := a.deps.DB.Sources().Get(ctx, photo.SourceID)
	if err != nil || src.Status != domain.SourceActive {
		return false
	}
	return true
}

// writeMediaPending answers when the bytes are not on disk. It distinguishes
// "coming soon" from "cannot be produced right now", which is what lets the
// slideshow skip a photo instead of showing a broken image.
func (a *API) writeMediaPending(w http.ResponseWriter, r *http.Request, photo *domain.Photo) {
	ctx := r.Context()
	switch photo.PreviewStatus {
	case domain.PreviewFailed:
		WriteError(w, r, domain.Errorf(domain.CodeNotFound, "photo has no preview"))
		return
	case domain.PreviewEvicted, domain.PreviewUnavailable, domain.PreviewPending,
		domain.PreviewProcessing, domain.PreviewReady:
	default:
	}

	// An evicted preview can be rebuilt on demand, but only while the share is
	// reachable; the request itself never decodes anything (PRD §5.2).
	sourceOnline := a.deps.Sources == nil || a.deps.Sources.Online(photo.SourceID)
	if !sourceOnline {
		WriteError(w, r, domain.Errorf(domain.CodePreviewUnavailable,
			"the source is offline and no cached preview exists"))
		return
	}
	if a.deps.Queue != nil {
		if err := a.deps.Queue.EnqueuePhoto(ctx, domain.JobBuildPreview,
			photo.ID, photo.SourceID, jobs.PriorityBuildPreview); err != nil {
			if log := LoggerFrom(ctx); log != nil {
				log.Warn("preview rebuild not queued", "component", "media",
					"event", "rebuild_enqueue_failed", "photo_id", photo.ID)
			}
		}
	}
	w.Header().Set("Retry-After", strconv.Itoa(PreviewRetryAfterSeconds))
	WriteJSON(w, http.StatusAccepted, processingBody{
		Status: string(domain.CodePreviewProcessing), RetryAfterSecond: PreviewRetryAfterSeconds,
	})
}
