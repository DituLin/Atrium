package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
)

type ArchivePublicationBundle struct {
	Parsed        archive.ParseResult
	Plan          archive.UnderstandingPlan
	Understanding archive.Understanding
}

func archiveLoadBundleIn(ctx context.Context, tx *sql.Tx, f ArchiveWorkFence, x ArchiveExtraction) (ArchivePublicationBundle, error) {
	var b ArchivePublicationBundle
	var raw []byte
	var digest string
	e := tx.QueryRowContext(ctx, `SELECT p.body,p.digest FROM archive_products p JOIN archive_extraction_products x ON x.household_id=p.household_id AND x.task_id=p.task_id AND x.product_key=p.product_key AND x.fingerprint=p.fingerprint AND x.digest=p.digest WHERE p.household_id=? AND p.task_id=? AND p.fingerprint=? AND p.product_key='publication' AND x.extraction_id=?`, f.HouseholdID, f.TaskID, x.Fingerprint, x.ID).Scan(&raw, &digest)
	if e != nil {
		return b, e
	}
	if len(raw) > MaxArchiveProductBytes || productDigest(raw) != digest {
		return b, domain.ErrConflict
	}
	e = json.Unmarshal(raw, &b)
	return b, e
}

// PublishExtraction is the sole candidate -> confirmed transaction. Inference
// happens before this call; evidence, current identities and all fences are
// checked again while the writer lock is held.
func (r *ArchiveTasks) PublishExtraction(ctx context.Context, f ArchiveWorkFence, extractionID string) (*domain.ArchiveTask, error) {
	var out *domain.ArchiveTask
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		t, e := r.workIn(ctx, tx, f, now)
		if e != nil {
			return e
		}
		var x ArchiveExtraction
		x.ID = extractionID
		e = tx.QueryRowContext(ctx, `SELECT document_id,fingerprint,document_revision,resolution_revision FROM archive_extractions WHERE household_id=? AND task_id=? AND id=? AND status='candidate'`, f.HouseholdID, f.TaskID, x.ID).Scan(&x.DocumentID, &x.Fingerprint, &x.DocumentRevision, &x.ResolutionRevision)
		if errors.Is(e, sql.ErrNoRows) {
			return domain.ErrConflict
		}
		if e != nil {
			return e
		}
		var rev int64
		var prior sql.NullString
		var withdrawn bool
		e = tx.QueryRowContext(ctx, `SELECT document_revision,active_publication_id,withdrawn FROM archive_documents WHERE household_id=? AND id=?`, f.HouseholdID, x.DocumentID).Scan(&rev, &prior, &withdrawn)
		if e != nil {
			return e
		}
		if rev != x.DocumentRevision || withdrawn {
			return domain.ErrConflict
		}
		b, e := archiveLoadBundleIn(ctx, tx, f, x)
		if e != nil {
			return e
		}
		candidate, e := archive.CompilePublication(f.HouseholdID, x.DocumentID, b.Parsed, b.Plan, b.Understanding)
		if e != nil {
			return e
		}

		probes := map[string]IdentityProbe{}
		for _, identity := range candidate.Protected {
			probe := IdentityProbe{Kind: identity.Kind, Issuer: identity.Issuer, RuleVersion: IdentityExactVersion, Value: identity.Value, Evidence: identity.Evidence}
			if previous, exists := probes[identity.MentionRef]; exists {
				duplicate := false
				for _, part := range archiveIdentityParts(previous) {
					duplicate = duplicate || part.Kind == probe.Kind && part.Issuer == probe.Issuer && part.Value == probe.Value
				}
				if !duplicate {
					previous.Additional = append(previous.Additional, probe)
				}
				probes[identity.MentionRef] = previous
			} else {
				probes[identity.MentionRef] = probe
			}
		}
		snapshot, e := archivePublicationSnapshotIn(ctx, tx, candidate.Resolution, probes)
		if e != nil {
			return e
		}
		if snapshot.Revision != x.ResolutionRevision {
			return domain.ErrConflict
		}
		plan, e := archive.ResolveEntities(candidate.Resolution, snapshot)
		if e != nil {
			return e
		}
		review := candidate.RequiresReview
		pending := false
		unresolved := map[string]bool{}
		for _, decision := range plan.Decisions {
			if decision.Action == "needs_resolution" {
				pending = true
				unresolved[decision.MentionRef] = true
			}
		}
		// A changed physical event partition must not replace established observations.
		rows, e := tx.QueryContext(ctx, `SELECT anchor_key,anchors_json FROM archive_subject_events WHERE household_id=? AND document_id=?`, f.HouseholdID, x.DocumentID)
		if e != nil {
			return e
		}
		old := map[string][]archive.PhysicalAnchor{}
		for rows.Next() {
			var k, raw string
			if e = rows.Scan(&k, &raw); e != nil {
				rows.Close()
				return e
			}
			var anchors []archive.PhysicalAnchor
			if e = json.Unmarshal([]byte(raw), &anchors); e != nil {
				rows.Close()
				return e
			}
			old[k] = anchors
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(old) > 0 {
			used := map[string]bool{}
			for i, ev := range candidate.Events {
				match := ""
				for key, anchors := range old {
					if archive.PhysicalAnchorsMatch(anchors, ev.Anchors) {
						if match != "" {
							review = true
						}
						match = key
					}
				}
				if match == "" || used[match] {
					review = true
				} else {
					candidate.Events[i].AnchorKey = match
					used[match] = true
				}
			}
			if len(used) != len(old) {
				review = true
			}

		}

		if review {
			q, e := archiveCreateQuestionIn(ctx, tx, f, x, plan, snapshot, candidate.Resolution)
			if e != nil {
				return e
			}
			t.ClarificationJSON = json.RawMessage(entityJSON(q))
			e = applyArchiveUpdate(t, domain.ArchiveTaskUpdate{ExpectedStateVersion: f.StateVersion, Stage: "review", StateJSON: t.StateJSON, ResultJSON: json.RawMessage(entityJSON(map[string]any{"parse_status": candidate.Status}))}, domain.ArchiveTaskWaitingInput)
			if e != nil {
				return e
			}
			out = t
			return archiveSave(ctx, tx, t, "publication_waiting", now, "")
		}
		links, e := archiveApplyResolutionIn(ctx, tx, candidate.Resolution, plan, probes)
		if e != nil {
			return e
		}
		for _, identity := range candidate.Protected {
			if entity := links[identity.MentionRef]; entity != "" {
				if e = archiveProtectedIdentityPutIn(ctx, tx, f.HouseholdID, entity, ProtectedIdentityFact{DocumentID: x.DocumentID, Kind: identity.Kind, Issuer: identity.Issuer, RuleVersion: IdentityExactVersion, Value: identity.Value, Evidence: identity.Evidence}); e != nil {
					return e
				}
			}
		}
		publication := domain.NewID()
		newrev := rev + 1
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_publications VALUES(?,?,?,?,?,?,?,?)`, f.HouseholdID, publication, x.DocumentID, x.ID, newrev, entityJSON(candidate.Labels), candidate.Status, entityJSON(candidate.Coverage))
		if e != nil {
			return e
		}
		// Carry forward current references, including evidence from older extractions.
		if prior.Valid {
			_, e = tx.ExecContext(ctx, `INSERT INTO archive_publication_observations SELECT household_id,?,observation_id,revision FROM archive_publication_observations WHERE household_id=? AND publication_id=?`, publication, f.HouseholdID, prior.String)
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO archive_publication_alternatives SELECT household_id,?,observation_id,extraction_id FROM archive_publication_alternatives WHERE household_id=? AND publication_id=?`, publication, f.HouseholdID, prior.String)
			if e != nil {
				return e
			}
		}
		for _, ev := range candidate.Events {
			var eventID string
			e = tx.QueryRowContext(ctx, `SELECT id FROM archive_subject_events WHERE household_id=? AND document_id=? AND anchor_key=?`, f.HouseholdID, x.DocumentID, ev.AnchorKey).Scan(&eventID)
			if errors.Is(e, sql.ErrNoRows) {
				eventID = domain.NewID()
				_, e = tx.ExecContext(ctx, `INSERT INTO archive_subject_events VALUES(?,?,?,?,?,?,?)`, f.HouseholdID, eventID, x.DocumentID, ev.AnchorKey, archive.PublicationAnchorRule, entityJSON(ev.Anchors), ev.Type)
			}
			if e != nil {
				return e
			}
			if e = archiveReconcileObservationSlotsIn(ctx, tx, f.HouseholdID, eventID, ev.Fields); e != nil {
				return e
			}
			canonicalEvent, e := archiveTransactionEventIn(ctx, tx, f.HouseholdID, eventID, ev.Fields)
			if e != nil {
				return e
			}
			for _, field := range ev.Fields {
				if field.Name == "national_id" || field.Name == "passport" || field.Name == "birth_certificate" || field.Name == "identity_issuer" {
					continue
				}
				unsafe := field.RequiresReview
				for _, ref := range field.Subjects {
					unsafe = unsafe || unresolved[ref]
				}
				if unsafe {
					pending = true
					continue
				}
				observationEvent := eventID
				slot := field.SlotKey
				transactionAmount := archive.TransactionIdentity(ev.Fields) != nil && field.Name == "amount" && archiveAmountSignature(field) != ""
				if transactionAmount {
					observationEvent = canonicalEvent
					slot = "supported-transaction-amount/1"
					for _, proofField := range ev.Fields {
						if proofField.Name == "transaction_id" || proofField.Name == "transaction_type" || proofField.Name == "issuer" {
							field.VisibilityAnchors = append(field.VisibilityAnchors, proofField.VisibilityAnchors...)
						}
					}
				}
				var id string
				e = tx.QueryRowContext(ctx, `SELECT id FROM archive_observations WHERE household_id=? AND event_id=? AND slot_key=?`, f.HouseholdID, observationEvent, slot).Scan(&id)
				if errors.Is(e, sql.ErrNoRows) {
					id = domain.NewID()
					_, e = tx.ExecContext(ctx, `INSERT INTO archive_observations VALUES(?,?,?,?)`, f.HouseholdID, id, observationEvent, slot)
				}
				if e != nil {
					return e
				}
				entities := []string{}
				for _, s := range field.Subjects {
					if links[s] != "" {
						entities = append(entities, links[s])
					}
				}
				body := entityJSON(struct {
					Field    archive.PublicationField
					Entities []string
				}{field, entities})
				var n int64
				var prev, previousExtraction string
				e = tx.QueryRowContext(ctx, `SELECT revision,body_json,extraction_id FROM archive_observation_revisions WHERE household_id=? AND observation_id=? ORDER BY revision DESC LIMIT 1`, f.HouseholdID, id).Scan(&n, &prev, &previousExtraction)
				if e != nil && !errors.Is(e, sql.ErrNoRows) {
					return e
				}
				// Source revisions advance independently from audited human assertions.
				// Current reads compose each visible manual axis over the latest source.
				if transactionAmount {
					_, e = tx.ExecContext(ctx, `DELETE FROM archive_publication_alternatives WHERE household_id=? AND publication_id=? AND observation_id=?`, f.HouseholdID, publication, id)
					if e != nil {
						return e
					}
				}
				if transactionAmount {
					proofs, err := archiveCurrentAmountProofsIn(ctx, tx, f.HouseholdID, "", id, x.DocumentID, false)
					if err != nil {
						return err
					}
					conflict := false
					for _, proof := range proofs {
						conflict = conflict || proof.Signature != archiveAmountSignature(field)
					}
					if conflict {
						_, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO archive_observation_alternatives VALUES(?,?,?,?)`, f.HouseholdID, id, x.ID, body)
						if e != nil {
							return e
						}
						_, e = tx.ExecContext(ctx, `INSERT INTO archive_publication_alternatives VALUES(?,?,?,?)`, f.HouseholdID, publication, id, x.ID)
						if e != nil {
							return e
						}
						pending = true
					}
				}

				if prev != body || previousExtraction != x.ID {
					n++
					_, e = tx.ExecContext(ctx, `INSERT INTO archive_observation_revisions VALUES(?,?,?,?,?, 'source')`, f.HouseholdID, id, n, x.ID, body)
					if e != nil {
						return e
					}
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO archive_publication_observations VALUES(?,?,?,?) ON CONFLICT(household_id,publication_id,observation_id) DO UPDATE SET revision=excluded.revision`, f.HouseholdID, publication, id, n)
				if e != nil {
					return e
				}
			}
		}
		_, e = tx.ExecContext(ctx, `UPDATE archive_documents SET active_publication_id=?,document_revision=? WHERE household_id=? AND id=?`, publication, newrev, f.HouseholdID, x.DocumentID)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE archive_extractions SET status='published' WHERE household_id=? AND id=?`, f.HouseholdID, x.ID)
		if e != nil {
			return e
		}
		if e = archiveDocumentChangedIn(ctx, tx, f.HouseholdID, x.DocumentID, newrev); e != nil {
			return e
		}
		result := json.RawMessage(entityJSON(map[string]any{"parse_status": candidate.Status, "publication_id": publication, "document_revision": newrev}))
		status := domain.ArchiveTaskSucceeded
		if pending {
			status = domain.ArchiveTaskWaitingInput
			x.DocumentRevision = newrev
			fresh, e := archiveEntitySnapshotIn(ctx, tx, f.HouseholdID, nil)
			if e != nil {
				return e
			}
			q, e := archiveCreateQuestionIn(ctx, tx, f, x, plan, fresh, candidate.Resolution)
			if e != nil {
				return e
			}
			t.ClarificationJSON = json.RawMessage(entityJSON(q))
		}
		if e = applyArchiveUpdate(t, domain.ArchiveTaskUpdate{ExpectedStateVersion: f.StateVersion, Stage: "published", StateJSON: t.StateJSON, ResultJSON: result}, status); e != nil {
			return e
		}
		out = t
		return archiveSave(ctx, tx, t, "published", now, "")
	})
	return out, e
}
func archiveDocumentChangedIn(ctx context.Context, tx *sql.Tx, h, doc string, revision int64) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO archive_outbox(household_id,id,kind,document_id,revision) VALUES(?,?,'document_changed',?,?)`, h, domain.NewID(), doc, revision)
	return e
}

// Current same-document links are reusable only when an active observation
// actually references that physical mention. An alias in another document or
// a needs_resolution link (including merge undo) is not such authority.
func archivePublicationSnapshotIn(ctx context.Context, tx *sql.Tx, in archive.ResolutionInput, probes map[string]IdentityProbe) (archive.CandidateSnapshot, error) {
	s, e := archiveEntitySnapshotIn(ctx, tx, in.HouseholdID, probes)
	if e != nil {
		return s, e
	}
	for _, m := range in.Mentions {
		found := false
		for _, b := range s.Bindings {
			if b.Scope == "document_mention" && b.ScopeKey == in.DocumentID+"#"+m.Ref {
				found = true
			}
		}
		if found {
			continue
		}
		var target string
		var version int64
		e = tx.QueryRowContext(ctx, `SELECT l.matched_entity_id,l.version FROM archive_entity_links l JOIN archive_documents d ON d.household_id=l.household_id AND d.id=l.document_id WHERE l.household_id=? AND l.document_id=? AND l.mention_ref=? AND l.status='linked' AND d.withdrawn=0 AND EXISTS(SELECT 1 FROM archive_publication_observations p JOIN archive_observation_revisions r ON r.household_id=p.household_id AND r.observation_id=p.observation_id AND r.revision=p.revision JOIN json_each(r.body_json,'$.Field.Subjects') owned WHERE p.household_id=d.household_id AND p.publication_id=d.active_publication_id AND owned.value=l.mention_ref AND EXISTS(SELECT 1 FROM json_each(r.body_json,'$.Entities') owners WHERE owners.value=l.matched_entity_id))`, in.HouseholdID, in.DocumentID, m.Ref).Scan(&target, &version)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return s, e
		}
		s.Bindings = append(s.Bindings, archive.ConfirmedBinding{Scope: "document_mention", ScopeKey: in.DocumentID + "#" + m.Ref, EntityID: target, ConfirmedBy: "active_publication", Version: version, Evidence: m.Evidence})
	}
	return s, nil
}

// Native slots use the same physical bijection rule as events. Reconcile all
// fields first so a split cannot overwrite the first matched old observation.
func archiveReconcileObservationSlotsIn(ctx context.Context, tx *sql.Tx, h, event string, fields []archive.PublicationField) error {
	rows, e := tx.QueryContext(ctx, `SELECT o.slot_key,r.body_json FROM archive_observations o JOIN archive_observation_revisions r ON r.household_id=o.household_id AND r.observation_id=o.id WHERE o.household_id=? AND o.event_id=? AND r.revision=(SELECT MAX(v.revision) FROM archive_observation_revisions v WHERE v.household_id=o.household_id AND v.observation_id=o.id)`, h, event)
	if e != nil {
		return e
	}
	old := map[string][]archive.PhysicalAnchor{}
	for rows.Next() {
		var key, raw string
		if e = rows.Scan(&key, &raw); e != nil {
			rows.Close()
			return e
		}
		var body struct{ Field archive.PublicationField }
		if e = json.Unmarshal([]byte(raw), &body); e != nil {
			rows.Close()
			return e
		}
		old[key] = body.Field.Anchors
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	matched := map[string]int{}
	for i := range fields {
		f := &fields[i]
		if len(f.Anchors) == 0 || f.Anchors[0].Locator.Page == nil {
			continue
		}
		keys := []string{}
		for key, a := range old {
			if archive.PhysicalAnchorsMatch(a, f.Anchors) {
				keys = append(keys, key)
			}
		}
		if len(keys) > 1 {
			f.RequiresReview = true
		}
		if len(keys) == 1 {
			key := keys[0]
			if prior, ok := matched[key]; ok {
				f.RequiresReview = true
				fields[prior].RequiresReview = true
			} else {
				f.SlotKey = key
				matched[key] = i
			}
		}
	}
	return nil
}
