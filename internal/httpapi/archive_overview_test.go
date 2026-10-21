package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestArchiveOverviewHTTPRejectsQueriesAndNonScreenCredentials(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.read")
	token, err := auth.NewScreenToken()
	require.NoError(t, err)
	require.NoError(t, h.db.Screens().Create(context.Background(), &domain.Screen{ID: "overview-tv", Name: "Synthetic TV", TokenHash: auth.HashToken(token)}))
	for _, tc := range []struct {
		query, token string
		status       int
	}{
		{"", token, http.StatusOK},
		{"?entity_id=private", token, http.StatusBadRequest},
		{"?limit=1", token, http.StatusBadRequest},
		{"?query=a&query=b", token, http.StatusBadRequest},
		{"?%zz", token, http.StatusBadRequest},
		{"", h.token, http.StatusForbidden},
		{"", "", http.StatusUnauthorized},
	} {
		req, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/archives/overview"+tc.query, nil)
		require.NoError(t, err)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		resp, err := h.server.Client().Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, tc.status, resp.StatusCode, tc.query)
		if tc.status == http.StatusOK {
			require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
		}
	}
	require.NoError(t, h.db.Screens().Revoke(context.Background(), "overview-tv", time.Now()))
	req, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/archives/overview", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := h.server.Client().Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusGone, resp.StatusCode)
}
