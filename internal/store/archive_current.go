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
			assertion := assertions[item.ID]
			item, e = archiveComposeAssertions(item, assertion)
			if e != nil {
				return e
			}
			total += len(item.Body) - len(bodyJSON)
			if total > 4<<20 {
				return domain.ErrConflict
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
			safe := !conflicts[item.ID] || assertion.Value != nil
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

// Value and ownership are independent sticky assertions. Each is selected from
// its immutable audit entry and authored observation revision, while a current
// publication of that same source must still carry the observation at or after
// that revision. Global observation revision orders replacements within an axis.
type ArchiveAssertionProvenance struct {
	ID, Kind, Actor, DocumentID, PublicationID, ActivePublicationID, ExtractionID string
	DocumentRevision, ObservationRevision                                         int64
}
type archiveCurrentAssertion struct {
	Source       ArchiveCurrentObservation
	AuthoredBody json.RawMessage
	Provenance   ArchiveAssertionProvenance
	Change       ArchiveRevision
}
type archiveAssertionSet struct{ Value, Ownership *archiveCurrentAssertion }

func archiveCurrentAssertionsIn(ctx context.Context, tx *sql.Tx, h, publication string, public bool) (map[string]archiveAssertionSet, error) {
	rows, e := tx.QueryContext(ctx, `SELECT r.observation_id,active.revision,active.extraction_id,active.body_json,d.id,d.document_revision,d.active_publication_id,o.event_id,m.id,m.actor,m.kind,m.body_json,d.active_publication_id,m.document_revision,authored.id,r.extraction_id,r.revision,r.body_json FROM archive_manual_revisions m JOIN archive_documents d ON d.household_id=m.household_id AND d.id=m.document_id JOIN archive_publications authored ON authored.household_id=m.household_id AND authored.document_id=m.document_id AND authored.document_revision=m.document_revision JOIN archive_publication_observations p ON p.household_id=authored.household_id AND p.publication_id=authored.id AND p.observation_id=json_extract(m.body_json,'$.ObservationID') JOIN archive_observation_revisions r ON r.household_id=p.household_id AND r.observation_id=p.observation_id AND r.revision=p.revision JOIN archive_observations o ON o.household_id=p.household_id AND o.id=p.observation_id JOIN archive_publication_observations current ON current.household_id=p.household_id AND current.publication_id=d.active_publication_id AND current.observation_id=p.observation_id JOIN archive_observation_revisions active ON active.household_id=current.household_id AND active.observation_id=current.observation_id AND active.revision=current.revision WHERE m.household_id=? AND m.kind IN ('value','reassign') AND d.withdrawn=0 AND (?=0 OR d.tv_visible=1) AND current.revision>=r.revision AND (json_extract(active.body_json,CASE m.kind WHEN 'value' THEN '$.ValueAssertionID' ELSE '$.OwnershipAssertionID' END)=m.id OR json_extract(active.body_json,CASE m.kind WHEN 'value' THEN '$.ValueAssertionID' ELSE '$.OwnershipAssertionID' END) IS NULL) AND EXISTS(SELECT 1 FROM archive_publication_observations target WHERE target.household_id=p.household_id AND target.publication_id=? AND target.observation_id=p.observation_id) ORDER BY r.revision DESC LIMIT 4097`, h, public, publication)
	if e != nil {
		return nil, e
	}
	items := []archiveCurrentAssertion{}
	total := 0
	for rows.Next() {
		var a archiveCurrentAssertion
		var raw, change, authoredBody string
		e = rows.Scan(&a.Source.ID, &a.Source.Revision, &a.Source.ExtractionID, &raw, &a.Source.SourceDocumentID, &a.Source.DocumentRevision, &a.Source.PublicationID, &a.Source.EventID, &a.Provenance.ID, &a.Provenance.Actor, &a.Provenance.Kind, &change, &a.Provenance.ActivePublicationID, &a.Provenance.DocumentRevision, &a.Provenance.PublicationID, &a.Provenance.ExtractionID, &a.Provenance.ObservationRevision, &authoredBody)
		if e != nil {
			rows.Close()
			return nil, e
		}
		a.Source.Body = json.RawMessage(raw)
		a.AuthoredBody = json.RawMessage(authoredBody)
		a.Source.AssertionKind = "user"
		if e = json.Unmarshal([]byte(change), &a.Change); e != nil {
			rows.Close()
			return nil, e
		}
		a.Provenance.DocumentID = a.Source.SourceDocumentID
		total += len(raw) + len(change) + len(authoredBody)
		items = append(items, a)
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
	out := map[string]archiveAssertionSet{}
	cache := map[string][]archive.PhysicalAnchor{}
	for _, a := range items {
		set := out[a.Source.ID]
		if a.Change.Kind == "value" && set.Value != nil || a.Change.Kind == "reassign" && set.Ownership != nil {
			continue
		}
		safe := true
		if public {
			secrets, ok := cache[a.Source.SourceDocumentID]
			if !ok {
				secrets, e = archivePrivateAnchorsIn(ctx, tx, h, a.Source.SourceDocumentID)
				if e != nil {
					return nil, e
				}
				cache[a.Source.SourceDocumentID] = secrets
			}
			var b struct{ Field archive.PublicationField }
			if e = json.Unmarshal(a.Source.Body, &b); e != nil {
				return nil, e
			}
			var authored struct{ Field archive.PublicationField }
			if e = json.Unmarshal(a.AuthoredBody, &authored); e != nil {
				return nil, e
			}
			for _, secret := range secrets {
				for _, anchor := range append(b.Field.VisibilityAnchors, authored.Field.VisibilityAnchors...) {
					if !archiveAnchorsDisjoint(secret, anchor) {
						safe = false
					}
				}
			}
		}
		if safe {
			copy := a
			if a.Change.Kind == "value" {
				set.Value = &copy
			} else {
				set.Ownership = &copy
			}
			out[a.Source.ID] = set
		}
	}
	return out, nil
}
func archiveComposeAssertions(item ArchiveCurrentObservation, set archiveAssertionSet) (ArchiveCurrentObservation, error) {
	if set.Value == nil && set.Ownership == nil {
		return item, nil
	}
	if set.Value != nil {
		item = set.Value.Source
	}
	var body map[string]json.RawMessage
	if e := json.Unmarshal(item.Body, &body); e != nil {
		return item, e
	}
	// Never borrow an unrelated axis from the last whole-body user revision.
	delete(body, "UserAssertion")
	delete(body, "AssertedValue")
	delete(body, "ValueAssertionID")
	delete(body, "OwnershipAssertionID")
	if set.Value != nil {
		body["AssertedValue"] = json.RawMessage(entityJSON(set.Value.Change.Value))
		body["ValueAssertion"] = json.RawMessage(entityJSON(set.Value.Provenance))
	}
	if set.Ownership != nil {
		body["Entities"] = json.RawMessage(entityJSON([]string{set.Ownership.Change.EntityID}))
		body["OwnershipAssertion"] = json.RawMessage(entityJSON(set.Ownership.Provenance))
	}
	item.Body = json.RawMessage(entityJSON(body))
	item.AssertionKind = "user"
	return item, nil
}

// Current source values, not the historical fact that an alternative once
// existed, determine whether a shared transaction is presently contradictory.
func archiveCurrentConflictsIn(ctx context.Context, tx *sql.Tx, h, publication string, public bool) (map[string]bool, error) {
	proofs, e := archiveCurrentAmountProofsIn(ctx, tx, h, publication, "", "", public)
	if e != nil {
		return nil, e
	}
	signatures := map[string]string{}
	out := map[string]bool{}
	for _, proof := range proofs {
		if previous, ok := signatures[proof.ID]; ok && previous != proof.Signature {
			out[proof.ID] = true
		}
		signatures[proof.ID] = proof.Signature
	}
	return out, nil
}

type archiveAmountProof struct{ ID, DocumentID, Signature string }

func archiveCurrentAmountProofsIn(ctx context.Context, tx *sql.Tx, h, publication, observation, excludeDocument string, public bool) ([]archiveAmountProof, error) {
	rows, e := tx.QueryContext(ctx, `SELECT p.observation_id,d.id,r.body_json FROM archive_documents d JOIN archive_publication_observations p ON p.household_id=d.household_id AND p.publication_id=d.active_publication_id JOIN archive_observations o ON o.household_id=p.household_id AND o.id=p.observation_id JOIN archive_observation_revisions r ON r.household_id=p.household_id AND r.observation_id=p.observation_id AND r.revision=p.revision WHERE d.household_id=? AND d.withdrawn=0 AND d.id<>? AND o.slot_key='supported-transaction-amount/1' AND (?=0 OR d.tv_visible=1) AND (?='' OR p.observation_id=?) AND (?='' OR EXISTS(SELECT 1 FROM archive_publication_observations target WHERE target.household_id=p.household_id AND target.publication_id=? AND target.observation_id=p.observation_id)) LIMIT 4097`, h, excludeDocument, public, observation, observation, publication, publication)
	if e != nil {
		return nil, e
	}
	type rawProof struct{ id, doc, body string }
	raws := []rawProof{}
	total := 0
	for rows.Next() {
		var p rawProof
		if e = rows.Scan(&p.id, &p.doc, &p.body); e != nil {
			rows.Close()
			return nil, e
		}
		total += len(p.body)
		raws = append(raws, p)
		if total > 4<<20 || len(raws) > 4096 {
			rows.Close()
			return nil, domain.ErrConflict
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []archiveAmountProof{}
	cache := map[string][]archive.PhysicalAnchor{}
	for _, p := range raws {
		var b struct{ Field archive.PublicationField }
		if e = json.Unmarshal([]byte(p.body), &b); e != nil {
			return nil, e
		}
		safe := true
		if public {
			secrets, ok := cache[p.doc]
			if !ok {
				secrets, e = archivePrivateAnchorsIn(ctx, tx, h, p.doc)
				if e != nil {
					return nil, e
				}
				cache[p.doc] = secrets
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
			out = append(out, archiveAmountProof{p.id, p.doc, archiveAmountSignature(b.Field)})
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
