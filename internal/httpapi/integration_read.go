package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/integration"
	"github.com/DituLin/Atrium/internal/store"
)

func integrationReply[T any](a *API, w http.ResponseWriter, data T) {
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, integration.Response[T]{SchemaVersion: integration.SchemaVersion, ObservedAt: a.now(), Availability: "available", Data: data})
}

func (a *API) integrationRoutes() {
	for _, route := range []struct {
		path, permission string
		handler          http.HandlerFunc
	}{
		{"GET /home", "home.read", a.integrationHome},
		{"GET /nas/status", "nas.read", a.integrationNAS},
		{"GET /photos", "photos.read", a.integrationPhotos},
		{"GET /photos/{id}", "photos.read", a.integrationPhoto},
		{"GET /screens", "screens.read", a.integrationScreens},
		{"GET /screens/{id}", "screens.read", a.integrationScreen},
		{"POST /screens/{id}/commands", "screens.control", a.integrationCommandIssue},
		{"GET /commands/{id}", "commands.read", a.integrationCommandGet},
		{"GET /operations/{id}", "commands.read", a.integrationOperationGet},
	} {
		method, path, _ := strings.Cut(route.path, " ")
		a.mux.HandleFunc(method+" "+APIPrefix+"/integrations"+path, a.requireIntegration(route.permission, route.handler))
	}
}

func (a *API) requireIntegration(permission string, next http.HandlerFunc) http.HandlerFunc {
	return a.requireScope(auth.ScopeIntegration, func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context()).Integration
		if p != nil {
			var done func()
			w, r, done = beginIntegrationTrace(w, r, p.ID, permission)
			defer done()
		}
		if p == nil || !p.Policy.Allows(permission) {
			WriteError(w, r, domain.Errorf(domain.CodeForbidden, "integration permission denied"))
			return
		}
		if !a.integrationLimits.Allow(p.ID) {
			WriteError(w, r, domain.Errorf(domain.CodeRateLimited, "integration request limit reached"))
			return
		}
		ctx, cancel := timeoutContext(r, 5*time.Second)
		defer cancel()
		next(w, r.WithContext(ctx))
	})
}

func (a *API) integrationHome(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context()).Integration
	data := integration.Home{Core: "reachable", Timezone: a.deps.Home.Location().String(), Day: a.deps.Home.Today(), Capabilities: append([]string{}, p.Policy.Permissions...)}
	if p.Policy.Allows("photos.read") {
		start, _ := a.deps.Home.DayBounds(a.now())
		counts, err := a.deps.DB.Photos().CountScoped(r.Context(), p.Policy.SourceIDs, a.deps.Home.Today(), start)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		converted := integration.PhotoCounts(counts)
		data.Photos = &converted
	}
	if p.Policy.Allows("nas.read") {
		sources, err := a.integrationSourceData(r.Context(), p)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		data.NAS = &sources
	}
	if p.Policy.Allows("screens.read") {
		screens, err := a.integrationScreenData(r.Context(), p)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		data.Screens = &screens
	}
	integrationReply(a, w, data)
}

func (a *API) integrationSourceData(ctx context.Context, p *domain.IntegrationPrincipal) ([]integration.Source, error) {
	out := []integration.Source{}
	for _, id := range p.Policy.SourceIDs {
		s, err := a.deps.DB.Sources().Get(ctx, id)
		if isNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if s.Status != domain.SourceActive {
			continue
		}
		start, _ := a.deps.Home.DayBounds(a.now())
		counts, err := a.deps.DB.Photos().CountScoped(ctx, []string{id}, a.deps.Home.Today(), start)
		if err != nil {
			return nil, err
		}
		health := string(s.Health)
		if health == "" {
			health = "unknown"
		}
		out = append(out, integration.Source{ID: s.ID, Health: health, LastCheckAt: s.LastCheckAt, LastSuccessAt: s.LastSuccessAt, LastScanAt: s.LastScanCompletedA, ReadyPhotos: counts.Ready})
	}
	return out, nil
}

func (a *API) integrationNAS(w http.ResponseWriter, r *http.Request) {
	data, err := a.integrationSourceData(r.Context(), auth.FromContext(r.Context()).Integration)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	integrationReply(a, w, data)
}

func (a *API) safeIntegrationScreen(ctx context.Context, p *domain.IntegrationPrincipal, s *domain.Screen) integration.Screen {
	original := a.toScreenDTO(s, a.now())
	dto := integration.Screen{ID: s.ID, Name: s.Name, Registered: original.Registered, Online: original.Online, LastSeenAt: s.LastSeenAt, AppliedSequence: s.AppliedSequence}
	if s.CurrentRoute != nil {
		dto.CurrentRoute = a.safeIntegrationRoute(ctx, p, s.CurrentRoute)
	}
	return dto
}

func (a *API) integrationScreenData(ctx context.Context, p *domain.IntegrationPrincipal) ([]integration.Screen, error) {
	out := []integration.Screen{}
	for _, id := range p.Policy.ScreenIDs {
		s, err := a.deps.DB.Screens().Get(ctx, id)
		if isNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, a.safeIntegrationScreen(ctx, p, s))
	}
	return out, nil
}

func (a *API) integrationScreens(w http.ResponseWriter, r *http.Request) {
	data, err := a.integrationScreenData(r.Context(), auth.FromContext(r.Context()).Integration)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	integrationReply(a, w, data)
}

func (a *API) integrationScreen(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context()).Integration
	id := r.PathValue("id")
	if !p.Policy.AllowsScreen(id) {
		WriteError(w, r, domain.Errorf(domain.CodeNotFound, "screen unavailable"))
		return
	}
	s, err := a.deps.DB.Screens().Get(r.Context(), id)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "screen"))
		return
	}
	integrationReply(a, w, a.safeIntegrationScreen(r.Context(), p, s))
}

func toIntegrationPhoto(p domain.Photo) integration.Photo {
	return integration.Photo{ID: p.ID, SourceID: p.SourceID, CapturedAt: p.CapturedAt, CapturedConfidence: string(p.CapturedConfidence), FirstSeenAt: p.FirstSeenAt, IsBaseline: p.IsBaseline, PreviewStatus: string(p.PreviewStatus)}
}

func (a *API) integrationPhoto(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context()).Integration
	photo, err := a.deps.DB.Photos().GetScoped(r.Context(), p.Policy.SourceIDs, r.PathValue("id"))
	if err != nil {
		WriteError(w, r, notFoundAs(err, "photo"))
		return
	}
	integrationReply(a, w, toIntegrationPhoto(*photo))
}

type integrationCursor struct {
	Principal  string            `json:"p"`
	Version    int64             `json:"v"`
	Collection domain.Collection `json:"c"`
	Day        string            `json:"d"`
	ID         string            `json:"id,omitempty"`
	Time       time.Time         `json:"t"`
	HasTime    bool              `json:"h,omitempty"`
	Seed       uint64            `json:"s,omitempty"`
	Offset     int               `json:"o,omitempty"`
	Pool       string            `json:"pool,omitempty"`
}

func (a *API) signIntegrationCursor(c integrationCursor) string {
	b, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, a.integrationCursorKey)
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *API) readIntegrationCursor(raw string, expected integrationCursor) (integrationCursor, error) {
	invalid := domain.Errorf(domain.CodeInvalidRequest, "invalid or stale cursor; restart collection")
	if raw == "" {
		return expected, nil
	}
	if len(raw) > 4096 {
		return expected, invalid
	}
	left, right, ok := strings.Cut(raw, ".")
	if !ok {
		return expected, invalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(left)
	if err != nil {
		return expected, invalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(right)
	if err != nil {
		return expected, invalid
	}
	mac := hmac.New(sha256.New, a.integrationCursorKey)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return expected, invalid
	}
	var got integrationCursor
	if json.Unmarshal(payload, &got) != nil || got.Principal != expected.Principal || got.Version != expected.Version || got.Collection != expected.Collection || got.Day != expected.Day || got.Offset < 0 {
		return expected, invalid
	}
	return got, nil
}

func (a *API) integrationPhotos(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context()).Integration
	q := r.URL.Query()
	collection := domain.Collection(q.Get("collection"))
	if collection == "" {
		collection = domain.CollectionRecent
	}
	if !collection.Valid() {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "invalid collection"))
		return
	}
	limit := 20
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "limit must be between 1 and 50"))
			return
		}
		limit = n
	}
	cur, err := a.readIntegrationCursor(q.Get("cursor"), integrationCursor{Principal: p.ID, Version: p.PolicyVersion, Collection: collection, Day: a.deps.Home.Today()})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var rows []domain.Photo
	hasNext := false
	if collection == domain.CollectionRandom {
		rows, cur, hasNext, err = a.integrationRandom(r.Context(), p, cur, limit)
	} else {
		rows, err = a.deps.DB.Photos().ListScoped(r.Context(), store.ScopedPhotoQuery{SourceIDs: p.Policy.SourceIDs, Collection: collection, Day: cur.Day, Limit: limit, AfterID: cur.ID, AfterTime: cur.Time, HasTime: cur.HasTime})
		if len(rows) == limit {
			last := rows[len(rows)-1]
			cur.ID = last.ID
			if collection == domain.CollectionRecent {
				cur.Time = last.FirstSeenAt
			} else if last.CapturedAt != nil {
				cur.Time = *last.CapturedAt
				cur.HasTime = true
			} else {
				cur.Time = time.Time{}
				cur.HasTime = false
			}
			hasNext = true
		}
	}
	if err != nil {
		WriteError(w, r, err)
		return
	}
	start, _ := a.deps.Home.DayBounds(a.now())
	counts, err := a.deps.DB.Photos().CountScoped(r.Context(), p.Policy.SourceIDs, cur.Day, start)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	out := integration.PhotoList{Items: []integration.Photo{}, Collection: string(collection), UnknownCaptured: counts.UnknownCaptured, BaselineOnly: collection == domain.CollectionRecent && counts.Ready == counts.Baseline && counts.Baseline > 0}
	if collection == domain.CollectionCapturedToday {
		out.Day = cur.Day
	}
	for _, photo := range rows {
		out.Items = append(out.Items, toIntegrationPhoto(photo))
	}
	if hasNext {
		cursor := a.signIntegrationCursor(cur)
		out.NextCursor = &cursor
	}
	integrationReply(a, w, out)
}

func (a *API) integrationRandom(ctx context.Context, p *domain.IntegrationPrincipal, cur integrationCursor, limit int) ([]domain.Photo, integrationCursor, bool, error) {
	ids, err := a.deps.DB.Photos().ScopedEligibleIDs(ctx, p.Policy.SourceIDs, RandomPoolLimit)
	if err != nil {
		return nil, cur, false, err
	}
	h := sha256.New()
	for _, id := range ids {
		h.Write([]byte(id))
		h.Write([]byte{0})
	}
	fingerprint := hex.EncodeToString(h.Sum(nil))
	if cur.Pool != "" && cur.Pool != fingerprint {
		return nil, cur, false, domain.Errorf(domain.CodeInvalidRequest, "collection changed; restart random round")
	}
	cur.Pool = fingerprint
	if cur.Seed == 0 {
		cur.Seed = newSeed()
	}
	shuffleIDs(ids, cur.Seed)
	if cur.Offset >= len(ids) {
		return nil, cur, false, nil
	}
	end := min(cur.Offset+limit, len(ids))
	out := []domain.Photo{}
	for _, id := range ids[cur.Offset:end] {
		photo, err := a.deps.DB.Photos().GetScoped(ctx, p.Policy.SourceIDs, id)
		if isNotFound(err) {
			continue
		}
		if err != nil {
			return nil, cur, false, err
		}
		out = append(out, *photo)
	}
	cur.Offset = end
	return out, cur, end < len(ids), nil
}
