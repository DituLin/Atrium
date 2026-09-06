package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
)

type pairingDTO struct {
	ID               string  `json:"id"`
	Code             string  `json:"code"`
	Status           string  `json:"status"`
	ClientHint       string  `json:"client_hint,omitempty"`
	RemoteIP         string  `json:"remote_ip,omitempty"`
	CreatedAt        string  `json:"created_at"`
	ExpiresAt        string  `json:"expires_at"`
	ApprovedScreenID string  `json:"approved_screen_id,omitempty"`
	ApprovedAt       *string `json:"approved_at,omitempty"`
	ClaimedAt        *string `json:"claimed_at,omitempty"`
}

type pairingListResponse struct {
	Pairings []pairingDTO `json:"pairings"`
}

type approveRequest struct {
	ScreenID string `json:"screen_id"`
	Name     string `json:"name"`
}

type approveResponse struct {
	Pairing pairingDTO `json:"pairing"`
	Screen  screenDTO  `json:"screen"`
}

func toPairingDTO(p *domain.Pairing) pairingDTO {
	dto := pairingDTO{
		ID: p.ID, Code: p.Code, Status: string(p.Status),
		ClientHint: p.ClientHint, RemoteIP: p.RemoteIP,
		CreatedAt:        p.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:        p.ExpiresAt.UTC().Format(time.RFC3339),
		ApprovedScreenID: p.ApprovedScreenID,
	}
	if p.ApprovedAt != nil {
		v := p.ApprovedAt.UTC().Format(time.RFC3339)
		dto.ApprovedAt = &v
	}
	if p.ClaimedAt != nil {
		v := p.ClaimedAt.UTC().Format(time.RFC3339)
		dto.ClaimedAt = &v
	}
	return dto
}

func (a *API) handlePairingsList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	rows, err := a.deps.Pairing.List(ctx, 100)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	out := pairingListResponse{Pairings: make([]pairingDTO, 0, len(rows))}
	for i := range rows {
		out.Pairings = append(out.Pairings, toPairingDTO(&rows[i]))
	}
	WriteJSON(w, http.StatusOK, out)
}

func (a *API) handlePairingApprove(w http.ResponseWriter, r *http.Request) {
	var req approveRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		WriteError(w, r, err)
		return
	}
	if req.ScreenID == "" {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "screen_id is required"))
		return
	}
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	res, err := a.deps.Pairing.Approve(ctx, r.PathValue("id"), req.ScreenID, req.Name)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "pairing"))
		return
	}
	a.audit(ctx, r, "pairing.approve", res.Pairing.ID, res.Screen.ID)
	WriteJSON(w, http.StatusOK, approveResponse{
		Pairing: toPairingDTO(res.Pairing),
		Screen:  a.toScreenDTO(res.Screen, a.now()),
	})
}

func (a *API) handlePairingReject(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	id := r.PathValue("id")
	if err := a.deps.Pairing.Reject(ctx, id); err != nil {
		WriteError(w, r, notFoundAs(err, "pairing"))
		return
	}
	a.audit(ctx, r, "pairing.reject", id, "")
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	id := auth.FromContext(r.Context())
	if id != nil && id.Screen != nil {
		_ = a.deps.DB.Screens().TouchSeen(ctx, id.Screen.ID, auth.ClientIP(r), r.Header.Get("X-Client-Version"), a.now())
	}
	snap, err := a.deps.Snapshot.Compose(ctx, id.IsAdmin())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if a.deps.Bus != nil {
		snap.Version = a.deps.Bus.Version()
	}
	WriteJSON(w, http.StatusOK, snap)
}

func (a *API) handleNASStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	snap, err := a.deps.Snapshot.Compose(ctx, auth.FromContext(r.Context()).IsAdmin())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, snap.NAS)
}

// handleDiagnostics renders the operator document of design §6.11. The
// aggregation lives in internal/diag so the CLI and the API share one shape.
func (a *API) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if a.deps.Diagnostics == nil {
		WriteError(w, r, domain.Errorf(domain.CodeInternal, "diagnostics are not available"))
		return
	}
	ctx, cancel := timeoutContext(r, 10*time.Second)
	defer cancel()
	doc, err := a.deps.Diagnostics.Collect(ctx)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, doc)
}

// audit records an administrative action; failures never break the request.
func (a *API) audit(ctx context.Context, r *http.Request, action, target, detail string) {
	id := auth.FromContext(r.Context())
	actor := "unknown"
	if id != nil {
		actor = string(id.Scope)
		if id.AdminTokenID != "" {
			actor = "admin:" + id.AdminTokenID
		}
	}
	_ = a.deps.DB.Audit().Append(ctx, &domain.AuditEntry{
		At: a.now(), Actor: actor, Action: action, Target: target, Detail: detail,
	})
}
