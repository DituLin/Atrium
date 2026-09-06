package httpapi_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// unguardedPaths are documented as needing no credential, so they answer
// something other than 401 and are exempt from the check below.
var unguardedPaths = map[string]bool{
	"/health/live":            true,
	"/health/ready":           true,
	"/api/v1/pair/start":      true,
	"/api/v1/pair/{id}":       true,
	"/api/v1/pair/{id}/claim": true,
}

// TestEveryDocumentedRouteIsRegistered keeps docs/api/openapi.yaml honest: a
// documented route that nothing serves would fall through to the SPA handler
// and answer with HTML instead of a JSON error.
func TestEveryDocumentedRouteIsRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "openapi.yaml"))
	require.NoError(t, err)

	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Paths)

	h := newHarness(t)
	checked := 0
	for path, ops := range doc.Paths {
		if unguardedPaths[path] {
			continue
		}
		concrete := strings.NewReplacer("{id}", "01JPLACEHOLDER").Replace(path)
		for method := range ops {
			if method == "parameters" {
				continue
			}
			resp := h.request(strings.ToUpper(method), concrete, "")
			// Without a credential every guarded route answers 401, or 429
			// once the auth-failure limiter trips. A 404 would mean the
			// request fell through to the SPA catch-all instead.
			assert.Contains(t, []int{http.StatusUnauthorized, http.StatusTooManyRequests}, resp.StatusCode,
				"%s %s is documented but not registered", method, path)
			assert.Contains(t, []string{"unauthorized", "rate_limited"}, errorCode(t, resp),
				"%s %s", method, path)
			checked++
		}
	}
	assert.Greater(t, checked, 20, "the documented surface should not shrink silently")
}
