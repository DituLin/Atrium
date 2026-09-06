package httpapi

import (
	"net/http"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// sourceDTO is the admin view of a data source. It never carries root_path:
// even an administrator reads paths only through the explicit photo admin
// route (design §15).
type sourceDTO struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Health          string  `json:"health"`
	HealthDetail    string  `json:"health_detail,omitempty"`
	IdentityBound   bool    `json:"identity_bound"`
	IdentityFSType  string  `json:"identity_fstype,omitempty"`
	IdentityMatches bool    `json:"identity_matches"`
	LastCheckAt     *string `json:"last_check_at,omitempty"`
	LastSuccessAt   *string `json:"last_success_at,omitempty"`
	ScanGeneration  int64   `json:"scan_generation"`
	LastScanAt      *string `json:"last_scan_at,omitempty"`
	BaselineDoneAt  *string `json:"baseline_completed_at,omitempty"`
	ShareTotalBytes *int64  `json:"share_total_bytes,omitempty"`
	ShareFreeBytes  *int64  `json:"share_free_bytes,omitempty"`
	StuckOps        int64   `json:"stuck_ops"`
	SkippedSymlinks int64   `json:"skipped_symlinks"`
	RevokeReason    string  `json:"revoke_reason,omitempty"`
	Photos          counts  `json:"photos"`
}

type counts struct {
	Ready       int64 `json:"ready"`
	Pending     int64 `json:"pending"`
	Unsupported int64 `json:"unsupported"`
	Removed     int64 `json:"removed"`
	Excluded    int64 `json:"excluded"`
}

type sourceListResponse struct {
	Sources []sourceDTO `json:"sources"`
}

func (a *API) toSourceDTO(r *http.Request, s *domain.Source) sourceDTO {
	loc := a.deps.Home.Location()
	dto := sourceDTO{
		ID: s.ID, Name: s.Name, Status: string(s.Status), Health: string(s.Health),
		HealthDetail: s.HealthDetail, IdentityBound: s.IdentityBound != nil,
		IdentityMatches: true,
		LastCheckAt:     formatPtr(s.LastCheckAt, loc),
		LastSuccessAt:   formatPtr(s.LastSuccessAt, loc),
		ScanGeneration:  s.ScanGeneration,
		LastScanAt:      formatPtr(s.LastScanCompletedA, loc),
		BaselineDoneAt:  formatPtr(s.BaselineCompleted, loc),
		ShareTotalBytes: s.ShareTotalBytes, ShareFreeBytes: s.ShareFreeBytes,
		RevokeReason: s.RevokeReason,
	}
	if s.IdentityBound != nil {
		dto.IdentityFSType = s.IdentityBound.FSType
	}
	if a.deps.Sources != nil {
		if entry := a.deps.Sources.Get(s.ID); entry != nil {
			stats := entry.Stats()
			dto.StuckOps = stats.StuckOps
			dto.SkippedSymlinks = stats.SkippedSymlinks
			dto.IdentityMatches = !entry.IdentityMismatch()
		}
	}
	ctx := r.Context()
	repo := a.deps.DB.Photos()
	for status, target := range map[domain.PhotoStatus]*int64{
		domain.PhotoReady:       &dto.Photos.Ready,
		domain.PhotoPending:     &dto.Photos.Pending,
		domain.PhotoUnsupported: &dto.Photos.Unsupported,
		domain.PhotoRemoved:     &dto.Photos.Removed,
		domain.PhotoExcluded:    &dto.Photos.Excluded,
	} {
		if n, err := repo.CountBySourceStatus(ctx, s.ID, status); err == nil {
			*target = n
		}
	}
	return dto
}

func formatPtr(t *time.Time, loc *time.Location) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	v := t.In(loc).Format(time.RFC3339)
	return &v
}

func (a *API) handleSourcesList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	rows, err := a.deps.DB.Sources().List(ctx)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	out := sourceListResponse{Sources: make([]sourceDTO, 0, len(rows))}
	for i := range rows {
		out.Sources = append(out.Sources, a.toSourceDTO(r, &rows[i]))
	}
	WriteJSON(w, http.StatusOK, out)
}

type scanRequestBody struct {
	Mode string `json:"mode"`
}

type scanResponse struct {
	Run    *scanRunDTO `json:"scan_run"`
	Merged bool        `json:"merged"`
}

func (a *API) handleSourceScan(w http.ResponseWriter, r *http.Request) {
	var body scanRequestBody
	if r.ContentLength > 0 {
		if err := DecodeJSON(w, r, &body); err != nil {
			WriteError(w, r, err)
			return
		}
	}
	mode := domain.ScanManual
	switch body.Mode {
	case "", "incremental", string(domain.ScanManual):
	case "full":
		mode = domain.ScanFull
	default:
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "mode must be incremental or full"))
		return
	}
	if a.deps.Index == nil {
		WriteError(w, r, domain.Errorf(domain.CodeConflict, "the indexer is not running"))
		return
	}
	ctx, cancel := timeoutContext(r, 30*time.Second)
	defer cancel()

	id := r.PathValue("id")
	res, err := a.deps.Index.Request(ctx, id, mode)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "source"))
		return
	}
	a.audit(ctx, r, "source.scan", id, string(mode))
	out := scanResponse{Merged: res.Merged}
	if res.Run != nil {
		dto := toScanRunDTO(res.Run, a.deps.Home.Location())
		out.Run = &dto
	}
	WriteJSON(w, http.StatusAccepted, out)
}

func (a *API) handleSourceRevoke(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	id := r.PathValue("id")
	if err := a.deps.DB.Sources().Revoke(ctx, id, "admin_revoked", a.now()); err != nil {
		WriteError(w, r, notFoundAs(err, "source"))
		return
	}
	a.audit(ctx, r, "source.revoke", id, "")
	a.publish(domain.TopicPhotos, domain.TopicNAS, domain.TopicHome)
	a.writeSource(w, r, id)
}

func (a *API) handleSourceRestore(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	id := r.PathValue("id")
	if a.deps.Sources != nil && a.deps.Sources.Get(id) == nil {
		// Restoring a source that is no longer in the configuration would
		// re-expose photos from a root the operator has stopped authorizing.
		WriteError(w, r, domain.Errorf(domain.CodeConflict,
			"source %s is not in the configuration", id))
		return
	}
	if err := a.deps.DB.Sources().Restore(ctx, id, a.now()); err != nil {
		WriteError(w, r, notFoundAs(err, "source"))
		return
	}
	a.audit(ctx, r, "source.restore", id, "")
	a.publish(domain.TopicPhotos, domain.TopicNAS, domain.TopicHome)
	a.writeSource(w, r, id)
}

func (a *API) handleSourceRebind(w http.ResponseWriter, r *http.Request) {
	if a.deps.Sources == nil {
		WriteError(w, r, domain.Errorf(domain.CodeConflict, "the source manager is not running"))
		return
	}
	ctx, cancel := timeoutContext(r, 30*time.Second)
	defer cancel()
	id := r.PathValue("id")
	probe, err := a.deps.Sources.Rebind(ctx, id)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "source"))
		return
	}
	a.audit(ctx, r, "source.rebind_identity", id, string(probe.Health))
	a.publish(domain.TopicNAS, domain.TopicHome)
	a.writeSource(w, r, id)
}

func (a *API) writeSource(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	row, err := a.deps.DB.Sources().Get(ctx, id)
	if err != nil {
		WriteError(w, r, notFoundAs(err, "source"))
		return
	}
	WriteJSON(w, http.StatusOK, a.toSourceDTO(r, row))
}

// publish notifies subscribers of an administrative change.
func (a *API) publish(topics ...domain.Topic) {
	if a.deps.Bus != nil {
		a.deps.Bus.Publish(topics...)
	}
}
