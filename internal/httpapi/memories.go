package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
)

// A memory position is bound to the authenticated screen and home calendar
// day. An inclusive item position resumes the image currently on screen;
// next_cursor is exclusive, so normal pagination never repeats that image.
type memoryCursor struct {
	Version   int    `json:"v"`
	Day       string `json:"day"`
	Screen    string `json:"screen"`
	Anchor    string `json:"anchor"`
	Inclusive bool   `json:"inclusive"`
}

type memoryDTO struct {
	photoDTO
	ResumeCursor string `json:"resume_cursor"`
}

type memoriesResponse struct {
	Items      []memoryDTO `json:"items"`
	NextCursor *string     `json:"next_cursor"`
	Meta       struct {
		Collection string `json:"collection"`
		Day        string `json:"day"`
		ScreenID   string `json:"screen_id"`
		Total      int    `json:"total"`
	} `json:"meta"`
}

type rankedMemory struct {
	id   string
	rank [sha256.Size]byte
}

func memoryRank(day, screen, id string) [sha256.Size]byte {
	return sha256.Sum256([]byte("atrium:memories:v1\x00" + day + "\x00" + screen + "\x00" + id))
}

func compareMemory(a, b rankedMemory) int {
	if compared := bytes.Compare(a.rank[:], b.rank[:]); compared != 0 {
		return compared
	}
	if a.id < b.id {
		return -1
	}
	if a.id > b.id {
		return 1
	}
	return 0
}

func encodeMemoryCursor(cursor memoryCursor) string {
	blob, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(blob)
}

func invalidMemoryCursor() error {
	return domain.Errorf(domain.CodeInvalidRequest, "memory cursor is not valid")
}

// Decode a small, exact object rather than accepting JSON's duplicate-key or
// null-to-zero coercions. The cursor is a position, never an authorization:
// every request obtains the current eligible pool from the database.
func decodeMemoryCursor(raw, screen string) (memoryCursor, error) {
	var cursor memoryCursor
	if raw == "" {
		return cursor, nil
	}
	if len(raw) > 1024 {
		return cursor, invalidMemoryCursor()
	}
	blob, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		return cursor, invalidMemoryCursor()
	}
	decoder := json.NewDecoder(bytes.NewReader(blob))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return cursor, invalidMemoryCursor()
	}
	seen := make(map[string]bool, 5)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return cursor, invalidMemoryCursor()
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return cursor, invalidMemoryCursor()
		}
		seen[key] = true
		switch key {
		case "v":
			err = decoder.Decode(&cursor.Version)
		case "day":
			err = decoder.Decode(&cursor.Day)
		case "screen":
			err = decoder.Decode(&cursor.Screen)
		case "anchor":
			err = decoder.Decode(&cursor.Anchor)
		case "inclusive":
			var inclusive *bool
			err = decoder.Decode(&inclusive)
			if inclusive == nil {
				return cursor, invalidMemoryCursor()
			}
			cursor.Inclusive = *inclusive
		default:
			return cursor, invalidMemoryCursor()
		}
		if err != nil {
			return cursor, invalidMemoryCursor()
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(seen) != 5 {
		return cursor, invalidMemoryCursor()
	}
	if _, err := decoder.Token(); err != io.EOF {
		return cursor, invalidMemoryCursor()
	}
	parsedDay, err := time.Parse("2006-01-02", cursor.Day)
	if err != nil || parsedDay.Format("2006-01-02") != cursor.Day || cursor.Version != 1 || !domain.ValidSlug(cursor.Screen) || cursor.Screen != screen || !validMemoryAnchor(cursor.Anchor) {
		return cursor, invalidMemoryCursor()
	}
	return cursor, nil
}

func validMemoryAnchor(anchor string) bool {
	if len(anchor) == 0 || len(anchor) > 128 {
		return false
	}
	for _, character := range anchor {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

func memoryQuery(raw string) (url.Values, int, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return nil, 0, domain.Errorf(domain.CodeInvalidRequest, "memory query is not valid")
	}
	for key, values := range query {
		if key != "cursor" && key != "limit" || len(values) != 1 {
			return nil, 0, domain.Errorf(domain.CodeInvalidRequest, "memory query is not valid")
		}
	}
	limit := DefaultPageLimit
	if rawLimit, present := query["limit"]; present {
		limit, err = strconv.Atoi(rawLimit[0])
		if err != nil || limit < 1 || limit > MaxPageLimit {
			return nil, 0, domain.Errorf(domain.CodeInvalidRequest, "limit must be between 1 and 100")
		}
	}
	return query, limit, nil
}

// Advance by the scanned window rather than the rows left after the second
// visibility check. Even a now-empty page can precede still-authorized photos.
func nextMemoryCursor(pool []rankedMemory, end int, position memoryCursor) *string {
	if end <= 0 || end >= len(pool) {
		return nil
	}
	position.Anchor = pool[end-1].id
	position.Inclusive = false
	next := encodeMemoryCursor(position)
	return &next
}

func (a *API) handleMemories(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity := auth.FromContext(r.Context())
	if identity == nil || identity.Screen == nil {
		WriteError(w, r, domain.Errorf(domain.CodeForbidden, "route requires a screen credential"))
		return
	}
	query, limit, err := memoryQuery(r.URL.RawQuery)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	screenID := identity.Screen.ID
	cursor, err := decodeMemoryCursor(query.Get("cursor"), screenID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	day := a.deps.Home.Today()
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	ids, err := a.deps.DB.Photos().ListEligibleIDs(ctx, RandomPoolLimit)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	pool := make([]rankedMemory, len(ids))
	for i, id := range ids {
		pool[i] = rankedMemory{id: id, rank: memoryRank(day, screenID, id)}
	}
	sort.Slice(pool, func(i, j int) bool { return compareMemory(pool[i], pool[j]) < 0 })
	start := 0
	if cursor.Version != 0 && cursor.Day == day {
		anchor := rankedMemory{id: cursor.Anchor, rank: memoryRank(day, screenID, cursor.Anchor)}
		start = sort.Search(len(pool), func(i int) bool {
			compared := compareMemory(pool[i], anchor)
			return compared > 0 || compared == 0 && cursor.Inclusive
		})
	}
	end := min(start+limit, len(pool))
	pageIDs := make([]string, 0, end-start)
	for _, item := range pool[start:end] {
		pageIDs = append(pageIDs, item.id)
	}
	// ListByIDs repeats the current source/ready visibility filter. A source
	// revoked after the pool read therefore does not leak into this page.
	rows, err := a.deps.DB.Photos().ListByIDs(ctx, pageIDs)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	previews := a.loadPreviewSizes(ctx, rows)
	out := memoriesResponse{Items: make([]memoryDTO, 0, len(rows))}
	out.Meta.Collection, out.Meta.Day, out.Meta.ScreenID, out.Meta.Total = "memories", day, screenID, len(pool)
	position := memoryCursor{Version: 1, Day: day, Screen: screenID, Inclusive: true}
	for i := range rows {
		if rows[i].PreviewStatus != domain.PreviewReady {
			continue
		}
		position.Anchor = rows[i].ID
		out.Items = append(out.Items, memoryDTO{
			photoDTO:     a.toPhotoDTO(&rows[i], a.deps.Home.Location(), previews),
			ResumeCursor: encodeMemoryCursor(position),
		})
	}
	out.NextCursor = nextMemoryCursor(pool, end, position)
	WriteJSON(w, http.StatusOK, out)
}
