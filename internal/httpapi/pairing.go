package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/domain"
)

// Pairing rate limits from design §6.8.
const (
	PairStartPerMinutePerIP = 5
	PairStartPerMinuteTotal = 20
	PairPollPerMinute       = 60
)

// ScreenCookieMaxAge is one year, as required by design §6.8.
const ScreenCookieMaxAge = 31536000

type pairStartRequest struct {
	ClientHint string `json:"client_hint"`
}

type pairStartResponse struct {
	PairingID      string `json:"pairing_id"`
	Code           string `json:"code"`
	ExpiresAt      string `json:"expires_at"`
	PollIntervalMS int    `json:"poll_interval_ms"`
}

type pairStatusResponse struct {
	PairingID string `json:"pairing_id"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at"`
}

type pairClaimResponse struct {
	ScreenID string `json:"screen_id"`
	Name     string `json:"name"`
	Token    string `json:"token"`
}

func (a *API) handlePairStart(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r)
	if !a.limits.pairStartIP.Allow(ip) || !a.limits.pairStartGlobal.Allow("global") {
		WriteError(w, r, domain.Errorf(domain.CodeRateLimited, "too many pairing attempts").
			WithDetails(map[string]any{"retry_after_seconds": 60}))
		return
	}
	var req pairStartRequest
	if r.ContentLength > 0 {
		if err := DecodeJSON(w, r, &req); err != nil {
			WriteError(w, r, err)
			return
		}
	}
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	pr, err := a.deps.Pairing.Start(ctx, truncate(req.ClientHint, 200), ip)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, pairStartResponse{
		PairingID:      pr.ID,
		Code:           pr.Code,
		ExpiresAt:      pr.ExpiresAt.UTC().Format(time.RFC3339),
		PollIntervalMS: auth.PairingPollIntervalMS,
	})
}

func (a *API) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !a.limits.pairPoll.Allow(id) {
		WriteError(w, r, domain.Errorf(domain.CodeRateLimited, "polling too fast").
			WithDetails(map[string]any{"retry_after_seconds": 5}))
		return
	}
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	pr, err := a.deps.Pairing.Get(ctx, id)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "pairing"))
		return
	}
	WriteJSON(w, http.StatusOK, pairStatusResponse{
		PairingID: pr.ID,
		Status:    string(pr.Status),
		ExpiresAt: pr.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (a *API) handlePairClaim(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	res, err := a.deps.Pairing.Claim(ctx, r.PathValue("id"))
	if err != nil {
		WriteError(w, r, notFoundAs(err, "pairing"))
		return
	}
	http.SetCookie(w, a.screenCookie(res.Token))
	WriteJSON(w, http.StatusOK, pairClaimResponse{
		ScreenID: res.Screen.ID, Name: res.Screen.Name, Token: res.Token,
	})
}

// screenCookie builds the credential cookie required by design §6.8. Secure is
// dropped only when TLS is off, because a browser rejects a Secure cookie on a
// plain HTTP origin.
func (a *API) screenCookie(token string) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure is computed from the TLS mode just above
		Name:     auth.ScreenCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   ScreenCookieMaxAge,
		HttpOnly: true,
		Secure:   !a.deps.Insecure,
		SameSite: http.SameSiteStrictMode,
	}
}

// timeoutContext derives a request context with a deadline.
func timeoutContext(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// notFoundAs converts a repository miss into a stable API error.
func notFoundAs(err error, what string) error {
	if _, ok := domain.AsError(err); ok {
		return err
	}
	if isNotFound(err) {
		return domain.Errorf(domain.CodeNotFound, "%s not found", what)
	}
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
