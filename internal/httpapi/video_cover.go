package httpapi

import (
	"bytes"
	"errors"
	"image/jpeg"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/video"
)

func (a *API) handleVideoCover(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	w.Header().Set("Cache-Control", "no-store")
	scopes, err := a.videoScopes(ctx)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	v, err := a.deps.DB.Videos().GetVisible(ctx, scopes, r.PathValue("id"))
	if err != nil {
		WriteError(w, r, notFoundAs(err, "video"))
		return
	}
	if v.Status == domain.VideoUnsupported {
		WriteError(w, r, domain.Errorf(domain.CodeNotFound, "video has no cover"))
		return
	}
	cover, err := a.deps.DB.Videos().GetCover(ctx, scopes, v.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			a.writeVideoCoverPending(w, r, v)
			return
		}
		WriteError(w, r, err)
		return
	}
	if a.deps.VideoCache == nil {
		WriteError(w, r, domain.Errorf(domain.CodePreviewUnavailable, "video cover cache unavailable"))
		return
	}
	data, err := a.deps.VideoCache.Read(cover.Token)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, video.ErrOutputLimit) {
			_ = a.deps.VideoCache.Remove(cover.Token)
			_, _ = a.deps.DB.VideoWork().ForgetCover(ctx, cover.Token, a.deps.Now())
			a.writeVideoCoverPending(w, r, v)
			return
		}
		WriteError(w, r, domain.Errorf(domain.CodePreviewUnavailable, "video cover unavailable"))
		return
	}
	dimensions, decodeErr := jpeg.DecodeConfig(bytes.NewReader(data))
	if decodeErr != nil || int64(len(data)) != cover.Bytes || dimensions.Width != cover.Width || dimensions.Height != cover.Height {
		_ = a.deps.VideoCache.Remove(cover.Token)
		_, _ = a.deps.DB.VideoWork().ForgetCover(ctx, cover.Token, a.deps.Now())
		a.writeVideoCoverPending(w, r, v)
		return
	}
	// Recheck authority and publication after reading the local file, before
	// sending any bytes. Never let cached data bypass a source revocation.
	scopes, err = a.videoScopes(ctx)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	current, err := a.deps.DB.Videos().GetCover(ctx, scopes, v.ID)
	if err != nil || current.Token != cover.Token {
		WriteError(w, r, domain.Errorf(domain.CodeNotFound, "video cover not found"))
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *API) writeVideoCoverPending(w http.ResponseWriter, r *http.Request, v *domain.Video) {
	src, err := a.deps.DB.Sources().Get(r.Context(), v.SourceID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if src.Status != domain.SourceActive {
		WriteError(w, r, domain.Errorf(domain.CodeNotFound, "video not found"))
		return
	}
	if src.Health != domain.HealthOnline {
		WriteError(w, r, domain.Errorf(domain.CodeSourceOffline, "video source is offline and no cover is cached"))
		return
	}
	w.Header().Set("Retry-After", "5")
	WriteJSON(w, http.StatusAccepted, processingBody{Status: "video_processing", RetryAfterSecond: 5})
}
