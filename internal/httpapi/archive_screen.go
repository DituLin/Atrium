package httpapi

import (
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"net/http"
	"strconv"
)

func (a *API) archiveScreenRoutes() {
	a.mux.HandleFunc("POST "+APIPrefix+"/integrations/archives/views", a.requireIntegration("archive.read", a.archiveCreateView))
	a.mux.HandleFunc("GET "+APIPrefix+"/archives/views/{id}", a.requireScope(auth.ScopeScreen, a.archiveReadView))
	a.mux.HandleFunc("GET "+APIPrefix+"/archives/documents/{id}", a.requireScope(auth.ScopeScreen, a.archiveScreenDocument))
	a.mux.HandleFunc("GET "+APIPrefix+"/archives/documents/{id}/versions/{version}/pages/{page}", a.requireScope(auth.ScopeScreen, a.archiveScreenPreview))
}
func (a *API) archiveScreenDocument(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.URL.Query()) != 0 {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "invalid archive query"))
		return
	}
	identity := auth.FromContext(r.Context())
	if identity == nil || identity.Screen == nil {
		WriteError(w, r, domain.ErrNotFound)
		return
	}
	h, e := a.deps.DB.LocalHousehold(r.Context())
	if e != nil {
		WriteError(w, r, e)
		return
	}
	result, e := a.deps.DB.ArchiveTasks().ArchiveDocumentForScreen(r.Context(), identity.Screen.ID, identity.Screen.TokenHash, h.ID, r.PathValue("id"))
	if e != nil {
		WriteError(w, r, e)
		return
	}
	integrationReply(a, w, result)
}
func (a *API) archiveScreenPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	version, e := strconv.ParseInt(r.PathValue("version"), 10, 64)
	page, pe := strconv.Atoi(r.PathValue("page"))
	if e != nil || pe != nil || version < 1 || page < 1 || page > 100 || len(r.URL.Query()) != 0 || strconv.FormatInt(version, 10) != r.PathValue("version") || strconv.Itoa(page) != r.PathValue("page") {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "invalid archive preview"))
		return
	}
	identity := auth.FromContext(r.Context())
	if identity == nil || identity.Screen == nil {
		WriteError(w, r, domain.ErrNotFound)
		return
	}
	h, e := a.deps.DB.LocalHousehold(r.Context())
	if e != nil {
		WriteError(w, r, e)
		return
	}
	repo := a.deps.DB.ArchiveTasks()
	screen := identity.Screen
	ref, e := repo.ArchiveOriginalForScreen(r.Context(), screen.ID, screen.TokenHash, h.ID, r.PathValue("id"), version)
	if e != nil {
		WriteError(w, r, e)
		return
	}
	if a.deps.ArchiveBlobs == nil {
		WriteError(w, r, domain.ErrNotFound)
		return
	}
	f, e := a.deps.ArchiveBlobs.OpenVerified(r.Context(), ref)
	if e != nil {
		WriteError(w, r, domain.ErrNotFound)
		return
	}
	defer f.Close()
	result, e := a.deps.ArchivePreview.Preview(r.Context(), archive.NativeSource{File: f, SHA256: ref.SHA256}, page)
	if e != nil {
		WriteError(w, r, domain.Errorf(domain.CodeNotFound, "preview unavailable"))
		return
	}
	// Rendering releases all database locks. Revalidate after the potentially
	// slow child process so a concurrent withdrawal cannot publish its raster.
	check, e := repo.ArchiveOriginalForScreen(r.Context(), screen.ID, screen.TokenHash, h.ID, r.PathValue("id"), version)
	if e != nil {
		WriteError(w, r, e)
		return
	}
	if check != ref {
		WriteError(w, r, domain.ErrNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(result.PNG)))
	w.Header().Set("X-Archive-Pages", strconv.Itoa(result.TotalPages))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(result.PNG)
	}
}

func (a *API) archiveCreateView(w http.ResponseWriter, r *http.Request) {
	var input struct {
		QueryRef    string `json:"query_ref"`
		ScreenID    string `json:"screen_id"`
		OperationID string `json:"operation_id"`
	}
	if e := decodeIntegrationJSON(w, r, &input); e != nil {
		WriteError(w, r, e)
		return
	}
	if len(input.OperationID) != 26 || domain.ValidateArchiveIdentifier(input.OperationID) != nil || domain.ValidateArchiveIdentifier(input.QueryRef) != nil || domain.ValidateArchiveIdentifier(input.ScreenID) != nil {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "invalid archive view"))
		return
	}
	h, e := a.deps.DB.LocalHousehold(r.Context())
	if e != nil {
		WriteError(w, r, e)
		return
	}
	view, e := a.deps.DB.ArchiveTasks().CreateArchiveScreenView(r.Context(), auth.FromContext(r.Context()).IntegrationActor(), h.ID, input.QueryRef, input.ScreenID, input.OperationID)
	if e != nil {
		WriteError(w, r, e)
		return
	}
	integrationReply(a, w, view)
}
func (a *API) archiveReadView(w http.ResponseWriter, r *http.Request) {
	cursor := ""
	for k, v := range r.URL.Query() {
		if k != "cursor" || len(v) != 1 || len(v[0]) > 4096 {
			WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "invalid archive view"))
			return
		}
		cursor = v[0]
	}
	identity := auth.FromContext(r.Context())
	if identity == nil || identity.Screen == nil {
		WriteError(w, r, domain.ErrNotFound)
		return
	}
	h, e := a.deps.DB.LocalHousehold(r.Context())
	if e != nil {
		WriteError(w, r, e)
		return
	}
	screen := identity.Screen
	repo := a.deps.DB.ArchiveTasks()
	filter, e := repo.ArchiveScreenViewFilter(r.Context(), screen.ID, screen.TokenHash, h.ID, r.PathValue("id"))
	if e != nil {
		WriteError(w, r, e)
		return
	}
	if a.deps.Home != nil {
		filter.Timezone = a.deps.Home.Location().String()
	}
	filter.Cursor = cursor
	page, e := repo.SearchArchivesForScreen(r.Context(), screen.ID, screen.TokenHash, h.ID, filter, a.integrationCursorKey)
	if e != nil {
		WriteError(w, r, e)
		return
	}
	if a.deps.Home == nil {
		WriteError(w, r, domain.ErrNotFound)
		return
	}
	q := archive.AnalysisQuery{Operation: "count", Count: "documents", Ownership: filter.Ownership, Domain: filter.Domain, EntityID: filter.EntityID, Query: filter.Query, DocumentType: filter.DocumentType, DateFrom: filter.DateFrom, DateTo: filter.DateTo, Timezone: a.deps.Home.Location().String()}
	storedQuery, e := repo.ArchiveScreenViewAnalysis(r.Context(), screen.ID, screen.TokenHash, h.ID, r.PathValue("id"))
	if e != nil {
		WriteError(w, r, e)
		return
	}
	if storedQuery != nil {
		q = *storedQuery
		q.Timezone = a.deps.Home.Location().String()
	}
	analysis, e := repo.AnalyzeArchivesForScreen(r.Context(), screen.ID, screen.TokenHash, h.ID, q)
	if e != nil {
		WriteError(w, r, e)
		return
	}
	if page.Generation != fmt.Sprintf("%d:%d", analysis.DataEpoch, analysis.Generation) {
		WriteError(w, r, domain.ErrConflict)
		return
	}
	// The command's private IM wording is never sent to the television. Its
	// criteria execute only in Core; titles come from current public evidence.
	analysis.Query = archive.AnalysisQuery{Operation: analysis.Query.Operation, Count: analysis.Query.Count, Timezone: analysis.Query.Timezone}
	if _, e = repo.ArchiveScreenViewFilter(r.Context(), screen.ID, screen.TokenHash, h.ID, r.PathValue("id")); e != nil {
		WriteError(w, r, e)
		return
	}
	integrationReply(a, w, struct {
		ID       string                 `json:"archive_view_id"`
		Page     archive.SearchPage     `json:"page"`
		Analysis archive.AnalysisResult `json:"analysis"`
	}{r.PathValue("id"), page, analysis})
}
