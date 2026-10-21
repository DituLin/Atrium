package httpapi

import (
	"net/http"

	"github.com/DituLin/Atrium/internal/archive"
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
	// Configuration profiles are independent, human-confirmed facts. A name or
	// ID in private archive storage is never sufficient to publish them: use
	// only the authorized snapshot's complete set of publicly named entities.
	if result.Coverage.Complete && a.deps.Config != nil {
		visible := make(map[string]bool, len(result.Entities))
		for _, entity := range result.Entities {
			visible[entity.ID] = true
		}
		for _, profile := range a.deps.Config.Archives.GrowthProfiles {
			if visible[profile.EntityID] {
				result.GrowthProfiles = append(result.GrowthProfiles, archive.PublicGrowthProfile{
					EntityID: profile.EntityID, BirthDate: profile.BirthDate,
					Sex: profile.Sex, HeightMeasurement: profile.HeightMeasurement,
				})
			}
		}
	}
	integrationReply(a, w, result)
}
