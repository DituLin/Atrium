package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
)

// Authenticator resolves credentials on a request into an Identity.
type Authenticator struct {
	db      *store.DB
	origins *OriginPolicy
	fails   *FailureLimiter
	now     func() time.Time
}

// AuthFailuresPerMinute and AuthBlockDuration implement design §6.8.
const (
	AuthFailuresPerMinute = 10
	AuthBlockDuration     = time.Minute
)

// NewAuthenticator builds the request authenticator.
func NewAuthenticator(db *store.DB, origins *OriginPolicy, now func() time.Time) *Authenticator {
	if now == nil {
		now = time.Now
	}
	return &Authenticator{
		db:      db,
		origins: origins,
		fails:   NewFailureLimiter(AuthFailuresPerMinute, AuthBlockDuration, now),
		now:     now,
	}
}

// Origins exposes the origin policy.
func (a *Authenticator) Origins() *OriginPolicy { return a.origins }

// Blocked reports whether the caller is inside an auth-failure cooldown.
func (a *Authenticator) Blocked(ip string) (bool, time.Duration) {
	if !a.fails.Blocked(ip) {
		return false, 0
	}
	return true, a.fails.RetryAfter(ip)
}

// Authenticate resolves the credentials on r. It returns a *domain.Error with
// a stable code when the request may not proceed.
func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request, required Scope) (*Identity, error) {
	ip := ClientIP(r)
	if blocked, retry := a.Blocked(ip); blocked {
		return nil, rateLimited(retry)
	}

	if bearer := BearerToken(r); bearer != "" {
		id, err := a.byBearer(ctx, bearer)
		if err != nil {
			return nil, a.fail(ip, err)
		}
		return a.authorize(id, required, ip)
	}

	if cookie := CookieToken(r); cookie != "" {
		// Cookies travel automatically, so they are only honoured on GET routes
		// from an allowed origin, and never for the admin scope (design §6.8).
		if required == ScopeAdmin {
			return nil, a.fail(ip, domain.Errorf(domain.CodeUnauthorized, "admin routes require a bearer token"))
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			return nil, a.fail(ip, domain.Errorf(domain.CodeUnauthorized, "cookie credentials are only accepted on GET"))
		}
		if !a.origins.CheckRequest(r) {
			return nil, a.fail(ip, domain.Errorf(domain.CodeForbidden, "origin is not allowed"))
		}
		id, err := a.byScreenToken(ctx, cookie)
		if err != nil {
			return nil, a.fail(ip, err)
		}
		id.ViaCookie = true
		return a.authorize(id, required, ip)
	}

	return nil, a.fail(ip, domain.Errorf(domain.CodeUnauthorized, "no credential presented"))
}

// AuthenticateWebSocket resolves credentials for a handshake. A browser always
// sends an Origin and it must be allowed; a non-browser client (the CLI, a
// probe, an MCP bridge) sends none and must therefore present a bearer token,
// which no cross-site page can attach.
func (a *Authenticator) AuthenticateWebSocket(ctx context.Context, r *http.Request) (*Identity, error) {
	origin := r.Header.Get("Origin")
	if origin != "" && !a.origins.Check(origin) {
		return nil, a.fail(ClientIP(r), domain.Errorf(domain.CodeForbidden, "origin is not allowed"))
	}
	if origin == "" && BearerToken(r) == "" {
		return nil, a.fail(ClientIP(r), domain.Errorf(domain.CodeUnauthorized,
			"a websocket handshake without an Origin requires a bearer token"))
	}
	return a.Authenticate(ctx, r, ScopeScreen)
}

func (a *Authenticator) authorize(id *Identity, required Scope, ip string) (*Identity, error) {
	switch required {
	case ScopeNone:
		return id, nil
	case ScopeScreen:
		// The admin scope includes every screen route.
		if id.Scope == ScopeScreen || id.Scope == ScopeAdmin {
			return id, nil
		}
	case ScopeAdmin:
		if id.Scope == ScopeAdmin {
			return id, nil
		}
	}
	return nil, a.fail(ip, domain.Errorf(domain.CodeForbidden, "credential does not grant the %s scope", required))
}

func (a *Authenticator) byBearer(ctx context.Context, token string) (*Identity, error) {
	switch ScopeOf(token) {
	case ScopeScreen:
		return a.byScreenToken(ctx, token)
	case ScopeAdmin:
		return a.byAdminToken(ctx, token)
	default:
		return nil, domain.Errorf(domain.CodeUnauthorized, "malformed credential")
	}
}

func (a *Authenticator) byScreenToken(ctx context.Context, token string) (*Identity, error) {
	if !ValidToken(token, ScreenPrefix) {
		return nil, domain.Errorf(domain.CodeUnauthorized, "malformed screen credential")
	}
	sc, err := a.db.Screens().GetByTokenHash(ctx, HashToken(token))
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.Errorf(domain.CodeUnauthorized, "unknown screen credential")
	}
	if err != nil {
		return nil, err
	}
	if sc.Status != domain.ScreenActive {
		return nil, domain.Errorf(domain.CodeScreenRevoked, "screen %q is revoked", sc.ID)
	}
	return &Identity{Scope: ScopeScreen, Screen: sc}, nil
}

func (a *Authenticator) byAdminToken(ctx context.Context, token string) (*Identity, error) {
	rec, err := a.db.AdminTokens().GetByHash(ctx, HashToken(token))
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.Errorf(domain.CodeUnauthorized, "unknown admin credential")
	}
	if err != nil {
		return nil, err
	}
	if rec.RevokedAt != nil {
		return nil, domain.Errorf(domain.CodeUnauthorized, "admin credential is revoked")
	}
	_ = a.db.AdminTokens().TouchUsed(ctx, rec.ID, a.now())
	return &Identity{Scope: ScopeAdmin, AdminTokenID: rec.ID}, nil
}

// fail counts an authentication failure and converts it into a 429 once the
// caller crosses the allowance.
func (a *Authenticator) fail(ip string, err error) error {
	if blocked := a.fails.RecordFailure(ip); blocked {
		return rateLimited(a.fails.RetryAfter(ip))
	}
	return err
}

func rateLimited(retry time.Duration) error {
	secs := int(retry.Seconds())
	if secs < 1 {
		secs = 1
	}
	return domain.Errorf(domain.CodeRateLimited, "too many authentication failures").
		WithDetails(map[string]any{"retry_after_seconds": secs})
}
