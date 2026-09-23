package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/stretchr/testify/require"
)

func TestSecurityHeadersAllowLegacySameOriginReferrer(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := httpapi.WithSecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://core/", nil))
	// Older WebViews omit Origin and Sec-Fetch-Site on GET. Cookie auth needs
	// the same-origin Referer fallback, while cross-origin referrals stay hidden.
	require.Equal(t, "same-origin", recorder.Header().Get("Referrer-Policy"))
	require.Equal(t, "DENY", recorder.Header().Get("X-Frame-Options"))
	require.Contains(t, recorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
}
