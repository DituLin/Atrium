package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/domain"
	"strings"
)

// ArchiveRevision contains a human correction with its audit reason.
type ArchiveRevision struct{ Kind, ObservationID, Value, EntityID, Reason string }

// ReviseArchive applies an authorized correction at the expected document revision.
func (r *ArchiveTasks) ReviseArchive(ctx context.Context, a domain.IntegrationActor, h, doc string, expected int64, change ArchiveRevision) (int64, error) {
	var revision int64
	if len(change.Value) > 16384 || len(change.Reason) > 1024 || strings.TrimSpace(change.Reason) == "" {
		return 0, domain.ErrConflict
	}
	if change.Kind != "value" && change.Kind != "reassign" && change.Kind != "private" && change.Kind != "withdraw" {
		return 0, domain.ErrConflict
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
		if !p.Policy.AllowsArchive("archive.organize", h) {
			return domain.Errorf(domain.CodeForbidden, "archive.organize required")
		}
		var publication sql.NullString
		e = tx.QueryRowContext(ctx, `SELECT document_revision,active_publication_id FROM archive_documents WHERE household_id=? AND id=?`, h, doc).Scan(&revision, &publication)
		if errors.Is(e, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if e != nil {
			return e
		}
		if revision != expected {
			return domain.ErrConflict
		}
		var previousBody, extraction string
		var observationRevision int64
		if change.ObservationID != "" {
			e = tx.QueryRowContext(ctx, `SELECT r.body_json,r.extraction_id,r.revision FROM archive_publication_observations p JOIN archive_observation_revisions r ON r.household_id=p.household_id AND r.observation_id=p.observation_id AND r.revision=p.revision WHERE p.household_id=? AND p.publication_id=? AND p.observation_id=?`, h, publication.String, change.ObservationID).Scan(&previousBody, &extraction, &observationRevision)
			if errors.Is(e, sql.ErrNoRows) {
				return domain.ErrNotFound
			}
			if e != nil {
				return e
			}
		}
		if (change.Kind == "value" || change.Kind == "reassign") && previousBody == "" {
			return domain.ErrConflict
		}
		if change.Kind == "reassign" {
			if _, _, e = archiveEntityVersionIn(ctx, tx, h, change.EntityID); e != nil {
				return e
			}
		}
		revision++
		id := domain.NewID()
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_manual_revisions VALUES(?,?,?,?,?,?,?,?)`, h, id, doc, revision, a.PrincipalID, change.Kind, entityJSON(change), FormatTime(now))
		if e != nil {
			return e
		}
		if change.Kind == "private" {
			_, e = tx.ExecContext(ctx, `INSERT INTO archive_private_regions VALUES(?,?,?,?) ON CONFLICT(household_id,document_id,observation_id) DO UPDATE SET revision_id=excluded.revision_id`, h, doc, change.ObservationID, id)
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `UPDATE archive_documents SET original_restricted=1,tv_visible=CASE WHEN ?='' THEN 0 ELSE tv_visible END WHERE household_id=? AND id=?`, change.ObservationID, h, doc)
			if e != nil {
				return e
			}
		}
		if change.Kind == "withdraw" {
			_, e = tx.ExecContext(ctx, `UPDATE archive_documents SET withdrawn=1 WHERE household_id=? AND id=?`, h, doc)
			if e != nil {
				return e
			}
		}
		if publication.Valid {
			newPublication := domain.NewID()
			_, e = tx.ExecContext(ctx, `INSERT INTO archive_publications SELECT household_id,?,document_id,extraction_id,?,classification_json,status,coverage_json FROM archive_publications WHERE household_id=? AND id=?`, newPublication, revision, h, publication.String)
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO archive_publication_observations SELECT household_id,?,observation_id,revision FROM archive_publication_observations WHERE household_id=? AND publication_id=?`, newPublication, h, publication.String)
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO archive_publication_alternatives SELECT household_id,?,observation_id,extraction_id FROM archive_publication_alternatives WHERE household_id=? AND publication_id=?`, newPublication, h, publication.String)
			if e != nil {
				return e
			}
			if change.Kind == "value" || change.Kind == "reassign" {
				var body map[string]json.RawMessage
				if e = json.Unmarshal([]byte(previousBody), &body); e != nil {
					return e
				}
				body["UserAssertion"] = json.RawMessage(entityJSON(change))
				if change.Kind == "value" {
					body["AssertedValue"] = json.RawMessage(entityJSON(change.Value))
					body["ValueAssertionID"] = json.RawMessage(entityJSON(id))
				}
				if change.Kind == "reassign" {
					body["Entities"] = json.RawMessage(entityJSON([]string{change.EntityID}))
					body["OwnershipAssertionID"] = json.RawMessage(entityJSON(id))
				}
				if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM archive_observation_revisions WHERE household_id=? AND observation_id=?`, h, change.ObservationID).Scan(&observationRevision); e != nil {
					return e
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO archive_observation_revisions VALUES(?,?,?,?,?,'user')`, h, change.ObservationID, observationRevision, extraction, entityJSON(body))
				if e != nil {
					return e
				}
				_, e = tx.ExecContext(ctx, `UPDATE archive_publication_observations SET revision=? WHERE household_id=? AND publication_id=? AND observation_id=?`, observationRevision, h, newPublication, change.ObservationID)
				if e != nil {
					return e
				}
			}
			_, e = tx.ExecContext(ctx, `UPDATE archive_documents SET active_publication_id=? WHERE household_id=? AND id=?`, newPublication, h, doc)
			if e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, `UPDATE archive_documents SET document_revision=? WHERE household_id=? AND id=?`, revision, h, doc)
		if e != nil {
			return e
		}
		return archiveDocumentChangedIn(ctx, tx, h, doc, revision)
	})
	return revision, e
}
