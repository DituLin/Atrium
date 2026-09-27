package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// MediaTicketHeader carries a playback ticket on video stream requests.
const MediaTicketHeader = "X-Atrium-Media-Ticket"

// MediaTicketTTL bounds how long a native player may keep streaming one video.
const MediaTicketTTL = 6 * time.Hour

const mediaTicketPrefix = "amt1"

// MediaTickets issues short-lived, stateless credentials for one screen and
// one video. They exist for native players that cannot reuse the screen
// cookie: Android WebView 66 omits SameSite cookies from CookieManager, so the
// TV's ExoPlayer requests arrived unauthenticated. The page obtains a ticket
// with its cookie and hands it to the player, which sends it as a header —
// never in a URL. The key lives only in memory: a restart voids all tickets.
type MediaTickets struct {
	key []byte
	now func() time.Time
}

// NewMediaTickets creates a ticket signer with a fresh random key.
func NewMediaTickets(now func() time.Time) *MediaTickets {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("auth: media ticket key: %v", err))
	}
	return &MediaTickets{key: key, now: now}
}

// Issue returns a ticket for screenID to stream videoID, and its expiry.
func (m *MediaTickets) Issue(screenID, videoID string) (string, time.Time) {
	expires := m.now().Add(MediaTicketTTL).Truncate(time.Second)
	payload := screenID + "|" + videoID + "|" + strconv.FormatInt(expires.Unix(), 10)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return mediaTicketPrefix + "." + encoded + "." + m.sign(encoded), expires
}

// Verify checks a ticket for videoID and returns the screen it was issued to.
func (m *MediaTickets) Verify(ticket, videoID string) (string, error) {
	parts := strings.Split(ticket, ".")
	if len(parts) != 3 || parts[0] != mediaTicketPrefix {
		return "", domain.Errorf(domain.CodeUnauthorized, "malformed media ticket")
	}
	if !hmac.Equal([]byte(parts[2]), []byte(m.sign(parts[1]))) {
		return "", domain.Errorf(domain.CodeUnauthorized, "invalid media ticket")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", domain.Errorf(domain.CodeUnauthorized, "malformed media ticket")
	}
	fields := strings.Split(string(raw), "|")
	if len(fields) != 3 {
		return "", domain.Errorf(domain.CodeUnauthorized, "malformed media ticket")
	}
	expires, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return "", domain.Errorf(domain.CodeUnauthorized, "malformed media ticket")
	}
	if !m.now().Before(time.Unix(expires, 0)) {
		return "", domain.Errorf(domain.CodeUnauthorized, "media ticket expired")
	}
	if fields[1] != videoID {
		return "", domain.Errorf(domain.CodeForbidden, "media ticket is for another video")
	}
	return fields[0], nil
}

func (m *MediaTickets) sign(encoded string) string {
	mac := hmac.New(sha256.New, m.key)
	mac.Write([]byte(mediaTicketPrefix + "." + encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
