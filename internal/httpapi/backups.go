package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/DituLin/Atritum/internal/auth"
	"github.com/DituLin/Atritum/internal/backup"
	"github.com/DituLin/Atritum/internal/domain"
)

// backupDTO is the wire shape of one snapshot. The directory is configuration,
// not a discovery: only the file name is returned (design §15).
type backupDTO struct {
	Name       string `json:"name"`
	SizeBytes  int64  `json:"size_bytes"`
	CreatedAt  string `json:"created_at"`
	ConfigCopy bool   `json:"config_copy"`
}

type backupListResponse struct {
	Enabled bool        `json:"enabled"`
	Keep    int         `json:"keep"`
	Backups []backupDTO `json:"backups"`
}

func toBackupDTO(s backup.Snapshot) backupDTO {
	return backupDTO{
		Name: s.Name, SizeBytes: s.SizeBytes,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339), ConfigCopy: s.ConfigCopy,
	}
}

func (a *API) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	if a.deps.Backup == nil || !a.deps.Backup.Enabled() {
		WriteError(w, r, domain.Errorf(domain.CodeConflict,
			"backup.dir is not configured; set it in the configuration first"))
		return
	}
	ctx, cancel := timeoutContext(r, 2*time.Minute)
	defer cancel()
	snap, err := a.deps.Backup.Run(ctx)
	if err != nil {
		WriteError(w, r, domain.WrapErr(domain.CodeInternal, err, "backup failed"))
		return
	}
	a.audit(ctx, r, "backup.create", snap.Name, "")
	WriteJSON(w, http.StatusCreated, toBackupDTO(snap))
}

func (a *API) handleBackupList(w http.ResponseWriter, r *http.Request) {
	out := backupListResponse{Backups: []backupDTO{}}
	if a.deps.Backup != nil {
		out.Enabled = a.deps.Backup.Enabled()
	}
	out.Keep = a.deps.Config.Backup.Keep
	if a.deps.Backup == nil {
		WriteJSON(w, http.StatusOK, out)
		return
	}
	snaps, err := a.deps.Backup.List()
	if err != nil {
		WriteError(w, r, domain.WrapErr(domain.CodeInternal, err, "cannot list backups"))
		return
	}
	for _, s := range snaps {
		out.Backups = append(out.Backups, toBackupDTO(s))
	}
	WriteJSON(w, http.StatusOK, out)
}

// tokenRotateResponse carries the new credential exactly once.
type tokenRotateResponse struct {
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
	Note      string `json:"note"`
}

// handleTokenRotate issues a new admin credential and revokes the caller's
// only after the response has been written, so a lost response never locks the
// operator out of a running server (design §6.8).
func (a *API) handleTokenRotate(w http.ResponseWriter, r *http.Request) {
	if a.deps.Admin == nil {
		WriteError(w, r, domain.Errorf(domain.CodeInternal, "admin service is not available"))
		return
	}
	ctx, cancel := timeoutContext(r, 5*time.Second)
	defer cancel()
	now := a.now()
	token, _, err := a.deps.Admin.Issue(ctx, "rotated", now)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	a.audit(ctx, r, "admin.token.rotate", "", "")
	WriteJSON(w, http.StatusOK, tokenRotateResponse{
		Token:     token,
		CreatedAt: now.UTC().Format(time.RFC3339),
		Note:      "store this now; it is shown once and the previous token is already revoked",
	})
	// The replacement is in the client's hands before the old one dies.
	_ = http.NewResponseController(w).Flush()

	id := auth.FromContext(r.Context())
	if id == nil || id.AdminTokenID == "" {
		return
	}
	if err := a.deps.Admin.RevokeToken(context.WithoutCancel(ctx), id.AdminTokenID, now); err != nil {
		if log := LoggerFrom(r.Context()); log != nil {
			log.Warn("previous admin token not revoked", "component", "auth",
				"event", "token_revoke_failed")
		}
	}
}
