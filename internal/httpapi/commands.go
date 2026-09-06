package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/domain"
)

// commandDTO is the wire shape of a screen command (design §8).
type commandDTO struct {
	ID          string                `json:"id"`
	ScreenID    string                `json:"screen_id"`
	Sequence    int64                 `json:"sequence"`
	Kind        string                `json:"kind"`
	Payload     domain.CommandPayload `json:"payload"`
	IssuedBy    string                `json:"issued_by,omitempty"`
	IssuedAt    string                `json:"issued_at"`
	ExpiresAt   string                `json:"expires_at"`
	Status      string                `json:"status"`
	DeliveredAt *string               `json:"delivered_at,omitempty"`
	ResolvedAt  *string               `json:"resolved_at,omitempty"`
	ErrorCode   string                `json:"error_code,omitempty"`
	Result      *domain.CommandResult `json:"result,omitempty"`
}

type commandListResponse struct {
	Commands []commandDTO `json:"commands"`
}

type issueCommandRequest struct {
	Kind    string                `json:"kind"`
	Payload domain.CommandPayload `json:"payload"`
}

func toCommandDTO(c *domain.Command) commandDTO {
	dto := commandDTO{
		ID: c.ID, ScreenID: c.ScreenID, Sequence: c.Sequence, Kind: string(c.Kind),
		Payload: c.Payload, IssuedBy: c.IssuedBy,
		IssuedAt:  c.IssuedAt.UTC().Format(time.RFC3339),
		ExpiresAt: c.ExpiresAt.UTC().Format(time.RFC3339),
		Status:    string(c.Status), ErrorCode: c.ErrorCode, Result: c.Result,
	}
	if c.DeliveredAt != nil {
		v := c.DeliveredAt.UTC().Format(time.RFC3339)
		dto.DeliveredAt = &v
	}
	if c.ResolvedAt != nil {
		v := c.ResolvedAt.UTC().Format(time.RFC3339)
		dto.ResolvedAt = &v
	}
	return dto
}

// handleCommandIssue accepts a command for one screen. An offline screen is a
// 409 that still carries the persisted command, so the caller can see exactly
// what was recorded rather than guessing (PRD §5.3).
func (a *API) handleCommandIssue(w http.ResponseWriter, r *http.Request) {
	if a.deps.Commands == nil {
		WriteError(w, r, domain.Errorf(domain.CodeInternal, "command service is not available"))
		return
	}
	var req issueCommandRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		WriteError(w, r, err)
		return
	}
	kind := domain.CommandKind(req.Kind)
	if !kind.Valid() {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidCommand, "unknown command kind %q", req.Kind))
		return
	}
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()

	screenID := r.PathValue("id")
	cmd, err := a.deps.Commands.Issue(ctx, screenID, kind, req.Payload, actorOf(r))
	if err != nil {
		var apiErr *domain.Error
		if errors.As(err, &apiErr) && apiErr.Code == domain.CodeScreenOffline && cmd != nil {
			a.audit(ctx, r, "command.issue", screenID, string(kind)+":screen_offline")
			WriteJSONError(w, http.StatusConflict, apiErr, map[string]any{
				"command": toCommandDTO(cmd),
			})
			return
		}
		WriteError(w, r, err)
		return
	}
	a.audit(ctx, r, "command.issue", screenID, string(kind)+":"+cmd.ID)
	WriteJSON(w, http.StatusAccepted, toCommandDTO(cmd))
}

func (a *API) handleCommandGet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	cmd, err := a.deps.DB.Commands().Get(ctx, r.PathValue("id"))
	if err != nil {
		WriteError(w, r, notFoundAs(err, "command"))
		return
	}
	WriteJSON(w, http.StatusOK, toCommandDTO(cmd))
}

func (a *API) handleCommandsList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	q := r.URL.Query()
	status := domain.CommandStatus(q.Get("status"))
	if status != "" && !status.Valid() {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "unknown status %q", status))
		return
	}
	limit := 50
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "limit must be between 1 and 200"))
			return
		}
		limit = n
	}
	rows, err := a.deps.DB.Commands().List(ctx, q.Get("screen_id"), status, limit)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	out := commandListResponse{Commands: make([]commandDTO, 0, len(rows))}
	for i := range rows {
		out.Commands = append(out.Commands, toCommandDTO(&rows[i]))
	}
	WriteJSON(w, http.StatusOK, out)
}

// actorOf names who issued a command, without revealing the credential.
func actorOf(r *http.Request) string {
	id := auth.FromContext(r.Context())
	if id == nil {
		return "unknown"
	}
	if id.AdminTokenID != "" {
		return "admin:" + id.AdminTokenID
	}
	return string(id.Scope)
}
