package httpapi

import (
	"net/http"

	"github.com/DituLin/Atritum/internal/auth"
)

// APIPrefix is the versioned API root.
const APIPrefix = "/api/v1"

// routes registers every handler on the mux. Go 1.22 patterns carry the method
// and the path parameters, so no third-party router is needed.
func (a *API) routes() {
	m := a.mux

	// Unauthenticated.
	m.HandleFunc("GET /health/live", a.handleHealthLive)
	m.HandleFunc("GET /health/ready", a.handleHealthReady)
	m.HandleFunc("POST "+APIPrefix+"/pair/start", a.handlePairStart)
	m.HandleFunc("GET "+APIPrefix+"/pair/{id}", a.handlePairStatus)
	m.HandleFunc("POST "+APIPrefix+"/pair/{id}/claim", a.handlePairClaim)

	// Screen scope (admin is accepted too).
	m.HandleFunc("GET "+APIPrefix+"/home", a.requireScope(auth.ScopeScreen, a.handleHome))
	m.HandleFunc("GET "+APIPrefix+"/screens/me", a.requireScope(auth.ScopeScreen, a.handleScreenMe))
	m.HandleFunc("GET "+APIPrefix+"/nas/status", a.requireScope(auth.ScopeScreen, a.handleNASStatus))
	m.HandleFunc("GET "+APIPrefix+"/photos", a.requireScope(auth.ScopeScreen, a.handlePhotosList))
	m.HandleFunc("GET "+APIPrefix+"/photos/{id}", a.requireScope(auth.ScopeScreen, a.handlePhotoGet))
	m.HandleFunc("GET "+APIPrefix+"/media/photos/{id}", a.requireScope(auth.ScopeScreen, a.handleMediaGet))
	if a.deps.WS != nil {
		// The hub authenticates the handshake itself: an upgrade must answer
		// with a close code, not with a JSON body a browser cannot read.
		m.Handle("GET "+APIPrefix+"/screens/connect", a.deps.WS)
	}

	// Admin scope.
	m.HandleFunc("GET "+APIPrefix+"/screens", a.requireScope(auth.ScopeAdmin, a.handleScreensList))
	m.HandleFunc("GET "+APIPrefix+"/screens/{id}", a.requireScope(auth.ScopeAdmin, a.handleScreenGet))
	m.HandleFunc("DELETE "+APIPrefix+"/screens/{id}", a.requireScope(auth.ScopeAdmin, a.handleScreenRevoke))
	m.HandleFunc("GET "+APIPrefix+"/pairings", a.requireScope(auth.ScopeAdmin, a.handlePairingsList))
	m.HandleFunc("POST "+APIPrefix+"/pairings/{id}/approve", a.requireScope(auth.ScopeAdmin, a.handlePairingApprove))
	m.HandleFunc("DELETE "+APIPrefix+"/pairings/{id}", a.requireScope(auth.ScopeAdmin, a.handlePairingReject))
	m.HandleFunc("POST "+APIPrefix+"/screens/{id}/commands", a.requireScope(auth.ScopeAdmin, a.handleCommandIssue))
	m.HandleFunc("GET "+APIPrefix+"/commands", a.requireScope(auth.ScopeAdmin, a.handleCommandsList))
	m.HandleFunc("GET "+APIPrefix+"/commands/{id}", a.requireScope(auth.ScopeAdmin, a.handleCommandGet))
	m.HandleFunc("GET "+APIPrefix+"/diagnostics", a.requireScope(auth.ScopeAdmin, a.handleDiagnostics))
	m.HandleFunc("POST "+APIPrefix+"/backups", a.requireScope(auth.ScopeAdmin, a.handleBackupCreate))
	m.HandleFunc("GET "+APIPrefix+"/backups", a.requireScope(auth.ScopeAdmin, a.handleBackupList))
	m.HandleFunc("POST "+APIPrefix+"/admin/token/rotate", a.requireScope(auth.ScopeAdmin, a.handleTokenRotate))
	m.HandleFunc("GET "+APIPrefix+"/sources", a.requireScope(auth.ScopeAdmin, a.handleSourcesList))
	m.HandleFunc("POST "+APIPrefix+"/sources/{id}/scan", a.requireScope(auth.ScopeAdmin, a.handleSourceScan))
	m.HandleFunc("POST "+APIPrefix+"/sources/{id}/revoke", a.requireScope(auth.ScopeAdmin, a.handleSourceRevoke))
	m.HandleFunc("POST "+APIPrefix+"/sources/{id}/restore", a.requireScope(auth.ScopeAdmin, a.handleSourceRestore))
	m.HandleFunc("POST "+APIPrefix+"/sources/{id}/rebind-identity", a.requireScope(auth.ScopeAdmin, a.handleSourceRebind))
	m.HandleFunc("GET "+APIPrefix+"/scans", a.requireScope(auth.ScopeAdmin, a.handleScansList))
	m.HandleFunc("GET "+APIPrefix+"/scans/{id}", a.requireScope(auth.ScopeAdmin, a.handleScanGet))
	m.HandleFunc("GET "+APIPrefix+"/photos/{id}/admin", a.requireScope(auth.ScopeAdmin, a.handlePhotoAdminGet))
	m.HandleFunc("POST "+APIPrefix+"/photos/{id}/exclude", a.requireScope(auth.ScopeAdmin, a.handlePhotoExclude))
	m.HandleFunc("POST "+APIPrefix+"/photos/{id}/include", a.requireScope(auth.ScopeAdmin, a.handlePhotoInclude))
	m.HandleFunc("POST "+APIPrefix+"/photos/{id}/retry", a.requireScope(auth.ScopeAdmin, a.handlePhotoRetry))
	m.HandleFunc("GET "+APIPrefix+"/exclusions", a.requireScope(auth.ScopeAdmin, a.handleExclusionsList))
	m.HandleFunc("POST "+APIPrefix+"/exclusions", a.requireScope(auth.ScopeAdmin, a.handleExclusionCreate))
	m.HandleFunc("DELETE "+APIPrefix+"/exclusions/{id}", a.requireScope(auth.ScopeAdmin, a.handleExclusionDelete))

	// Static client and SPA fallback; must be registered last as the catch-all.
	if a.deps.UI != nil {
		m.Handle("/", a.deps.UI)
	} else {
		m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}
}
