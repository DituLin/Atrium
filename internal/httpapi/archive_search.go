package httpapi

import (
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"net/http"
	"strconv"
)

func (a *API) archiveSearchRoutes() {
	a.archiveAnalysisRoutes()
	a.archiveScreenRoutes()
	prefix := APIPrefix + "/integrations/archives"
	for _, route := range []struct{ path, kind string }{{"/search", "search"}, {"/entities", "entities"}, {"/documents/{id}/evidence", "evidence"}} {
		kind := route.kind
		a.mux.HandleFunc("GET "+prefix+route.path, a.requireIntegration("archive.read", func(w http.ResponseWriter, r *http.Request) { a.archiveRead(w, r, kind, false) }))
		a.mux.HandleFunc("GET "+APIPrefix+"/archives"+route.path, a.requireScope(auth.ScopeScreen, func(w http.ResponseWriter, r *http.Request) { a.archiveRead(w, r, kind, true) }))
	}
}
func archiveFilter(r *http.Request, kind string) (archive.SearchFilter, error) {
	f := archive.SearchFilter{}
	q := r.URL.Query()
	for key, values := range q {
		if len(values) != 1 {
			return f, domain.Errorf(domain.CodeInvalidRequest, "invalid archive query")
		}
		value := values[0]
		switch key {
		case "query":
			f.Query = value
		case "entity_id":
			f.EntityID = value
		case "domain":
			f.Domain = value
		case "document_type":
			f.DocumentType = value
		case "date_from":
			f.DateFrom = value
		case "date_to":
			f.DateTo = value
		case "ownership":
			f.Ownership = value
		case "limit":
			n, e := strconv.Atoi(value)
			if e != nil {
				return f, domain.Errorf(domain.CodeInvalidRequest, "invalid archive query")
			}
			f.Limit = n
		case "cursor":
			f.Cursor = value
		default:
			return f, domain.Errorf(domain.CodeInvalidRequest, "invalid archive query")
		}
		if kind == "evidence" && key != "limit" && key != "cursor" {
			return f, domain.Errorf(domain.CodeInvalidRequest, "invalid archive query")
		}
	}
	f, e := f.Normalize()
	if e != nil {
		return f, domain.Errorf(domain.CodeInvalidRequest, "invalid archive query")
	}
	return f, nil
}
func (a *API) archiveRead(w http.ResponseWriter, r *http.Request, kind string, screen bool) {
	f, e := archiveFilter(r, kind)
	if e != nil {
		WriteError(w, r, e)
		return
	}
	if a.deps.Home != nil {
		f.Timezone = a.deps.Home.Location().String()
	}
	h, e := a.deps.DB.LocalHousehold(r.Context())
	if e != nil {
		WriteError(w, r, e)
		return
	}
	identity := auth.FromContext(r.Context())
	repo := a.deps.DB.ArchiveTasks()
	key := a.integrationCursorKey
	var result any
	if screen {
		if identity == nil || identity.Screen == nil {
			WriteError(w, r, domain.Errorf(domain.CodeUnauthorized, "screen unavailable"))
			return
		}
		id, hash := identity.Screen.ID, identity.Screen.TokenHash
		switch kind {
		case "search":
			result, e = repo.SearchArchivesForScreen(r.Context(), id, hash, h.ID, f, key)
		case "entities":
			result, e = repo.SearchArchiveEntitiesForScreen(r.Context(), id, hash, h.ID, f, key)
		case "evidence":
			result, e = repo.ArchiveEvidenceForScreen(r.Context(), id, hash, h.ID, r.PathValue("id"), f, key)
		}
	} else {
		actor := identity.IntegrationActor()
		switch kind {
		case "search":
			result, e = repo.SearchArchives(r.Context(), actor, h.ID, false, f, key)
		case "entities":
			result, e = repo.SearchArchiveEntities(r.Context(), actor, h.ID, false, f, key)
		case "evidence":
			result, e = repo.ArchiveEvidence(r.Context(), actor, h.ID, false, r.PathValue("id"), f, key)
		}
	}
	if e != nil {
		WriteError(w, r, e)
		return
	}
	integrationReply(a, w, result)
}
