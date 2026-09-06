package httpapi

import (
	"net/http"
	"time"

	"github.com/DituLin/Atritum/internal/domain"
)

// scanRunDTO reports one pass over a source.
type scanRunDTO struct {
	ID               string  `json:"id"`
	SourceID         string  `json:"source_id"`
	Mode             string  `json:"mode"`
	Status           string  `json:"status"`
	StartedAt        string  `json:"started_at"`
	FinishedAt       *string `json:"finished_at,omitempty"`
	DurationMS       int64   `json:"duration_ms,omitempty"`
	FilesSeen        int64   `json:"files_seen"`
	FilesNew         int64   `json:"files_new"`
	FilesChanged     int64   `json:"files_changed"`
	FilesMissing     int64   `json:"files_missing"`
	FilesRemoved     int64   `json:"files_removed"`
	FilesUnsupported int64   `json:"files_unsupported"`
	Errors           int64   `json:"errors"`
	Note             string  `json:"note,omitempty"`
}

type scanListResponse struct {
	Scans []scanRunDTO `json:"scans"`
}

func toScanRunDTO(run *domain.ScanRun, loc *time.Location) scanRunDTO {
	dto := scanRunDTO{
		ID: run.ID, SourceID: run.SourceID, Mode: string(run.Mode), Status: string(run.Status),
		StartedAt: run.StartedAt.In(loc).Format(time.RFC3339),
		FilesSeen: run.FilesSeen, FilesNew: run.FilesNew, FilesChanged: run.FilesChanged,
		FilesMissing: run.FilesMissing, FilesRemoved: run.FilesRemoved,
		FilesUnsupported: run.FilesUnsupported, Errors: run.Errors, Note: run.Note,
	}
	if run.FinishedAt != nil {
		v := run.FinishedAt.In(loc).Format(time.RFC3339)
		dto.FinishedAt = &v
		dto.DurationMS = run.FinishedAt.Sub(run.StartedAt).Milliseconds()
	}
	return dto
}

func (a *API) handleScansList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rows, err := a.deps.DB.ScanRuns().List(ctx, r.URL.Query().Get("source_id"), limit)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	loc := a.deps.Home.Location()
	out := scanListResponse{Scans: make([]scanRunDTO, 0, len(rows))}
	for i := range rows {
		out.Scans = append(out.Scans, toScanRunDTO(&rows[i], loc))
	}
	WriteJSON(w, http.StatusOK, out)
}

func (a *API) handleScanGet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	run, err := a.deps.DB.ScanRuns().Get(ctx, r.PathValue("id"))
	if err != nil {
		WriteError(w, r, notFoundAs(err, "scan run"))
		return
	}
	WriteJSON(w, http.StatusOK, toScanRunDTO(run, a.deps.Home.Location()))
}
