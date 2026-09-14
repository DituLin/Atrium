package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/video"
)

func (a *API) handleVideoContent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := timeoutContext(r, 5*time.Second)
	scopes, err := a.videoScopes(ctx)
	if err != nil {
		cancel()
		WriteError(w, r, err)
		return
	}
	v, err := a.deps.DB.Videos().GetVisible(ctx, scopes, r.PathValue("id"))
	if err != nil {
		cancel()
		WriteError(w, r, notFoundAs(err, "video"))
		return
	}
	published, err := a.deps.DB.Videos().GetCover(ctx, scopes, v.ID)
	cancel()
	if err != nil {
		WriteError(w, r, domain.Errorf(domain.CodePreviewUnavailable, "video is not ready"))
		return
	}
	entry := a.deps.Sources.Get(v.SourceID)
	if entry == nil {
		WriteError(w, r, domain.Errorf(domain.CodeNotFound, "video not found"))
		return
	}
	guard := func(ctx context.Context) error {
		if _, err := a.deps.Auth.Authenticate(ctx, r, auth.ScopeScreen); err != nil {
			return video.ErrStale
		}
		scopes, err := a.videoScopes(ctx)
		if err != nil {
			return video.ErrStreamRead
		}
		current, err := a.deps.DB.Videos().GetCover(ctx, scopes, v.ID)
		if err != nil || current.Token != published.Token || current.Revision != v.Revision {
			return video.ErrStale
		}
		src, err := a.deps.DB.Sources().Get(ctx, v.SourceID)
		if err != nil || src.Health != domain.HealthOnline {
			return video.ErrStreamRead
		}
		cfg := entry.Config.Identity
		probe := source.CheckIdentity(ctx, entry.FS, source.IdentityConfig{RequireMount: cfg.RequireMount, AllowLocal: cfg.AllowLocal, MarkerFile: cfg.MarkerFile})
		if !probe.OK() {
			return video.ErrStreamRead
		}
		if src.IdentityBound == nil || probe.Identity == nil || !src.IdentityBound.Equal(*probe.Identity) {
			return video.ErrStale
		}
		info, err := entry.FS.Stat(ctx, v.RelPath)
		if err != nil {
			return video.ErrStreamRead
		}
		if !info.Mode().IsRegular() || info.Size() != v.SizeBytes || info.ModTime().Unix() != v.MtimeUnix {
			return video.ErrStale
		}
		return nil
	}
	stream, err := a.videoReads.Open(r.Context(), func(ctx context.Context) (io.ReadSeekCloser, error) {
		if err := guard(ctx); err != nil {
			return nil, err
		}
		reader, err := entry.FS.Open(ctx, v.RelPath)
		if err != nil {
			return nil, video.ErrStreamRead
		}
		file, ok := reader.(*os.File)
		if !ok {
			_ = reader.Close()
			return nil, video.ErrStreamRead
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != v.SizeBytes || info.ModTime().Unix() != v.MtimeUnix {
			_ = file.Close()
			return nil, video.ErrStale
		}
		return file, nil
	}, guard)
	if err != nil {
		code := domain.CodeSourceOffline
		if errors.Is(err, video.ErrStale) {
			code = domain.CodeNotFound
		}
		if errors.Is(err, video.ErrStreamBusy) {
			w.Header().Set("Retry-After", "2")
		}
		WriteError(w, r, domain.Errorf(code, "video content unavailable"))
		return
	}
	defer func() { _ = stream.Close() }()
	// The player uses single ranges. Refuse multipart amplification before any
	// bytes are emitted; ServeContent handles syntax, bounds and If-Range.
	if strings.Contains(r.Header.Get("Range"), ",") {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", v.SizeBytes))
		http.Error(w, "multiple ranges are not supported", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	contentType := "video/mp4"
	if v.Ext == "mov" {
		contentType = "video/quicktime"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%d-%s"`, v.ID, v.Revision, published.Token))
	controller := http.NewResponseController(w)
	defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
	http.ServeContent(videoDeadlineWriter{ResponseWriter: w, controller: controller}, r, "", time.Unix(v.MtimeUnix, 0), stream)
}

// Each output chunk has a fresh network deadline, so a slow client cannot hold
// an idle stream slot forever while preserving long, actively playing videos.
type videoDeadlineWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
}

func (w videoDeadlineWriter) Write(data []byte) (int, error) {
	if err := w.controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, err
	}
	return w.ResponseWriter.Write(data)
}
