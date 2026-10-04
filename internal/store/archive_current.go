package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
)

type ArchiveCurrentObservation struct {
	ID, ExtractionID, EventID, PublicationID string
	SourceDocumentID                         string
	Revision, DocumentRevision               int64
	Body                                     json.RawMessage
	AssertionKind                            string
}

// CurrentArchiveObservations reads only active publication references. The
// public projection is a narrowing option for an already authorized client;
// future screen authentication must use its own repository entry point.
func (r *ArchiveTasks) CurrentArchiveObservations(ctx context.Context, a domain.IntegrationActor, h, doc string, public bool) ([]ArchiveCurrentObservation, error) {
	out := []ArchiveCurrentObservation{}
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		if _, e = r.authorizeArchiveClient(ctx, tx, a, h, "archive.read", now); e != nil {
			return e
		}
		var publication sql.NullString
		var revision int64
		var visible, withdrawn bool
		e = tx.QueryRowContext(ctx, `SELECT active_publication_id,document_revision,tv_visible,withdrawn FROM archive_documents WHERE household_id=? AND id=?`, h, doc).Scan(&publication, &revision, &visible, &withdrawn)
		if errors.Is(e, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if e != nil {
			return e
		}
		if withdrawn || !publication.Valid || (public && !visible) {
			return nil
		}
		private := []archive.PhysicalAnchor{}
		if public {
			private, e = archivePrivateAnchorsIn(ctx, tx, h, doc)
			if e != nil {
				return e
			}
		}
		conflicts, e := archiveCurrentConflictsIn(ctx, tx, h, publication.String, public)
		if e != nil {
			return e
		}
		assertions, e := archiveCurrentAssertionsIn(ctx, tx, h, publication.String, public)
		if e != nil {
			return e
		}
		confirmed := map[string]string{}
		linkRows, e := tx.QueryContext(ctx, `SELECT mention_ref,matched_entity_id FROM archive_entity_links WHERE household_id=? AND document_id=? AND status='linked' LIMIT 2049`, h, doc)
		if e != nil {
			return e
		}
		for linkRows.Next() {
			var mention, target string
			if e = linkRows.Scan(&mention, &target); e != nil {
				linkRows.Close()
				return e
			}
			confirmed[mention] = target
			if len(confirmed) > 2048 {
				linkRows.Close()
				return domain.ErrConflict
			}
		}
		e = linkRows.Err()
		linkRows.Close()
		if e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, `SELECT o.id,o.event_id,r.revision,r.extraction_id,r.body_json,r.assertion_kind FROM archive_publication_observations p JOIN archive_observations o ON o.household_id=p.household_id AND o.id=p.observation_id JOIN archive_observation_revisions r ON r.household_id=p.household_id AND r.observation_id=p.observation_id AND r.revision=p.revision WHERE p.household_id=? AND p.publication_id=? ORDER BY o.id LIMIT 4097`, h, publication.String)
		if e != nil {
			return e
		}
		defer rows.Close()
		total, count := 0, 0
		for rows.Next() {
			var item ArchiveCurrentObservation
			var bodyJSON string
			item.PublicationID = publication.String
			item.DocumentRevision = revision
			item.SourceDocumentID = doc
			if e = rows.Scan(&item.ID, &item.EventID, &item.Revision, &item.ExtractionID, &bodyJSON, &item.AssertionKind); e != nil {
				return e
			}
			count++
			total += len(bodyJSON)
			if total > 4<<20 || count > 4096 {
				return domain.ErrConflict
			}
			item.Body = json.RawMessage(bodyJSON)
			if public {
				var original struct{ Field archive.PublicationField }
				if e = json.Unmarshal(item.Body, &original); e != nil {
					return e
				}
				hidden := false
				for _, secret := range private {
					for _, anchor := range original.Field.VisibilityAnchors {
						if !archiveAnchorsDisjoint(secret, anchor) {
							hidden = true
						}
					}
				}
				if hidden {
					continue
				}
			}
			if assertion, ok := assertions[item.ID]; ok {
				item = assertion
			}

			if total > 4<<20 || len(out) >= 4096 {
				return domain.ErrConflict
			}
			var body struct {
				Field    archive.PublicationField
				Entities []string
			}
			if e = json.Unmarshal(item.Body, &body); e != nil {
				return e
			}
			safe := !conflicts[item.ID] || item.AssertionKind == "user"
			if item.AssertionKind != "user" {
				for _, mention := range body.Field.Subjects {
					target := confirmed[mention]
					matched := false
					for _, owner := range body.Entities {
						matched = matched || target != "" && target == owner
					}
					if !matched {
						safe = false
					}
				}
			}
			if public && item.SourceDocumentID == doc {
				for _, secret := range private {
					for _, anchor := range body.Field.VisibilityAnchors {
						if !archiveAnchorsDisjoint(secret, anchor) {
							safe = false
						}
					}
				}
			}
			if safe {
				out = append(out, item)
			}
		}
		return rows.Err()
	})
	return out, e
}

// Historical revisions remain privacy dependencies when parsing changes anchors.
func archivePrivateAnchorsIn(ctx context.Context, tx *sql.Tx, h, doc string) ([]archive.PhysicalAnchor, error) {
	rows, e := tx.QueryContext(ctx, `SELECT r.body_json FROM archive_private_regions p JOIN archive_observation_revisions r ON r.household_id=p.household_id AND r.observation_id=p.observation_id JOIN archive_extractions x ON x.household_id=r.household_id AND x.id=r.extraction_id AND x.document_id=p.document_id WHERE p.household_id=? AND p.document_id=? UNION ALL SELECT a.body_json FROM archive_private_regions p JOIN archive_observation_alternatives a ON a.household_id=p.household_id AND a.observation_id=p.observation_id JOIN archive_extractions x ON x.household_id=a.household_id AND x.id=a.extraction_id AND x.document_id=p.document_id WHERE p.household_id=? AND p.document_id=? LIMIT 4097`, h, doc, h, doc)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []archive.PhysicalAnchor{}
	total, count := 0, 0
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		total += len(raw)
		count++
		if total > 4<<20 || count > 4096 {
			return nil, domain.ErrConflict
		}
		var b struct{ Field archive.PublicationField }
		if e = json.Unmarshal([]byte(raw), &b); e != nil {
			return nil, e
		}
		out = append(out, b.Field.VisibilityAnchors...)
		if len(out) > 4096 {
			return nil, domain.ErrConflict
		}
	}
	return out, rows.Err()
}

// A shared observation's active human assertions are globally ordered by the
// transactionally allocated immutable revision. Return its actual provenance;
// never relabel the asserted value as another source's quotation.
func archiveCurrentAssertionsIn(ctx context.Context, tx *sql.Tx, h, publication string, public bool) (map[string]ArchiveCurrentObservation, error) {
	rows, e := tx.QueryContext(ctx, `SELECT r.observation_id,r.revision,r.extraction_id,r.body_json,d.id,d.document_revision,d.active_publication_id,o.event_id FROM archive_documents d JOIN archive_publication_observations p ON p.household_id=d.household_id AND p.publication_id=d.active_publication_id JOIN archive_observation_revisions r ON r.household_id=p.household_id AND r.observation_id=p.observation_id AND r.revision=p.revision JOIN archive_observations o ON o.household_id=r.household_id AND o.id=r.observation_id WHERE d.household_id=? AND d.withdrawn=0 AND r.assertion_kind='user' AND (?=0 OR d.tv_visible=1) AND EXISTS(SELECT 1 FROM archive_publication_observations target WHERE target.household_id=p.household_id AND target.publication_id=? AND target.observation_id=p.observation_id) ORDER BY r.revision DESC LIMIT 4097`, h, public, publication)
	if e != nil {
		return nil, e
	}
	items := []ArchiveCurrentObservation{}
	total := 0
	for rows.Next() {
		var item ArchiveCurrentObservation
		var raw string
		e = rows.Scan(&item.ID, &item.Revision, &item.ExtractionID, &raw, &item.SourceDocumentID, &item.DocumentRevision, &item.PublicationID, &item.EventID)
		if e != nil {
			rows.Close()
			return nil, e
		}
		item.Body = json.RawMessage(raw)
		item.AssertionKind = "user"
		total += len(raw)
		items = append(items, item)
		if total > 4<<20 || len(items) > 4096 {
			rows.Close()
			return nil, domain.ErrConflict
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := map[string]ArchiveCurrentObservation{}
	cache := map[string][]archive.PhysicalAnchor{}
	for _, item := range items {
		if _, ok := out[item.ID]; ok {
			continue
		}
		safe := true
		if public {
			secrets, ok := cache[item.SourceDocumentID]
			if !ok {
				secrets, e = archivePrivateAnchorsIn(ctx, tx, h, item.SourceDocumentID)
				if e != nil {
					return nil, e
				}
				cache[item.SourceDocumentID] = secrets
			}
			var b struct{ Field archive.PublicationField }
			if e = json.Unmarshal(item.Body, &b); e != nil {
				return nil, e
			}
			for _, secret := range secrets {
				for _, anchor := range b.Field.VisibilityAnchors {
					if !archiveAnchorsDisjoint(secret, anchor) {
						safe = false
					}
				}
			}
		}
		if safe {
			out[item.ID] = item
		}
	}
	return out, nil
}

func archiveCurrentConflictsIn(ctx context.Context, tx *sql.Tx, h, publication string, public bool) (map[string]bool, error) {
	rows, e := tx.QueryContext(ctx, `SELECT a.observation_id,ad.id,a.body_json FROM archive_observation_alternatives a JOIN archive_extractions ax ON ax.household_id=a.household_id AND ax.id=a.extraction_id JOIN archive_documents ad ON ad.household_id=ax.household_id AND ad.id=ax.document_id JOIN archive_publication_alternatives pa ON pa.household_id=ad.household_id AND pa.publication_id=ad.active_publication_id AND pa.observation_id=a.observation_id AND pa.extraction_id=a.extraction_id WHERE a.household_id=? AND ad.withdrawn=0 AND (?=0 OR ad.tv_visible=1) AND EXISTS(SELECT 1 FROM archive_publication_observations po WHERE po.household_id=a.household_id AND po.publication_id=? AND po.observation_id=a.observation_id) LIMIT 4097`, h, public, publication)
	if e != nil {
		return nil, e
	}
	type alternative struct{ id, doc, body string }
	alternatives := []alternative{}
	total := 0
	for rows.Next() {
		var a alternative
		if e = rows.Scan(&a.id, &a.doc, &a.body); e != nil {
			rows.Close()
			return nil, e
		}
		total += len(a.body)
		alternatives = append(alternatives, a)
		if total > 4<<20 || len(alternatives) > 4096 {
			rows.Close()
			return nil, domain.ErrConflict
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := map[string]bool{}
	cache := map[string][]archive.PhysicalAnchor{}
	for _, a := range alternatives {
		safe := true
		if public {
			secrets, ok := cache[a.doc]
			if !ok {
				secrets, e = archivePrivateAnchorsIn(ctx, tx, h, a.doc)
				if e != nil {
					return nil, e
				}
				cache[a.doc] = secrets
			}
			var b struct{ Field archive.PublicationField }
			if e = json.Unmarshal([]byte(a.body), &b); e != nil {
				return nil, e
			}
			for _, secret := range secrets {
				for _, anchor := range b.Field.VisibilityAnchors {
					if !archiveAnchorsDisjoint(secret, anchor) {
						safe = false
					}
				}
			}
		}
		if safe {
			out[a.id] = true
		}
	}
	return out, nil
}

func archiveAnchorsDisjoint(a, b archive.PhysicalAnchor) bool {
	if a.Locator.Kind != b.Locator.Kind {
		return false
	}
	if a.Locator.Text != nil && b.Locator.Text != nil {
		return a.Locator.Text.Start+a.End <= b.Locator.Text.Start+b.Start || b.Locator.Text.Start+b.End <= a.Locator.Text.Start+a.Start
	}
	if a.Locator.CSV != nil && b.Locator.CSV != nil {
		return a.Locator.CSV.Record != b.Locator.CSV.Record || a.Locator.CSV.Column != b.Locator.CSV.Column
	}
	if a.Locator.XLSX != nil && b.Locator.XLSX != nil {
		return a.Locator.XLSX.PartURI != b.Locator.XLSX.PartURI || a.Locator.XLSX.Cell != b.Locator.XLSX.Cell
	}
	if a.Locator.DOCX != nil && b.Locator.DOCX != nil {
		return entityJSON(a.Locator.DOCX) != entityJSON(b.Locator.DOCX)
	}
	if a.Locator.Page != nil && b.Locator.Page != nil {
		if a.Locator.Page.Page != b.Locator.Page.Page {
			return true
		}
		if entityJSON(a.Geometry) != entityJSON(b.Geometry) {
			return false
		}
		x, y := a.Locator.Page.Rect, b.Locator.Page.Rect
		return x[0]+x[2] <= y[0] || y[0]+y[2] <= x[0] || x[1]+x[3] <= y[1] || y[1]+y[3] <= x[1]
	}
	return false
}

// ArchiveOriginalReference authorizes the complete, unredacted original; any
// private child region denies a public original/page/context read.
func (r *ArchiveTasks) ArchiveOriginalReference(ctx context.Context, a domain.IntegrationActor, h, doc string, public bool) (domain.ArchiveBlobRef, error) {
	var ref domain.ArchiveBlobRef
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		if _, e = r.authorizeArchiveClient(ctx, tx, a, h, "archive.read", now); e != nil {
			return e
		}
		e = tx.QueryRowContext(ctx, `SELECT b.household_id,b.sha256,b.size_bytes,b.mime,b.storage_version FROM archive_documents d JOIN archive_blobs b ON b.household_id=d.household_id AND b.sha256=d.blob_sha256 WHERE d.household_id=? AND d.id=? AND d.withdrawn=0 AND (?=0 OR (d.tv_visible=1 AND d.original_restricted=0))`, h, doc, public).Scan(&ref.HouseholdID, &ref.SHA256, &ref.SizeBytes, &ref.MIME, &ref.StorageVersion)
		if errors.Is(e, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return e
	})
	return ref, e
}
