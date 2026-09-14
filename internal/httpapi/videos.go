package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/oklog/ulid/v2"
)

type videoDTO struct {
	ID          string                `json:"id"`
	SourceID    string                `json:"source_id"`
	Revision    int64                 `json:"revision"`
	Status      domain.VideoStatus    `json:"status"`
	FirstSeenAt string                `json:"first_seen_at"`
	Metadata    *domain.VideoMetadata `json:"metadata,omitempty"`
}

func toVideoDTO(v domain.Video) videoDTO {
	item := videoDTO{ID: v.ID, SourceID: v.SourceID, Revision: v.Revision, Status: v.Status, FirstSeenAt: v.FirstSeenAt.UTC().Format(time.RFC3339Nano)}
	if v.Status == domain.VideoReady {
		item.Metadata = &v.Metadata
	}
	return item
}

// videoScopes requires the runtime and persisted authority to agree. Offline
// sources may retain their index; a known identity mismatch cannot expose it.
func (a *API) videoScopes(ctx context.Context) ([]store.VideoScope, error) {
	scopes := []store.VideoScope{}
	if a.deps.Sources == nil {
		return scopes, nil
	}
	for _, e := range a.deps.Sources.All() {
		bound := e.Bound()
		if e.IdentityMismatch() || bound == nil {
			continue
		}
		src, err := a.deps.DB.Sources().Get(ctx, e.Config.ID)
		if err != nil {
			return nil, err
		}
		if src.Status != domain.SourceActive || src.RootPath != e.Config.Root || src.IdentityBound == nil || !src.IdentityBound.Equal(*bound) {
			continue
		}
		ext := e.Extensions()
		scopes = append(scopes, store.VideoScope{SourceID: src.ID, Root: src.RootPath, MP4: ext["mp4"], MOV: ext["mov"]})
	}
	return scopes, nil
}

func (a *API) videoRoutes() {
	a.mux.HandleFunc("GET "+APIPrefix+"/videos", a.requireScope(auth.ScopeScreen, a.handleVideosList))
	a.mux.HandleFunc("GET "+APIPrefix+"/videos/{id}", a.requireScope(auth.ScopeScreen, a.handleVideoGet))
}

func (a *API) handleVideosList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var before *store.VideoPosition
	raw := r.URL.Query().Get("cursor")
	if raw != "" {
		if len(raw) > 1024 {
			WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid"))
			return
		}
		cursor, err := decodeCursor(raw)
		if err != nil || cursor.Kind != "videos" {
			WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid"))
			return
		}
		t, err := cursorTime(cursor)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		if _, err := ulid.ParseStrict(cursor.ID); err != nil {
			WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid"))
			return
		}
		before = &store.VideoPosition{Time: t, ID: cursor.ID}
	}
	scopes, err := a.videoScopes(ctx)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rows, err := a.deps.DB.Videos().ListVisible(ctx, scopes, before, limit+1)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		value := encodeCursor(cursorPayload{Kind: "videos", Time: store.FormatTime(last.FirstSeenAt), ID: last.ID})
		next = &value
	}
	items := make([]videoDTO, 0, len(rows))
	for _, v := range rows {
		items = append(items, toVideoDTO(v))
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, struct {
		Items      []videoDTO `json:"items"`
		NextCursor *string    `json:"next_cursor"`
	}{items, next})
}

func (a *API) handleVideoGet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	scopes, err := a.videoScopes(ctx)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	v, err := a.deps.DB.Videos().GetVisible(ctx, scopes, r.PathValue("id"))
	if err != nil {
		WriteError(w, r, notFoundAs(err, "video"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, struct {
		Item videoDTO `json:"item"`
	}{toVideoDTO(*v)})
}
