package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

type exclusionDTO struct {
	ID        string `json:"id"`
	SourceID  string `json:"source_id"`
	MatchKind string `json:"match_kind"`
	Pattern   string `json:"pattern"`
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"created_at"`
}

type exclusionListResponse struct {
	Exclusions []exclusionDTO `json:"exclusions"`
}

type exclusionCreateRequest struct {
	SourceID string `json:"source_id"`
	Prefix   string `json:"prefix"`
	Path     string `json:"path"`
	Reason   string `json:"reason"`
}

func (a *API) handleExclusionsList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	rows, err := a.deps.DB.Exclusions().List(ctx, r.URL.Query().Get("source_id"))
	if err != nil {
		WriteError(w, r, err)
		return
	}
	loc := a.deps.Home.Location()
	out := exclusionListResponse{Exclusions: make([]exclusionDTO, 0, len(rows))}
	for i := range rows {
		out.Exclusions = append(out.Exclusions, exclusionDTO{
			ID: rows[i].ID, SourceID: rows[i].SourceID, MatchKind: string(rows[i].MatchKind),
			Pattern: rows[i].Pattern, Reason: rows[i].Reason,
			CreatedAt: rows[i].CreatedAt.In(loc).Format(time.RFC3339),
		})
	}
	WriteJSON(w, http.StatusOK, out)
}

func (a *API) handleExclusionCreate(w http.ResponseWriter, r *http.Request) {
	var body exclusionCreateRequest
	if err := DecodeJSON(w, r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	if body.SourceID == "" {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "source_id is required"))
		return
	}
	kind := domain.MatchPrefix
	pattern := strings.Trim(body.Prefix, "/")
	if pattern == "" {
		kind = domain.MatchPath
		pattern = strings.Trim(body.Path, "/")
	}
	if pattern == "" {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "prefix or path is required"))
		return
	}
	if strings.Contains(pattern, "..") {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "pattern must not contain .."))
		return
	}

	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	if _, err := a.deps.DB.Sources().Get(ctx, body.SourceID); err != nil {
		WriteError(w, r, notFoundAs(err, "source"))
		return
	}
	now := a.now()
	rule := &domain.Exclusion{
		SourceID: body.SourceID, MatchKind: kind, Pattern: pattern,
		Reason: body.Reason, CreatedAt: now,
	}
	if err := a.deps.DB.Exclusions().Add(ctx, rule); err != nil {
		WriteError(w, r, err)
		return
	}
	// Hide anything already indexed under the rule right away; waiting for the
	// next scan would keep revoked photos servable for up to a minute.
	if _, err := a.deps.DB.Photos().ExcludeMatching(ctx, body.SourceID, kind, pattern, body.Reason, now); err != nil {
		WriteError(w, r, err)
		return
	}
	a.audit(ctx, r, "exclusion.add", rule.ID, string(kind))
	a.publish(domain.TopicPhotos, domain.TopicHome)
	WriteJSON(w, http.StatusCreated, exclusionDTO{
		ID: rule.ID, SourceID: rule.SourceID, MatchKind: string(rule.MatchKind),
		Pattern: rule.Pattern, Reason: rule.Reason,
		CreatedAt: now.In(a.deps.Home.Location()).Format(time.RFC3339),
	})
}

func (a *API) handleExclusionDelete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	id := r.PathValue("id")
	if err := a.deps.DB.Exclusions().Delete(ctx, id); err != nil {
		WriteError(w, r, notFoundAs(err, "exclusion"))
		return
	}
	a.audit(ctx, r, "exclusion.remove", id, "")
	a.publish(domain.TopicPhotos, domain.TopicHome)
	w.WriteHeader(http.StatusNoContent)
}
