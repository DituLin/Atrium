package httpapi

import "net/http"

func (a *API) handleHouse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, a.deps.House.Compose(r.Context()))
}

func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, a.deps.Overview.Compose(r.Context()))
}
