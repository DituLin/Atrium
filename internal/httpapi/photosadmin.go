package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/jobs"
)

// photoAdminDTO is the only representation that reveals a relative path, and
// it is reachable only with the admin scope (design §8, §15).
type photoAdminDTO struct {
	photoDTO
	RelPath            string  `json:"rel_path"`
	Ext                string  `json:"ext"`
	SizeBytes          int64   `json:"size_bytes"`
	MtimeUnix          int64   `json:"mtime_unix"`
	Fingerprint        string  `json:"fingerprint,omitempty"`
	Status             string  `json:"status"`
	MetaStatus         string  `json:"meta_status"`
	MetaError          string  `json:"meta_error,omitempty"`
	PreviewError       string  `json:"preview_error,omitempty"`
	PreviewAttempts    int     `json:"preview_attempts"`
	PreviewNextRetryAt *string `json:"preview_next_retry_at,omitempty"`
	LastSeenAt         string  `json:"last_seen_at"`
	LastSeenGeneration int64   `json:"last_seen_generation"`
	MissingGenerations int     `json:"missing_generations"`
	RemovedAt          *string `json:"removed_at,omitempty"`
	ExcludedAt         *string `json:"excluded_at,omitempty"`
	ExcludeReason      string  `json:"exclude_reason,omitempty"`
}

func (a *API) handlePhotoAdminGet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	photo, err := a.deps.DB.Photos().Get(ctx, r.PathValue("id"))
	if err != nil {
		WriteError(w, r, notFoundAs(err, "photo"))
		return
	}
	loc := a.deps.Home.Location()
	dto := photoAdminDTO{
		photoDTO:           a.toPhotoDTO(photo, loc, a.loadPreviewSizes(ctx, []domain.Photo{*photo})),
		RelPath:            photo.RelPath,
		Ext:                photo.Ext,
		SizeBytes:          photo.SizeBytes,
		MtimeUnix:          photo.MtimeUnix,
		Fingerprint:        photo.Fingerprint,
		Status:             string(photo.Status),
		MetaStatus:         string(photo.MetaStatus),
		MetaError:          photo.MetaError,
		PreviewError:       photo.PreviewError,
		PreviewAttempts:    photo.PreviewAttempts,
		PreviewNextRetryAt: formatPtr(photo.PreviewNextRetryAt, loc),
		LastSeenAt:         photo.LastSeenAt.In(loc).Format(time.RFC3339),
		LastSeenGeneration: photo.LastSeenGeneration,
		MissingGenerations: photo.MissingGenerations,
		RemovedAt:          formatPtr(photo.RemovedAt, loc),
		ExcludedAt:         formatPtr(photo.ExcludedAt, loc),
		ExcludeReason:      photo.ExcludeReason,
	}
	a.audit(ctx, r, "photo.get", photo.ID, "")
	WriteJSON(w, http.StatusOK, dto)
}

type excludeRequest struct {
	Reason string `json:"reason"`
}

func (a *API) handlePhotoExclude(w http.ResponseWriter, r *http.Request) {
	var body excludeRequest
	if r.ContentLength > 0 {
		if err := DecodeJSON(w, r, &body); err != nil {
			WriteError(w, r, err)
			return
		}
	}
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	id := r.PathValue("id")
	photo, err := a.deps.DB.Photos().Get(ctx, id)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "photo"))
		return
	}
	now := a.now()
	if err := a.deps.DB.Photos().SetExcluded(ctx, id, true, body.Reason, now); err != nil {
		WriteError(w, r, err)
		return
	}
	// The rule is persisted separately so it survives a full rescan, which
	// would otherwise re-index the file as new.
	if err := a.deps.DB.Exclusions().Add(ctx, &domain.Exclusion{
		SourceID: photo.SourceID, MatchKind: domain.MatchPath,
		Pattern: photo.RelPath, Reason: body.Reason, CreatedAt: now,
	}); err != nil && !isConflict(err) {
		WriteError(w, r, err)
		return
	}
	a.dropPreviews(ctx, id)
	a.audit(ctx, r, "photo.exclude", id, body.Reason)
	a.publish(domain.TopicPhotos, domain.TopicHome)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handlePhotoInclude(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	id := r.PathValue("id")
	photo, err := a.deps.DB.Photos().Get(ctx, id)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "photo"))
		return
	}
	if err := a.deps.DB.Photos().SetExcluded(ctx, id, false, "", a.now()); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := a.removeMatchingExclusion(ctx, photo); err != nil {
		WriteError(w, r, err)
		return
	}
	if a.deps.Queue != nil {
		_ = a.deps.Queue.EnqueuePhoto(ctx, domain.JobExtractMeta, id, photo.SourceID, jobs.PriorityExtractMeta)
	}
	a.audit(ctx, r, "photo.include", id, "")
	a.publish(domain.TopicPhotos, domain.TopicHome)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handlePhotoRetry(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	id := r.PathValue("id")
	photo, err := a.deps.DB.Photos().Get(ctx, id)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "photo"))
		return
	}
	if err := a.deps.DB.Photos().Retry(ctx, id, a.now()); err != nil {
		WriteError(w, r, err)
		return
	}
	if a.deps.Queue != nil {
		if err := a.deps.Queue.EnqueuePhoto(ctx, domain.JobExtractMeta, id, photo.SourceID, jobs.PriorityExtractMeta); err != nil {
			WriteError(w, r, err)
			return
		}
	}
	a.audit(ctx, r, "photo.retry", id, "")
	w.WriteHeader(http.StatusAccepted)
}

// dropPreviews deletes the cached bytes of a photo that must stop being served.
func (a *API) dropPreviews(ctx context.Context, id string) {
	for _, v := range []domain.Variant{domain.VariantPreview, domain.VariantThumb} {
		if f, err := a.deps.DB.Previews().Get(ctx, id, v); err == nil && a.deps.Cache != nil {
			_ = a.deps.Cache.Remove(f.RelPath)
		}
	}
	_ = a.deps.DB.Previews().DeleteForPhoto(ctx, id)
}

// removeMatchingExclusion drops the persisted path rule for one photo.
func (a *API) removeMatchingExclusion(ctx context.Context, photo *domain.Photo) error {
	rows, err := a.deps.DB.Exclusions().List(ctx, photo.SourceID)
	if err != nil {
		return err
	}
	for i := range rows {
		if rows[i].MatchKind == domain.MatchPath && rows[i].Pattern == photo.RelPath {
			return a.deps.DB.Exclusions().Delete(ctx, rows[i].ID)
		}
	}
	return nil
}

func isConflict(err error) bool {
	e, ok := domain.AsError(err)
	if ok {
		return e.Code == domain.CodeConflict
	}
	return strings.Contains(err.Error(), "conflict")
}
