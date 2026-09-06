package webui_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/webui"
)

func builtFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                &fstest.MapFile{Data: []byte("<!doctype html><div id=root></div>")},
		"assets/index-A1b2C3d4.js":  &fstest.MapFile{Data: []byte("console.log(1)")},
		"assets/index-A1b2C3d4.css": &fstest.MapFile{Data: []byte("body{}")},
		"favicon.ico":               &fstest.MapFile{Data: []byte("icon")},
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHashedAssetsAreImmutableAndIndexIsNoCache(t *testing.T) {
	h := webui.New(webui.Options{FS: builtFS()})
	require.True(t, h.HasIndex())

	asset := get(t, h, "/assets/index-A1b2C3d4.js")
	require.Equal(t, http.StatusOK, asset.Code)
	require.Equal(t, "public, max-age=31536000, immutable", asset.Header().Get("Cache-Control"))

	index := get(t, h, "/")
	require.Equal(t, http.StatusOK, index.Code)
	require.Equal(t, "no-cache, must-revalidate", index.Header().Get("Cache-Control"))
	require.Contains(t, index.Header().Get("Content-Type"), "text/html")

	// An unhashed asset must be revalidated.
	icon := get(t, h, "/favicon.ico")
	require.Equal(t, http.StatusOK, icon.Code)
	require.Equal(t, "no-cache, must-revalidate", icon.Header().Get("Cache-Control"))
}

func TestSPAFallbackForClientRoutes(t *testing.T) {
	h := webui.New(webui.Options{FS: builtFS()})

	for _, route := range []string{"/dashboard", "/photos/recent", "/photo/01JABC", "/pair"} {
		rec := get(t, h, route)
		require.Equal(t, http.StatusOK, rec.Code, route)
		require.Contains(t, rec.Body.String(), "id=root", route)
	}
}

func TestMissingAssetIs404AndAPIPathsAreNeverRewritten(t *testing.T) {
	h := webui.New(webui.Options{FS: builtFS()})

	missing := get(t, h, "/assets/gone-ZZZZZZZZ.js")
	require.Equal(t, http.StatusNotFound, missing.Code, "a missing script must not return HTML")

	for _, p := range []string{"/api/v1/home", "/health/live"} {
		rec := get(t, h, p)
		require.Equal(t, http.StatusNotFound, rec.Code, p)
	}
}

func TestFriendlyPageWhenUIIsNotBuilt(t *testing.T) {
	h := webui.New(webui.Options{FS: fstest.MapFS{".gitkeep": &fstest.MapFile{}}})
	require.False(t, h.HasIndex())

	rec := get(t, h, "/dashboard")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "web UI is not built")
	require.Contains(t, rec.Body.String(), "make web")
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
}

func TestNonGetMethodsAreRejected(t *testing.T) {
	h := webui.New(webui.Options{FS: builtFS()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTraversalIsContained(t *testing.T) {
	h := webui.New(webui.Options{FS: builtFS()})
	rec := get(t, h, "/../../etc/passwd")
	// path.Clean collapses the traversal, so the request lands on the SPA.
	require.NotEqual(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "root:")
}
