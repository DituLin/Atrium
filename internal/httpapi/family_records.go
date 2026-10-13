package httpapi

import (
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"net/http"
	"strconv"
)

func (a *API) familyRecordRoutes() {
	a.mux.HandleFunc("GET "+APIPrefix+"/family/members", a.requireScope(auth.ScopeScreen, a.familyMembers))
	a.mux.HandleFunc("GET "+APIPrefix+"/family/records", a.requireScope(auth.ScopeScreen, a.familyRecords))
	a.mux.HandleFunc("PUT "+APIPrefix+"/admin/family/members/{id}", a.requireScope(auth.ScopeAdmin, a.familyPutMember))
}
func (a *API) familyPutMember(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeIntegrationJSON(w, r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	m := domain.FamilyMember{ID: r.PathValue("id"), Name: body.Name}
	if err := a.deps.DB.FamilyRecords().PutMember(r.Context(), m, a.now()); err != nil {
		WriteError(w, r, err)
		return
	}
	a.audit(r.Context(), r, "family.member.put", m.ID, "")
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, m)
}
func (a *API) familyMembers(w http.ResponseWriter, r *http.Request) {
	members, err := a.deps.DB.FamilyRecords().Members(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, map[string]any{"members": members})
}
func (a *API) integrationFamilyMembers(w http.ResponseWriter, r *http.Request) {
	members, err := a.deps.DB.FamilyRecords().Members(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	out := []domain.FamilyMember{}
	p := auth.FromContext(r.Context()).Integration
	for _, m := range members {
		if p.Policy.AllowsMember(m.ID) {
			out = append(out, m)
		}
	}
	integrationReply(a, w, map[string]any{"members": out})
}
func familyRecordLimit(r *http.Request) (int, error) {
	if r.URL.Query().Get("limit") == "" {
		return 100, nil
	}
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n < 1 || n > 500 {
		return 0, domain.Errorf(domain.CodeInvalidRequest, "limit must be 1 to 500")
	}
	return n, nil
}
func (a *API) familyRecords(w http.ResponseWriter, r *http.Request) {
	limit, err := familyRecordLimit(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	records, err := a.deps.DB.FamilyRecords().ListPage(r.Context(), r.URL.Query().Get("member_id"), true, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, records)
}
func (a *API) integrationFamilyRecords(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context()).Integration
	member := r.URL.Query().Get("member_id")
	if !p.Policy.AllowsMember(member) {
		WriteError(w, r, domain.ErrNotFound)
		return
	}
	limit, err := familyRecordLimit(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	records, err := a.deps.DB.FamilyRecords().ListPage(r.Context(), member, false, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		WriteError(w, r, err)
		return
	}
	integrationReply(a, w, records)
}
func (a *API) integrationFamilyRecord(w http.ResponseWriter, r *http.Request) {
	var body struct {
		domain.FamilyRecordInput
		RequestID string `json:"request_id"`
	}
	if err := decodeIntegrationJSON(w, r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	p := auth.FromContext(r.Context()).Integration
	if !p.Policy.AllowsMember(body.MemberID) {
		WriteError(w, r, domain.ErrNotFound)
		return
	}
	record, replayed, err := a.deps.DB.FamilyRecords().Record(r.Context(), p.ID, body.RequestID, body.FamilyRecordInput, a.now())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	nas := "pending"
	if record.ExportedAt != nil {
		nas = "synced"
	}
	if !replayed {
		a.audit(r.Context(), r, "family.record.create", record.ID, "")
	}
	integrationReply(a, w, map[string]any{"record": record, "replayed": replayed, "nas_status": nas})
}
