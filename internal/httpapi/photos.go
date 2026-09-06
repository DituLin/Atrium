package httpapi

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
	"github.com/DituLin/Atritum/internal/store"
)

// RandomPoolLimit caps how many IDs one random round shuffles (design §6.4).
const RandomPoolLimit = 100_000

// photoDTO is the item shape of design §8. It carries no path: a screen only
// ever learns an opaque ID and the two media URLs built from it.
type photoDTO struct {
	ID                 string     `json:"id"`
	SourceID           string     `json:"source_id"`
	CapturedAt         *string    `json:"captured_at"`
	CapturedConfidence string     `json:"captured_confidence"`
	FirstSeenAt        string     `json:"first_seen_at"`
	IsBaseline         bool       `json:"is_baseline"`
	Width              int        `json:"width,omitempty"`
	Height             int        `json:"height,omitempty"`
	Preview            previewDTO `json:"preview"`
	URLs               urlsDTO    `json:"urls"`
}

type previewDTO struct {
	Status string `json:"status"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type urlsDTO struct {
	Preview string `json:"preview"`
	Thumb   string `json:"thumb"`
}

type photoListResponse struct {
	Items      []photoDTO    `json:"items"`
	NextCursor *string       `json:"next_cursor"`
	Meta       photoListMeta `json:"meta"`
}

type photoListMeta struct {
	Collection string `json:"collection"`
	// UnknownCapturedCount is how many eligible photos have no capture time.
	// The client shows it next to `captured_today` so an empty day does not
	// look like data loss (PRD §5.2).
	UnknownCapturedCount int64 `json:"unknown_captured_count"`
	// BaselineOnly marks the "nothing new since the first import" empty state.
	BaselineOnly bool `json:"baseline_only,omitempty"`
	// Day is the home-timezone day `captured_today` resolved to.
	Day string `json:"day,omitempty"`
}

type photoItemResponse struct {
	Item      photoDTO      `json:"item"`
	Neighbors *neighborsDTO `json:"neighbors,omitempty"`
}

type neighborsDTO struct {
	Collection string  `json:"collection"`
	PreviousID *string `json:"previous_id"`
	NextID     *string `json:"next_id"`
}

func mediaURL(id string, variant domain.Variant) string {
	return APIPrefix + "/media/photos/" + id + "?variant=" + string(variant)
}

func (a *API) toPhotoDTO(p *domain.Photo, loc *time.Location, previews map[string]domain.PreviewFile) photoDTO {
	dto := photoDTO{
		ID: p.ID, SourceID: p.SourceID,
		CapturedConfidence: string(p.CapturedConfidence),
		FirstSeenAt:        p.FirstSeenAt.In(loc).Format(time.RFC3339),
		IsBaseline:         p.IsBaseline,
		Width:              p.Width, Height: p.Height,
		Preview: previewDTO{Status: string(p.PreviewStatus)},
		URLs: urlsDTO{
			Preview: mediaURL(p.ID, domain.VariantPreview),
			Thumb:   mediaURL(p.ID, domain.VariantThumb),
		},
	}
	if p.CapturedAt != nil {
		v := p.CapturedAt.In(capturedLocation(p, loc)).Format(time.RFC3339)
		dto.CapturedAt = &v
	}
	if f, ok := previews[p.ID]; ok {
		dto.Preview.Width, dto.Preview.Height = f.Width, f.Height
	}
	return dto
}

// capturedLocation renders a capture time in its original offset when one was
// recorded, so "10:12" stays the time on the camera rather than being
// re-expressed in the home timezone.
func capturedLocation(p *domain.Photo, home *time.Location) *time.Location {
	if p.CapturedOffsetSeconds == nil {
		return home
	}
	return time.FixedZone("", *p.CapturedOffsetSeconds)
}

// loadPreviewSizes fetches the cached dimensions for a page of photos.
func (a *API) loadPreviewSizes(ctx context.Context, rows []domain.Photo) map[string]domain.PreviewFile {
	out := make(map[string]domain.PreviewFile, len(rows))
	for i := range rows {
		f, err := a.deps.DB.Previews().Get(ctx, rows[i].ID, domain.VariantPreview)
		if err == nil {
			out[rows[i].ID] = *f
		}
	}
	return out
}

func (a *API) handlePhotosList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()

	collection := domain.Collection(r.URL.Query().Get("collection"))
	if collection == "" {
		collection = domain.CollectionRecent
	}
	if !collection.Valid() {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "unknown collection"))
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		WriteError(w, r, err)
		return
	}

	rows, next, err := a.queryCollection(ctx, collection, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	loc := a.deps.Home.Location()
	previews := a.loadPreviewSizes(ctx, rows)
	out := photoListResponse{
		Items: make([]photoDTO, 0, len(rows)),
		Meta:  photoListMeta{Collection: string(collection)},
	}
	for i := range rows {
		out.Items = append(out.Items, a.toPhotoDTO(&rows[i], loc, previews))
	}
	if next != "" {
		out.NextCursor = &next
	}
	if unknown, err := a.deps.DB.Photos().CountUnknownCaptured(ctx); err == nil {
		out.Meta.UnknownCapturedCount = unknown
	}
	if collection == domain.CollectionCapturedToday {
		out.Meta.Day = a.deps.Home.Today()
	}
	if collection == domain.CollectionRecent && len(rows) == 0 {
		if baseline, err := a.deps.DB.Photos().CountBaseline(ctx); err == nil && baseline > 0 {
			out.Meta.BaselineOnly = true
		}
	}
	WriteJSON(w, http.StatusOK, out)
}

// queryCollection dispatches to the right keyset or seeded query.
func (a *API) queryCollection(ctx context.Context, collection domain.Collection, cursor string, limit int) ([]domain.Photo, string, error) {
	repo := a.deps.DB.Photos()
	cur, err := decodeCursor(cursor)
	if err != nil && collection != domain.CollectionRandom {
		return nil, "", err
	}

	switch collection {
	case domain.CollectionRecent:
		var key *store.RecentCursor
		if cur.Kind == "recent" {
			t, err := cursorTime(cur)
			if err != nil {
				return nil, "", err
			}
			key = &store.RecentCursor{FirstSeenAt: t, ID: cur.ID}
		}
		rows, err := repo.ListRecent(ctx, key, limit)
		if err != nil {
			return nil, "", err
		}
		return rows, nextKeyset(rows, limit, "recent"), nil

	case domain.CollectionCapturedToday:
		var key *store.CapturedCursor
		if cur.Kind == "captured" {
			t, err := cursorTime(cur)
			if err != nil {
				return nil, "", err
			}
			key = &store.CapturedCursor{CapturedAt: t, ID: cur.ID}
		}
		rows, err := repo.ListCapturedDay(ctx, a.deps.Home.Today(), key, limit)
		if err != nil {
			return nil, "", err
		}
		return rows, nextKeyset(rows, limit, "captured"), nil

	case domain.CollectionAll:
		var key *store.AllCursor
		if cur.Kind == "all" {
			var t time.Time
			if cur.HasTime {
				if t, err = cursorTime(cur); err != nil {
					return nil, "", err
				}
			}
			key = &store.AllCursor{HasCaptured: cur.HasTime, CapturedAt: t, ID: cur.ID}
		}
		rows, err := repo.ListAll(ctx, key, limit)
		if err != nil {
			return nil, "", err
		}
		return rows, nextKeyset(rows, limit, "all"), nil

	default:
		return a.queryRandom(ctx, cursor, limit)
	}
}

// nextKeyset builds the cursor for the following page, or "" at the end.
func nextKeyset(rows []domain.Photo, limit int, kind string) string {
	if len(rows) < limit || len(rows) == 0 {
		return ""
	}
	last := rows[len(rows)-1]
	switch kind {
	case "recent":
		return encodeCursor(cursorPayload{Kind: kind, Time: store.FormatTime(last.FirstSeenAt), ID: last.ID})
	case "captured":
		if last.CapturedAt == nil {
			return ""
		}
		return encodeCursor(cursorPayload{Kind: kind, Time: store.FormatTime(*last.CapturedAt), ID: last.ID})
	default:
		p := cursorPayload{Kind: kind, ID: last.ID}
		if last.CapturedAt != nil {
			p.HasTime = true
			p.Time = store.FormatTime(*last.CapturedAt)
		}
		return encodeCursor(p)
	}
}

// queryRandom draws one page from a seeded shuffle of the eligible set. Within
// a round no photo repeats; at the end next_cursor is null so the client
// starts a fresh round with a new seed (PRD §5.2).
func (a *API) queryRandom(ctx context.Context, cursor string, limit int) ([]domain.Photo, string, error) {
	cur, err := decodeRandomCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	if cur.Seed == 0 {
		cur.Seed = newSeed()
	}
	ids, err := a.deps.DB.Photos().ListEligibleIDs(ctx, RandomPoolLimit)
	if err != nil {
		return nil, "", err
	}
	if len(ids) == 0 {
		return nil, "", nil
	}
	shuffleIDs(ids, cur.Seed)

	if cur.Offset >= len(ids) {
		return nil, "", nil
	}
	end := min(cur.Offset+limit, len(ids))
	page := ids[cur.Offset:end]
	rows, err := a.deps.DB.Photos().ListByIDs(ctx, page)
	if err != nil {
		return nil, "", err
	}
	if end >= len(ids) {
		return rows, "", nil
	}
	return rows, encodeRandomCursor(randomCursor{Seed: cur.Seed, Offset: end}), nil
}

// shuffleIDs applies a deterministic permutation for a seed.
func shuffleIDs(ids []string, seed uint64) {
	// A deterministic PRNG is the point: the same seed must reproduce the
	// same round so paging never repeats or skips a photo.
	r := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)) //nolint:gosec // reproducibility, not secrecy
	r.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
}

// newSeed draws a round seed from the system CSPRNG so two screens starting at
// the same moment do not display the same sequence.
func newSeed() uint64 {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano())
	}
	seed := binary.BigEndian.Uint64(b[:])
	if seed == 0 {
		seed = 1 // zero means "start a new round"
	}
	return seed
}

func (a *API) handlePhotoGet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()

	photo, err := a.deps.DB.Photos().GetEligible(ctx, r.PathValue("id"))
	if err != nil {
		WriteError(w, r, notFoundAs(err, "photo"))
		return
	}
	loc := a.deps.Home.Location()
	out := photoItemResponse{Item: a.toPhotoDTO(photo, loc, a.loadPreviewSizes(ctx, []domain.Photo{*photo}))}

	if raw := r.URL.Query().Get("neighbors"); raw != "" {
		collection := domain.Collection(raw)
		if !collection.Valid() {
			WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "unknown collection"))
			return
		}
		prev, next, err := a.deps.DB.Photos().Neighbours(ctx, collection, a.deps.Home.Today(), photo)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		out.Neighbors = &neighborsDTO{Collection: string(collection)}
		if prev != "" {
			out.Neighbors.PreviousID = &prev
		}
		if next != "" {
			out.Neighbors.NextID = &next
		}
	}
	WriteJSON(w, http.StatusOK, out)
}
