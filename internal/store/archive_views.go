package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/oklog/ulid/v2"
	"time"
)

type ArchiveScreenView struct {
	ID        string    `json:"archive_view_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (r *ArchiveTasks) saveArchiveQuery(ctx context.Context, a domain.IntegrationActor, h string, f archive.SearchFilter) (string, error) {
	id := domain.NewID()
	f.Cursor = ""
	f.Limit = 10
	raw, e := json.Marshal(f)
	if e != nil {
		return "", e
	}
	e = r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		if _, e = r.authorizeArchiveClient(ctx, tx, a, h, "archive.read", now); e != nil {
			return e
		}
		if e = pruneArchiveQueriesIn(ctx, tx, now); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_query_receipts(id,household_id,principal_id,credential_id,filter_json,expires_at) VALUES(?,?,?,?,?,?)`, id, h, a.PrincipalID, a.CredentialID, string(raw), FormatTime(now.Add(10*time.Minute)))
		return e
	})
	return id, e
}
func (r *ArchiveTasks) CreateArchiveScreenView(ctx context.Context, a domain.IntegrationActor, h, query, screen, operation string) (ArchiveScreenView, error) {
	var view ArchiveScreenView
	id, err := ulid.ParseStrict(operation)
	if err != nil || id.String() != operation {
		return view, domain.Errorf(domain.CodeInvalidRequest, "invalid operation ID")
	}
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		p, e := r.authorizeArchiveClient(ctx, tx, a, h, "archive.read", now)
		if e != nil {
			return e
		}
		if !p.Policy.Allows("screens.control") || !p.Policy.AllowsScreen(screen) {
			return domain.Errorf(domain.CodeForbidden, "archive display denied")
		}
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM screens s JOIN local_household l ON l.singleton=1 WHERE s.id=? AND s.status='active' AND s.revoked_at IS NULL AND l.household_id=?`, screen, h).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return domain.ErrNotFound
		}
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_query_receipts WHERE id=? AND household_id=? AND principal_id=? AND credential_id=? AND expires_at>?`, query, h, a.PrincipalID, a.CredentialID, FormatTime(now)).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return domain.ErrNotFound
		}
		var oldQuery, oldScreen, expires string
		e = tx.QueryRowContext(ctx, `SELECT id,query_ref,screen_id,expires_at FROM archive_screen_views WHERE principal_id=? AND operation_id=?`, a.PrincipalID, operation).Scan(&view.ID, &oldQuery, &oldScreen, &expires)
		if e == nil {
			if oldQuery != query || oldScreen != screen {
				return domain.ErrConflict
			}
			view.ExpiresAt, e = ParseTime(expires)
			if e != nil {
				return e
			}
			if !view.ExpiresAt.After(now) {
				return domain.ErrNotFound
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		view = ArchiveScreenView{ID: operation, ExpiresAt: now.Add(2 * time.Minute)}
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_screen_views(id,query_ref,household_id,principal_id,credential_id,screen_id,operation_id,expires_at) VALUES(?,?,?,?,?,?,?,?)`, view.ID, query, h, a.PrincipalID, a.CredentialID, screen, operation, FormatTime(view.ExpiresAt))
		return e
	})
	return view, e
}

// AuthorizeArchiveViewIn can be used by the command service within its own
// transaction. It checks the original credential too, not just principal ID.
func (r *ArchiveTasks) AuthorizeArchiveViewIn(ctx context.Context, tx *sql.Tx, principal, screen, view string) (archive.SearchFilter, error) {
	var filter archive.SearchFilter
	var h, p, c, raw, expires string
	e := tx.QueryRowContext(ctx, `SELECT v.household_id,v.principal_id,v.credential_id,q.filter_json,v.expires_at FROM archive_screen_views v JOIN archive_query_receipts q ON q.id=v.query_ref JOIN local_household l ON l.singleton=1 AND l.household_id=v.household_id JOIN screens s ON s.id=v.screen_id AND s.status='active' AND s.revoked_at IS NULL WHERE v.id=? AND v.screen_id=?`, view, screen).Scan(&h, &p, &c, &raw, &expires)
	if errors.Is(e, sql.ErrNoRows) {
		return filter, domain.ErrNotFound
	}
	if e != nil {
		return filter, e
	}
	if principal != "" && principal != p {
		return filter, domain.ErrNotFound
	}
	now, e := r.currentTime()
	if e != nil {
		return filter, e
	}
	expiry, e := ParseTime(expires)
	if e != nil || !expiry.After(now) {
		return filter, domain.ErrNotFound
	}
	owner, e := r.authorizeArchiveClient(ctx, tx, domain.IntegrationActor{PrincipalID: p, CredentialID: c}, h, "archive.read", now)
	if e != nil {
		return filter, e
	}
	if !owner.Policy.Allows("screens.control") || !owner.Policy.AllowsScreen(screen) {
		return filter, domain.Errorf(domain.CodeForbidden, "archive display denied")
	}
	if e = json.Unmarshal([]byte(raw), &filter); e != nil {
		return filter, e
	}
	return filter, nil
}
func (r *ArchiveTasks) ArchiveScreenViewFilter(ctx context.Context, screen, hash, h, view string) (archive.SearchFilter, error) {
	var filter archive.SearchFilter
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	e := r.db.InReadTx(ctx, func(tx *sql.Tx) error {
		if _, e := r.authorizeSearchIn(ctx, tx, archiveSearchActor{ScreenID: screen, ScreenHash: hash}, h); e != nil {
			return e
		}
		var e error
		filter, e = r.AuthorizeArchiveViewIn(ctx, tx, "", screen, view)
		return e
	})
	return filter, e
}

// ArchiveScreenViewAnalysis returns only the stored operation; the results are
// recomputed through the screen's current public authority.
func (r *ArchiveTasks) ArchiveScreenViewAnalysis(ctx context.Context, screen, hash, h, view string) (*archive.AnalysisQuery, error) {
	var out *archive.AnalysisQuery
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	e := r.db.InReadTx(ctx, func(tx *sql.Tx) error {
		if _, e := r.authorizeSearchIn(ctx, tx, archiveSearchActor{ScreenID: screen, ScreenHash: hash}, h); e != nil {
			return e
		}
		if _, e := r.AuthorizeArchiveViewIn(ctx, tx, "", screen, view); e != nil {
			return e
		}
		var raw sql.NullString
		if e := tx.QueryRowContext(ctx, `SELECT q.analysis_json FROM archive_screen_views v JOIN archive_query_receipts q ON q.id=v.query_ref WHERE v.id=?`, view).Scan(&raw); e != nil {
			return e
		}
		if raw.Valid {
			out = &archive.AnalysisQuery{}
			return json.Unmarshal([]byte(raw.String), out)
		}
		return nil
	})
	return out, e
}

func pruneArchiveQueriesIn(ctx context.Context, tx *sql.Tx, now time.Time) error {
	if _, e := tx.ExecContext(ctx, `DELETE FROM archive_screen_views WHERE expires_at<=?`, FormatTime(now)); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `DELETE FROM archive_query_receipts WHERE expires_at<=? AND NOT EXISTS(SELECT 1 FROM archive_screen_views v WHERE v.query_ref=archive_query_receipts.id)`, FormatTime(now))
	return e
}
