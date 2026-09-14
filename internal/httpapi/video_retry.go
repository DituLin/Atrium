package httpapi

import (
	"net/http"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

func (a *API) handleVideoRetry(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Revision int64 `json:"revision"`
	}
	if err := DecodeJSON(w, r, &request); err != nil {
		WriteError(w, r, err)
		return
	}
	if request.Revision < 1 {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "revision must be positive"))
		return
	}
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
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
	updated, err := a.deps.DB.VideoWork().RequestRetry(ctx, v.ID, request.Revision, a.now())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if !updated {
		WriteError(w, r, domain.Errorf(domain.CodeConflict, "video changed; refresh before retrying"))
		return
	}
	a.audit(ctx, r, "video.retry", v.ID, "")
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusAccepted, struct {
		Status   string `json:"status"`
		Revision int64  `json:"revision"`
	}{"pending", request.Revision + 1})
}
