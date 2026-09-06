package httpapi_test

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/backup"
	"github.com/DituLin/Atritum/internal/config"
	"github.com/DituLin/Atritum/internal/httpapi"
)

// Rotation hands over a working credential before the old one dies, so a lost
// response can never lock the operator out (design §6.8).
func TestAdminTokenRotationRevokesThePreviousToken(t *testing.T) {
	h := newHarnessWith(t, func(h *harness, deps *httpapi.Deps) {
		deps.Admin = auth.NewAdminService(h.db)
	})
	old := h.newAdminToken()

	resp := h.request(http.MethodPost, "/api/v1/admin/token/rotate", "", bearer(old))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decode[map[string]any](t, resp)
	fresh := body["token"].(string)
	require.NotEmpty(t, fresh)
	assert.NotEqual(t, old, fresh)
	assert.Contains(t, fresh, "atr_adm_")

	// The replacement works.
	ok := h.request(http.MethodGet, "/api/v1/screens", "", bearer(fresh))
	assert.Equal(t, http.StatusOK, ok.StatusCode)

	// The previous one does not.
	stale := h.request(http.MethodGet, "/api/v1/screens", "", bearer(old))
	assert.Equal(t, http.StatusUnauthorized, stale.StatusCode)
}

func TestBackupRoutesCreateAndList(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backups")
	h := newHarnessWith(t, func(h *harness, deps *httpapi.Deps) {
		deps.Backup = backup.New(backup.Options{
			Dir: dir, Keep: h.cfg.Backup.Keep, DB: h.db.SQL(), Now: deps.Now,
		})
	}, func(cfg *config.Config) {
		cfg.Backup.Enabled = true
		cfg.Backup.Dir = dir
		cfg.Backup.Keep = 7
	})
	admin := h.newAdminToken()

	empty := h.request(http.MethodGet, "/api/v1/backups", "", bearer(admin))
	require.Equal(t, http.StatusOK, empty.StatusCode)
	listed := decode[map[string]any](t, empty)
	assert.Equal(t, true, listed["enabled"])
	assert.Empty(t, listed["backups"])

	created := h.request(http.MethodPost, "/api/v1/backups", "", bearer(admin))
	require.Equal(t, http.StatusCreated, created.StatusCode)
	snap := decode[map[string]any](t, created)
	name := snap["name"].(string)
	assert.Contains(t, name, "atrium-")
	assert.Positive(t, snap["size_bytes"])
	// The directory is configuration, not a discovery: only the name travels.
	assert.NotContains(t, name, dir)

	again := h.request(http.MethodGet, "/api/v1/backups", "", bearer(admin))
	listed = decode[map[string]any](t, again)
	require.Len(t, listed["backups"], 1)
}

func TestBackupRouteRefusesWhenNotConfigured(t *testing.T) {
	h := newHarness(t)
	admin := h.newAdminToken()
	resp := h.request(http.MethodPost, "/api/v1/backups", "", bearer(admin))
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "conflict", errorCode(t, resp))
}

// Every operations route is admin-only and Bearer-only.
func TestOpsRoutesRejectCookiesAndScreenTokens(t *testing.T) {
	h := newHarness(t)
	admin := h.newAdminToken()
	screenToken := h.pairScreen(admin, "living_room_tv", "Living room")

	for _, path := range []string{"/api/v1/diagnostics", "/api/v1/backups"} {
		resp := h.request(http.MethodGet, path, "", bearer(screenToken))
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, path)

		withCookie := h.request(http.MethodGet, path, "", cookie(screenToken), origin(testOrigin))
		assert.Equal(t, http.StatusUnauthorized, withCookie.StatusCode, path)
	}
	rotate := h.request(http.MethodPost, "/api/v1/admin/token/rotate", "", bearer(screenToken))
	assert.Equal(t, http.StatusForbidden, rotate.StatusCode)
}
