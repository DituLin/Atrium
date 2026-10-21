package httpapi

import (
	"net/http"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
)

func (a *API) archiveOverviewRoutes() {
	a.mux.HandleFunc("GET "+APIPrefix+"/archives/overview", a.requireScope(auth.ScopeScreen, a.archiveOverview))
}

func (a *API) archiveOverview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// Reject even malformed or empty query keys. This snapshot always covers
	// the household, never caller-supplied entity or pagination criteria.
	if r.URL.RawQuery != "" {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "invalid archive overview query"))
		return
	}
	identity := auth.FromContext(r.Context())
	if identity == nil || identity.Screen == nil {
		WriteError(w, r, domain.Errorf(domain.CodeUnauthorized, "screen unavailable"))
		return
	}
	h, err := a.deps.DB.LocalHousehold(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	result, err := a.deps.DB.ArchiveTasks().ArchiveOverviewForScreen(r.Context(), identity.Screen.ID, identity.Screen.TokenHash, h.ID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	integrationReply(a, w, result)
}
