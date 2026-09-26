package httpapi

import (
	"net/http"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
)

type integrationIssueRequest struct {
	Label     string                   `json:"label"`
	Policy    domain.IntegrationPolicy `json:"policy"`
	ExpiresAt time.Time                `json:"expires_at"`
}

type integrationCredentialResponse struct {
	Principal *domain.IntegrationPrincipal `json:"principal"`
	Token     string                       `json:"token"`
}

func (a *API) adminIntegrationRoutes() {
	prefix := APIPrefix + "/admin/integrations"
	a.mux.HandleFunc("POST "+prefix, a.requireScope(auth.ScopeAdmin, a.adminIntegrationIssue))
	a.mux.HandleFunc("GET "+prefix, a.requireScope(auth.ScopeAdmin, a.adminIntegrationList))
	a.mux.HandleFunc("POST "+prefix+"/{id}/rotate", a.requireScope(auth.ScopeAdmin, a.adminIntegrationRotate))
	a.mux.HandleFunc("PUT "+prefix+"/{id}/policy", a.requireScope(auth.ScopeAdmin, a.adminIntegrationPolicy))
	a.mux.HandleFunc("DELETE "+prefix+"/{id}", a.requireScope(auth.ScopeAdmin, a.adminIntegrationRevoke))
}

func (a *API) adminIntegrationIssue(w http.ResponseWriter, r *http.Request) {
	var req integrationIssueRequest
	if err := decodeIntegrationJSON(w, r, &req); err != nil {
		WriteError(w, r, err)
		return
	}
	p, token, err := auth.NewIntegrationService(a.deps.DB, a.deps.Now).Issue(r.Context(), req.Label, req.Policy, req.ExpiresAt)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	a.audit(r.Context(), r, "integration.issue", p.ID, "")
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusCreated, integrationCredentialResponse{Principal: p, Token: token})
}

func (a *API) adminIntegrationList(w http.ResponseWriter, r *http.Request) {
	principals, err := a.deps.DB.Integrations().List(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if principals == nil {
		principals = []domain.IntegrationPrincipal{}
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, map[string]any{"principals": principals})
}

func (a *API) adminIntegrationRotate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := decodeIntegrationJSON(w, r, &req); err != nil {
		WriteError(w, r, err)
		return
	}
	if req.ExpiresAt.IsZero() {
		p, err := a.deps.DB.Integrations().Get(r.Context(), r.PathValue("id"))
		if err != nil {
			WriteError(w, r, notFoundAs(err, "integration"))
			return
		}
		req.ExpiresAt = p.ExpiresAt
	}
	p, token, err := auth.NewIntegrationService(a.deps.DB, a.deps.Now).Rotate(r.Context(), r.PathValue("id"), req.ExpiresAt)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "integration"))
		return
	}
	a.audit(r.Context(), r, "integration.rotate", p.ID, "")
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, integrationCredentialResponse{Principal: p, Token: token})
}

func (a *API) adminIntegrationPolicy(w http.ResponseWriter, r *http.Request) {
	var policy domain.IntegrationPolicy
	if err := decodeIntegrationJSON(w, r, &policy); err != nil {
		WriteError(w, r, err)
		return
	}
	p, err := auth.NewIntegrationService(a.deps.DB, a.deps.Now).UpdatePolicy(r.Context(), r.PathValue("id"), policy)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "integration"))
		return
	}
	a.audit(r.Context(), r, "integration.policy", p.ID, "")
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, p)
}

func (a *API) adminIntegrationRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := auth.NewIntegrationService(a.deps.DB, a.deps.Now).Revoke(r.Context(), id); err != nil {
		WriteError(w, r, notFoundAs(err, "integration"))
		return
	}
	a.audit(r.Context(), r, "integration.revoke", id, "")
	w.WriteHeader(http.StatusNoContent)
}
