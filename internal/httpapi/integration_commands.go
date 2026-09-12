package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/integration"
)

func (a *API) integrationCommandIssue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OperationID string                `json:"operation_id"`
		Kind        domain.CommandKind    `json:"kind"`
		Payload     domain.CommandPayload `json:"payload"`
	}
	if err := decodeIntegrationJSON(w, r, &req); err != nil {
		WriteError(w, r, err)
		return
	}
	if a.deps.Commands == nil {
		WriteError(w, r, domain.Errorf(domain.CodeInternal, "command service unavailable"))
		return
	}
	p := auth.FromContext(r.Context()).Integration
	cmd, err := a.deps.Commands.IssueIntegration(r.Context(), p.ID, req.OperationID, r.PathValue("id"), req.Kind, req.Payload)
	status := http.StatusOK
	if err != nil {
		var apiErr *domain.Error
		if cmd == nil || !errors.As(err, &apiErr) || apiErr.Code != domain.CodeScreenOffline {
			WriteError(w, r, err)
			return
		}
		status = http.StatusConflict
	} else if cmd.Status == domain.CommandAccepted {
		status = http.StatusAccepted
	}
	a.audit(r.Context(), r, "integration.command", cmd.ID, "operation:"+req.OperationID)
	if log := LoggerFrom(r.Context()); log != nil {
		log.InfoContext(r.Context(), "Integration command result", "event", "integration.command", "operation_id", req.OperationID, "command_id", cmd.ID, "command_status", string(cmd.Status), "error_code", safeIntegrationErrorCode(cmd.ErrorCode))
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, status, integration.Response[integration.Command]{SchemaVersion: integration.SchemaVersion, ObservedAt: a.now(), Availability: "available", Data: a.safeIntegrationCommand(r.Context(), p, cmd)})
}

func (a *API) integrationCommandGet(w http.ResponseWriter, r *http.Request) {
	a.integrationCommandLookup(w, r, false)
}
func (a *API) integrationOperationGet(w http.ResponseWriter, r *http.Request) {
	a.integrationCommandLookup(w, r, true)
}
func (a *API) integrationCommandLookup(w http.ResponseWriter, r *http.Request, operation bool) {
	if a.deps.Commands == nil {
		WriteError(w, r, domain.Errorf(domain.CodeInternal, "command service unavailable"))
		return
	}
	p := auth.FromContext(r.Context()).Integration
	var cmd *domain.Command
	var err error
	if operation {
		cmd, err = a.deps.Commands.GetIntegrationOperation(r.Context(), p.ID, r.PathValue("id"))
	} else {
		cmd, err = a.deps.Commands.GetIntegrationCommand(r.Context(), p.ID, r.PathValue("id"))
	}
	if err != nil {
		WriteError(w, r, notFoundAs(err, "command"))
		return
	}
	integrationReply(a, w, a.safeIntegrationCommand(r.Context(), p, cmd))
}

func (a *API) safeIntegrationCommand(ctx context.Context, p *domain.IntegrationPrincipal, c *domain.Command) integration.Command {
	dto := integration.Command{ID: c.ID, ScreenID: c.ScreenID, Sequence: c.Sequence, Kind: string(c.Kind), Payload: integration.CommandPayload{Route: string(c.Payload.Route), Collection: c.Payload.Collection, PhotoID: c.Payload.PhotoID}, IssuedAt: c.IssuedAt, ExpiresAt: c.ExpiresAt, Status: string(c.Status), ErrorCode: safeIntegrationErrorCode(c.ErrorCode)}
	// TV acknowledgements are untrusted input: project each resource separately.
	if c.Result != nil {
		dto.Result = &integration.CommandResult{}
		if c.Result.Route != nil {
			dto.Result.Route = a.safeIntegrationRoute(ctx, p, c.Result.Route)
		}
		if id := c.Result.ResourceID; id != "" && p.Policy.Allows("photos.read") {
			if _, err := a.deps.DB.Photos().GetScoped(ctx, p.Policy.SourceIDs, id); err == nil {
				dto.Result.ResourceID = id
			}
		}
	}
	return dto
}

func (a *API) safeIntegrationRoute(ctx context.Context, p *domain.IntegrationPrincipal, route *domain.RouteState) *integration.Route {
	dto := &integration.Route{Name: "unknown", ContentVisible: false}
	if route.Name.Valid() {
		dto.Name = string(route.Name)
		dto.ContentVisible = route.PhotoID == ""
	}
	if route.Name == domain.RoutePhotos && domain.Collection(route.Collection).Valid() {
		dto.Collection = route.Collection
	}
	if route.PhotoID != "" && p.Policy.Allows("photos.read") {
		if _, err := a.deps.DB.Photos().GetScoped(ctx, p.Policy.SourceIDs, route.PhotoID); err == nil {
			dto.PhotoID = route.PhotoID
			dto.ContentVisible = true
		}
	}
	return dto
}

func safeIntegrationErrorCode(code string) string {
	switch code {
	case "", "screen_offline", "delivery_failed", "expired", "ack_timeout", "server_restart", "session_replaced", "photo_unavailable", "render_failed", "invalid_route", "superseded":
		return code
	default:
		return "unknown"
	}
}
