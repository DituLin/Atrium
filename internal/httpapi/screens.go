package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
)

// screenDTO is the admin and self view of a paired screen. It never carries the
// token hash.
type screenDTO struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Status          string             `json:"status"`
	Registered      bool               `json:"registered"`
	Online          bool               `json:"online"`
	CreatedAt       string             `json:"created_at"`
	ApprovedAt      string             `json:"approved_at"`
	RevokedAt       *string            `json:"revoked_at,omitempty"`
	LastSeenAt      *string            `json:"last_seen_at,omitempty"`
	ClientVersion   string             `json:"client_version,omitempty"`
	CurrentRoute    *domain.RouteState `json:"current_route,omitempty"`
	LastSequence    int64              `json:"last_sequence"`
	AppliedSequence int64              `json:"applied_sequence"`
}

type screenListResponse struct {
	Screens []screenDTO `json:"screens"`
}

// toScreenDTO renders a screen. `registered` is approval state; `online` is
// session presence, which since V0.3 means a live WebSocket session with a
// heartbeat inside offline_after (design §6.5). Without a hub (unit tests,
// early boot) it falls back to the last-seen timestamp.
func (a *API) toScreenDTO(s *domain.Screen, now time.Time) screenDTO {
	dto := screenDTO{
		ID:              s.ID,
		Name:            s.Name,
		Status:          string(s.Status),
		Registered:      s.Status == domain.ScreenActive,
		CreatedAt:       s.CreatedAt.UTC().Format(time.RFC3339),
		ApprovedAt:      s.ApprovedAt.UTC().Format(time.RFC3339),
		CurrentRoute:    s.CurrentRoute,
		ClientVersion:   s.ClientVersion,
		LastSequence:    s.LastSequence,
		AppliedSequence: s.AppliedSequence,
	}
	if s.RevokedAt != nil {
		v := s.RevokedAt.UTC().Format(time.RFC3339)
		dto.RevokedAt = &v
	}
	if s.LastSeenAt != nil {
		v := s.LastSeenAt.UTC().Format(time.RFC3339)
		dto.LastSeenAt = &v
	}
	switch {
	case a.deps.Sessions != nil:
		dto.Online = dto.Registered && a.deps.Sessions.Online(s.ID)
	case s.LastSeenAt != nil:
		dto.Online = dto.Registered && now.Sub(*s.LastSeenAt) <= a.deps.Config.Screens.OfflineAfter.D()
	}
	return dto
}

func (a *API) handleScreenMe(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if id == nil || id.Screen == nil {
		WriteError(w, r, domain.Errorf(domain.CodeForbidden, "route requires a screen credential"))
		return
	}
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	now := a.now()
	// A successful authenticated request is presence information.
	_ = a.deps.DB.Screens().TouchSeen(ctx, id.Screen.ID, auth.ClientIP(r), r.Header.Get("X-Client-Version"), now)
	sc, err := a.deps.DB.Screens().Get(ctx, id.Screen.ID)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "screen"))
		return
	}
	WriteJSON(w, http.StatusOK, a.toScreenDTO(sc, now))
}

func (a *API) handleScreensList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	rows, err := a.deps.DB.Screens().List(ctx)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	now := a.now()
	out := screenListResponse{Screens: make([]screenDTO, 0, len(rows))}
	for i := range rows {
		out.Screens = append(out.Screens, a.toScreenDTO(&rows[i], now))
	}
	WriteJSON(w, http.StatusOK, out)
}

func (a *API) handleScreenGet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	sc, err := a.deps.DB.Screens().Get(ctx, r.PathValue("id"))
	if err != nil {
		WriteError(w, r, notFoundAs(err, "screen"))
		return
	}
	WriteJSON(w, http.StatusOK, a.toScreenDTO(sc, a.now()))
}

func (a *API) handleScreenRevoke(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	id := r.PathValue("id")
	now := a.now()
	if err := a.deps.DB.Screens().Revoke(ctx, id, now); err != nil {
		WriteError(w, r, notFoundAs(err, "screen"))
		return
	}
	// Revocation deletes the pairing history and closes the live session with
	// 4002 so the TV clears its cache immediately (design §6.5, PRD §8.2).
	if err := a.deps.DB.Pairings().DeleteForScreen(ctx, id); err != nil {
		WriteError(w, r, err)
		return
	}
	if a.deps.Sessions != nil {
		a.deps.Sessions.Revoke(id, "screen_revoked")
	}
	a.audit(ctx, r, "screen.revoke", id, "")
	sc, err := a.deps.DB.Screens().Get(ctx, id)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "screen"))
		return
	}
	WriteJSON(w, http.StatusOK, a.toScreenDTO(sc, now))
}

func isNotFound(err error) bool {
	return errors.Is(err, domain.ErrNotFound)
}
