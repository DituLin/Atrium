package auth

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/DituLin/Atrium/internal/domain"
)

// ScreenCookieName is the credential cookie set at pairing claim.
const ScreenCookieName = "atrium_screen"

// Identity is the authenticated caller of a request.
type Identity struct {
	Scope Scope
	// Screen is set for the screen scope.
	Screen *domain.Screen
	// AdminTokenID is set for the admin scope.
	AdminTokenID string
	// ViaCookie reports whether the credential came from a cookie.
	ViaCookie bool
}

// IsAdmin reports whether the identity carries the admin scope.
func (i *Identity) IsAdmin() bool { return i != nil && i.Scope == ScopeAdmin }

// IsScreen reports whether the identity carries the screen scope.
func (i *Identity) IsScreen() bool { return i != nil && i.Scope == ScopeScreen }

type contextKey struct{}

// WithIdentity stores the identity on a request context.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the identity attached to a request, or nil.
func FromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(contextKey{}).(*Identity)
	return id
}

// BearerToken extracts the token from an Authorization header.
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// CookieToken extracts the screen credential from the request cookie.
func CookieToken(r *http.Request) string {
	c, err := r.Cookie(ScreenCookieName)
	if err != nil || c == nil {
		return ""
	}
	return c.Value
}

// ClientIP returns the remote address without its port. Proxy headers are
// ignored on purpose: Atrium is reached directly on the LAN, so a forwarded
// header would be attacker-controlled input to the rate limiter.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// OriginPolicy decides whether a request's Origin header is acceptable.
type OriginPolicy struct {
	allowed map[string]struct{}
}

// NewOriginPolicy builds a policy from the configured origins.
func NewOriginPolicy(origins []string) *OriginPolicy {
	set := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		if n := normalizeOrigin(o); n != "" {
			set[n] = struct{}{}
		}
	}
	return &OriginPolicy{allowed: set}
}

// Allowed lists the accepted origins; diagnostics and tests use it.
func (p *OriginPolicy) Allowed() []string {
	out := make([]string, 0, len(p.allowed))
	for o := range p.allowed {
		out = append(out, o)
	}
	return out
}

// Check reports whether the Origin header is acceptable for a credential that
// travels automatically (cookie or WebSocket handshake). A missing Origin is
// rejected here; Bearer requests skip this check entirely (design §6.8).
func (p *OriginPolicy) Check(origin string) bool {
	n := normalizeOrigin(origin)
	if n == "" {
		return false
	}
	_, ok := p.allowed[n]
	return ok
}

// CheckRequest decides whether a cookie-authenticated request comes from an
// allowed origin. Browsers omit Origin on same-origin GET requests (fetch,
// <img>, navigations), so the check falls back to the Sec-Fetch-Site metadata
// header and, for engines that predate it, to the Referer origin. A request
// that carries none of the three is refused: a credential that travels
// automatically must prove where it came from.
func (p *OriginPolicy) CheckRequest(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		return p.Check(origin)
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "same-origin", "none":
		return true
	case "same-site", "cross-site":
		return false
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		return p.Check(ref)
	}
	return false
}

func normalizeOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" || origin == "null" {
		return ""
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}
