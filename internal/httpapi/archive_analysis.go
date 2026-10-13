package httpapi

import (
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"net/http"
)

func (a *API) archiveAnalysisRoutes() {
	for _, tail := range []string{"/analysis", "/analysis/{id}"} {
		a.mux.HandleFunc("GET "+APIPrefix+"/integrations/archives"+tail, a.requireIntegration("archive.analyze", func(w http.ResponseWriter, r *http.Request) { a.archiveAnalyze(w, r, false) }))
		a.mux.HandleFunc("GET "+APIPrefix+"/archives"+tail, a.requireScope(auth.ScopeScreen, func(w http.ResponseWriter, r *http.Request) { a.archiveAnalyze(w, r, true) }))
	}
}
func (a *API) archiveAnalyze(w http.ResponseWriter, r *http.Request, screen bool) {
	if a.deps.Home == nil {
		WriteError(w, r, domain.Errorf(domain.CodeInternal, "home timezone unavailable"))
		return
	}
	q := archive.AnalysisQuery{Timezone: a.deps.Home.Location().String()}
	for k, v := range r.URL.Query() {
		if len(v) != 1 || r.PathValue("id") != "" {
			WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "invalid analysis query"))
			return
		}
		value := v[0]
		switch k {
		case "ownership":
			q.Ownership = value
		case "operation":
			q.Operation = value
		case "count":
			q.Count = value
		case "metric":
			q.Metric = value
		case "entity_id":
			q.EntityID = value
		case "domain":
			q.Domain = value
		case "document_type":
			q.DocumentType = value
		case "query":
			q.Query = value
		case "date_from":
			q.DateFrom = value
		case "date_to":
			q.DateTo = value
		default:
			WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "invalid analysis query"))
			return
		}
	}
	h, e := a.deps.DB.LocalHousehold(r.Context())
	if e != nil {
		WriteError(w, r, e)
		return
	}
	identity := auth.FromContext(r.Context())
	repo := a.deps.DB.ArchiveTasks()
	var result archive.AnalysisResult
	if screen {
		if identity == nil || identity.Screen == nil {
			WriteError(w, r, domain.Errorf(domain.CodeUnauthorized, "screen unavailable"))
			return
		}
		if id := r.PathValue("id"); id != "" {
			result, e = repo.ArchiveAnalysisSnapshotForScreen(r.Context(), identity.Screen.ID, identity.Screen.TokenHash, h.ID, id)
		} else {
			result, e = repo.AnalyzeArchivesForScreen(r.Context(), identity.Screen.ID, identity.Screen.TokenHash, h.ID, q)
		}
	} else {
		if id := r.PathValue("id"); id != "" {
			result, e = repo.ArchiveAnalysisSnapshot(r.Context(), identity.IntegrationActor(), h.ID, false, id)
		} else {
			result, e = repo.AnalyzeArchives(r.Context(), identity.IntegrationActor(), h.ID, false, q)
		}
	}
	if e != nil {
		WriteError(w, r, e)
		return
	}
	integrationReply(a, w, result)
}
