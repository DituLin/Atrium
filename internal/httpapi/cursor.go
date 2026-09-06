package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
)

// Pagination bounds from design §6.4.
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 100
)

// parseLimit clamps a client-supplied page size.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return DefaultPageLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, domain.Errorf(domain.CodeInvalidRequest, "limit must be a positive integer")
	}
	if n > MaxPageLimit {
		n = MaxPageLimit
	}
	return n, nil
}

// cursorPayload is the wire form of a keyset position. Cursors are opaque to
// clients: they are base64 so nobody builds one by hand and starts depending
// on the ordering columns.
type cursorPayload struct {
	Kind    string `json:"k"`
	Time    string `json:"t,omitempty"`
	ID      string `json:"i,omitempty"`
	HasTime bool   `json:"h,omitempty"`
}

func encodeCursor(p cursorPayload) string {
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(raw string) (cursorPayload, error) {
	var p cursorPayload
	if raw == "" {
		return p, nil
	}
	blob, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return p, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid")
	}
	if err := json.Unmarshal(blob, &p); err != nil {
		return p, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid")
	}
	return p, nil
}

func cursorTime(p cursorPayload) (time.Time, error) {
	t, err := store.ParseTime(p.Time)
	if err != nil {
		return time.Time{}, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid")
	}
	return t, nil
}

// randomCursor carries the round seed and how far into it the client is. The
// seed is what makes a round repeatable: the same seed shuffles the same set
// the same way, so paging never repeats or skips a photo (design §6.4).
type randomCursor struct {
	Seed   uint64
	Offset int
}

func encodeRandomCursor(c randomCursor) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(fmt.Sprintf("%d:%d", c.Seed, c.Offset)))
}

func decodeRandomCursor(raw string) (randomCursor, error) {
	if raw == "" {
		return randomCursor{}, nil
	}
	blob, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return randomCursor{}, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid")
	}
	seedRaw, offsetRaw, ok := strings.Cut(string(blob), ":")
	if !ok {
		return randomCursor{}, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid")
	}
	seed, err := strconv.ParseUint(seedRaw, 10, 64)
	if err != nil {
		return randomCursor{}, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid")
	}
	offset, err := strconv.Atoi(offsetRaw)
	if err != nil || offset < 0 {
		return randomCursor{}, domain.Errorf(domain.CodeInvalidRequest, "cursor is not valid")
	}
	return randomCursor{Seed: seed, Offset: offset}, nil
}
