package httpapi_test

import (
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestArchiveSearchRoutesStrictAndReadOnly(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.read")
	for _, path := range []string{"/search", "/entities"} {
		req, e := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/integrations/archives"+path, nil)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+h.token)
		resp, e := h.server.Client().Do(req)
		require.NoError(t, e)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}
	for _, q := range []string{"limit=26", "query=x&query=y", "household_id=other", "public=true"} {
		req, e := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/integrations/archives/search?"+q, nil)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+h.token)
		resp, e := h.server.Client().Do(req)
		require.NoError(t, e)
		resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, q)
	}
}
