package httpapi

import (
	"net/http"
	"time"

	"github.com/DituLin/Atrium/internal/version"
)

type healthBody struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// handleHealthLive reports that the process is running.
func (a *API) handleHealthLive(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, healthBody{Status: "ok", Version: version.String()})
}

// handleHealthReady reports whether the database answers. It exposes no home
// data, so it is safe without a credential.
func (a *API) handleHealthReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 2*time.Second)
	defer cancel()
	if err := a.deps.DB.SQL().PingContext(ctx); err != nil {
		WriteJSON(w, http.StatusServiceUnavailable, healthBody{Status: "unavailable"})
		return
	}
	var one int
	if err := a.deps.DB.SQL().QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		WriteJSON(w, http.StatusServiceUnavailable, healthBody{Status: "unavailable"})
		return
	}
	WriteJSON(w, http.StatusOK, healthBody{Status: "ok", Version: version.String()})
}
