package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
)

const maxEntityCandidates = 4096
const maxIdentityFacts = 1024

// IdentityExactVersion identifies exact protected identity comparison rules.
const IdentityExactVersion = "identity-exact/1"

// ArchiveEntities exposes authorized human reads and revisions only. Automatic
// ingestion uses package-private *In helpers in T09's fenced publication tx.
// There is intentionally no standalone CreateEntity/ApplyPlan service API.
type ArchiveEntities struct{ db *DB }

// ArchiveEntities returns the authorized entity repository.
func (d *DB) ArchiveEntities() *ArchiveEntities { return &ArchiveEntities{db: d} }
func (r *ArchiveEntities) authorize(ctx context.Context, tx *sql.Tx, a domain.IntegrationActor, h string, write bool) error {
	p, e := r.db.ArchiveTasks().authorizeArchiveClient(ctx, tx, a, h, "archive.read", time.Now())
	if e != nil {
		return e
	}
	if write && !p.Policy.AllowsArchive("archive.organize", h) {
		return domain.Errorf(domain.CodeForbidden, "archive permission denied")
	}
	return nil
}

// Candidates returns a bounded identity snapshot authorized for the integration actor.
func (r *ArchiveEntities) Candidates(ctx context.Context, a domain.IntegrationActor, h string) (archive.CandidateSnapshot, error) {
	var out archive.CandidateSnapshot
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		if e := r.authorize(ctx, tx, a, h, false); e != nil {
			return e
		}
		var e error
		out, e = archiveEntitySnapshotIn(ctx, tx, h, nil)
		return e
	})
	if e != nil {
		return archive.CandidateSnapshot{}, e
	}
	return out, nil
}

// Merge records an authorized, version-checked human identity merge.
func (r *ArchiveEntities) Merge(ctx context.Context, a domain.IntegrationActor, h, source, target string, sv, tv int64, reason string) (string, error) {
	var id string
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		if e := r.authorize(ctx, tx, a, h, true); e != nil {
			return e
		}
		var e error
		id, e = archiveMergeEntitiesIn(ctx, tx, h, source, target, sv, tv, a.PrincipalID, reason)
		return e
	})
	if e != nil {
		return "", e
	}
	return id, nil
}

// UndoMerge reverses a version-checked human merge while retaining its audit history.
func (r *ArchiveEntities) UndoMerge(ctx context.Context, a domain.IntegrationActor, h, id string, version int64, reason string) error {
	return r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		if e := r.authorize(ctx, tx, a, h, true); e != nil {
			return e
		}
		return archiveUndoEntityMergeIn(ctx, tx, h, id, version, a.PrincipalID, reason)
	})
}

// ArchiveEntityProjection contains visible names and identity metadata for an entity.
type ArchiveEntityProjection struct {
	ID, Kind       string
	Names, Aliases []string
	Birth          archive.IdentityDate
}

// Public returns authorized public entity projections.
func (r *ArchiveEntities) Public(ctx context.Context, a domain.IntegrationActor, h string) ([]ArchiveEntityProjection, error) {
	var out []ArchiveEntityProjection
	e := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		if e := r.authorize(ctx, tx, a, h, false); e != nil {
			return e
		}
		var e error
		out, e = archiveEntityPublicIn(ctx, tx, h)
		return e
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

func archiveResolutionRevisionIn(ctx context.Context, tx *sql.Tx, h string) (int64, error) {
	var n int64
	e := tx.QueryRowContext(ctx, `SELECT revision FROM archive_resolution_revisions WHERE household_id=?`, h).Scan(&n)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	return n, e
}
func archiveCanonicalIn(ctx context.Context, tx *sql.Tx, h, id string) (string, error) {
	var exists int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_entities WHERE household_id=? AND id=?`, h, id).Scan(&exists); e != nil {
		return "", e
	}
	if exists != 1 {
		return "", domain.ErrNotFound
	}
	seen := map[string]bool{}
	for i := 0; i <= maxEntityCandidates; i++ {
		if seen[id] {
			return "", domain.ErrConflict
		}
		seen[id] = true
		var next string
		e := tx.QueryRowContext(ctx, `SELECT target_id FROM archive_entity_redirects WHERE household_id=? AND source_id=?`, h, id).Scan(&next)
		if errors.Is(e, sql.ErrNoRows) {
			return id, nil
		}
		if e != nil {
			return "", e
		}
		id = next
	}
	return "", domain.ErrConflict
}
func archiveEntityVersionIn(ctx context.Context, tx *sql.Tx, h, id string) (string, int64, error) {
	var k string
	var v int64
	e := tx.QueryRowContext(ctx, `SELECT kind,version FROM archive_entities WHERE household_id=? AND id=?`, h, id).Scan(&k, &v)
	if errors.Is(e, sql.ErrNoRows) {
		return "", 0, domain.ErrNotFound
	}
	return k, v, e
}
func archiveEntityTouchIn(ctx context.Context, tx *sql.Tx, h, id string) error {
	_, e := tx.ExecContext(ctx, `UPDATE archive_entities SET version=version+1 WHERE household_id=? AND id=?`, h, id)
	return e
}

type entityAttribute struct {
	ID, EntityID, Kind, Value, Precision string
	DocumentID, LegacyID                 sql.NullString
	Evidence                             []string
	Visible, Conflict                    bool
}

func archiveAttributesIn(ctx context.Context, tx *sql.Tx, h string, public bool) ([]entityAttribute, bool, error) {
	q := `SELECT a.id,a.entity_id,a.kind,a.value,a.precision,a.document_id,a.legacy_member_id,a.evidence_json,a.tv_visible,a.conflict FROM archive_entity_attributes a WHERE a.household_id=? AND (a.link_id IS NULL OR EXISTS(SELECT 1 FROM archive_entity_links l WHERE l.household_id=a.household_id AND l.id=a.link_id AND l.status='linked'))`
	if public {
		q += ` AND a.tv_visible=1 AND (EXISTS(SELECT 1 FROM archive_documents d WHERE d.household_id=a.household_id AND d.id=a.document_id AND d.tv_visible=1 AND d.original_restricted=0 AND d.withdrawn=0))`
	}
	q += ` ORDER BY a.id LIMIT 16385`
	rows, e := tx.QueryContext(ctx, q, h)
	if e != nil {
		return nil, false, e
	}
	defer func() { _ = rows.Close() }() // Read-only query cursor cleanup.
	out := []entityAttribute{}
	for rows.Next() {
		var a entityAttribute
		var evidence string
		if e = rows.Scan(&a.ID, &a.EntityID, &a.Kind, &a.Value, &a.Precision, &a.DocumentID, &a.LegacyID, &evidence, &a.Visible, &a.Conflict); e != nil {
			return nil, false, e
		}
		if e = json.Unmarshal([]byte(evidence), &a.Evidence); e != nil {
			return nil, false, e
		}
		out = append(out, a)
	}
	if e = rows.Err(); e != nil {
		return nil, false, e
	}
	if len(out) > 16384 {
		return nil, false, nil
	}
	return out, true, nil
}

// Candidate snapshots and their revision are read in one transaction. An
// overflow returns Complete=false, never a fabricated complete empty family.
func archiveEntitySnapshotIn(ctx context.Context, tx *sql.Tx, h string, probes map[string]IdentityProbe) (archive.CandidateSnapshot, error) {
	s := archive.CandidateSnapshot{HouseholdID: h, Complete: true, Strong: map[string]archive.StrongIdentityMatch{}}
	var e error
	s.Revision, e = archiveResolutionRevisionIn(ctx, tx, h)
	if e != nil {
		return s, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,kind,version FROM archive_entities WHERE household_id=? ORDER BY id LIMIT 4097`, h)
	if e != nil {
		return s, e
	}
	all := []archive.EntityCandidate{}
	for rows.Next() {
		var c archive.EntityCandidate
		if e = rows.Scan(&c.ID, &c.Kind, &c.Version); e != nil {
			_ = rows.Close() // Read-only query cursor cleanup.
			return s, e
		}
		all = append(all, c)
	}
	e = rows.Err()
	_ = rows.Close() // Read-only query cursor cleanup.
	if e != nil {
		return s, e
	}
	if len(all) > maxEntityCandidates {
		s.Complete = false
		return s, nil
	}
	canonical := map[string]string{}
	index := map[string]int{}
	for _, c := range all {
		root, e := archiveCanonicalIn(ctx, tx, h, c.ID)
		if e != nil {
			return s, e
		}
		canonical[c.ID] = root
		if root == c.ID {
			index[root] = len(s.Candidates)
			s.Candidates = append(s.Candidates, c)
		}
	}
	attrs, complete, e := archiveAttributesIn(ctx, tx, h, false)
	if e != nil {
		return s, e
	}
	s.Complete = complete
	for _, a := range attrs {
		c := &s.Candidates[index[canonical[a.EntityID]]]
		if a.Kind == "birth" {
			d := archive.IdentityDate{Precision: a.Precision, Value: a.Value, Evidence: a.Evidence, Conflict: a.Conflict}
			c.Birth = archive.CombineIdentityDates(c.Birth, d)
		} else {
			c.Names = append(c.Names, archive.CandidateName{Value: a.Value, Alias: a.Kind == "display_alias", Evidence: a.Evidence})
			c.Conflict = c.Conflict || a.Conflict
		}
	}
	rows, e = tx.QueryContext(ctx, `SELECT scope,scope_key,entity_id,confirmed_by,version,evidence_json,conflict FROM archive_entity_bindings WHERE household_id=? ORDER BY id LIMIT 4097`, h)
	if e != nil {
		return s, e
	}
	for rows.Next() {
		var b archive.ConfirmedBinding
		var evidence string
		if e = rows.Scan(&b.Scope, &b.ScopeKey, &b.EntityID, &b.ConfirmedBy, &b.Version, &evidence, &b.Conflict); e != nil {
			_ = rows.Close() // Read-only query cursor cleanup.
			return s, e
		}
		if e = json.Unmarshal([]byte(evidence), &b.Evidence); e != nil {
			_ = rows.Close() // Read-only query cursor cleanup.
			return s, e
		}
		b.EntityID = canonical[b.EntityID]
		s.Bindings = append(s.Bindings, b)
	}
	e = rows.Err()
	_ = rows.Close() // Read-only query cursor cleanup.
	if e != nil {
		return s, e
	}
	if len(s.Bindings) > maxEntityCandidates {
		s.Complete = false
		s.Bindings = nil
	}
	if len(probes) > 2048 {
		s.Complete = false
		return s, nil
	}
	for ref, p := range probes {
		r, e := archiveCompareIdentityIn(ctx, tx, h, p)
		if e != nil {
			return s, e
		}

		for other, q := range probes {
			if other == ref {
				continue
			}
			for _, left := range archiveIdentityParts(p) {
				for _, right := range archiveIdentityParts(q) {
					if supportedIdentity(left) && supportedIdentity(right) && left.Kind == right.Kind && left.Issuer == right.Issuer && left.RuleVersion == right.RuleVersion {
						if left.Value == right.Value {
							r.PeerRefs = appendUnique(r.PeerRefs, other)
						} else if exclusiveIdentity(left.Kind) {
							r.ConflictingPeerRefs = appendUnique(r.ConflictingPeerRefs, other)
						}
					}
				}
			}
		}
		sort.Strings(r.PeerRefs)
		sort.Strings(r.ConflictingPeerRefs)
		s.Strong[ref] = r
	}
	return s, nil
}
func archiveEntityPublicIn(ctx context.Context, tx *sql.Tx, h string) ([]ArchiveEntityProjection, error) {
	attrs, complete, e := archiveAttributesIn(ctx, tx, h, true)
	if e != nil {
		return nil, e
	}
	if !complete {
		return nil, domain.ErrConflict
	}
	items := map[string]*ArchiveEntityProjection{}
	for _, a := range attrs {
		id, e := archiveCanonicalIn(ctx, tx, h, a.EntityID)
		if e != nil {
			return nil, e
		}
		p := items[id]
		if p == nil {
			k, _, e := archiveEntityVersionIn(ctx, tx, h, id)
			if e != nil {
				return nil, e
			}
			p = &ArchiveEntityProjection{ID: id, Kind: k}
			items[id] = p
		}
		if a.Conflict && a.Kind != "birth" {
			continue
		}
		switch a.Kind {
		case "name", "full_address":
			p.Names = appendUnique(p.Names, a.Value)
		case "display_alias":
			p.Aliases = appendUnique(p.Aliases, a.Value)
		case "birth":
			p.Birth = archive.CombineIdentityDates(p.Birth, archive.IdentityDate{Value: a.Value, Precision: a.Precision, Conflict: a.Conflict})
		}
	}
	ids := []string{}
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []ArchiveEntityProjection{}
	for _, id := range ids {
		p := items[id]
		if p.Birth.Conflict {
			p.Birth = archive.IdentityDate{Conflict: true}
		}
		if len(p.Names) == 0 {
			continue
		}
		sort.Strings(p.Names)
		sort.Strings(p.Aliases)
		out = append(out, *p)
	}
	return out, nil
}
func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}
func entityJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func archiveEntityHistoryIn(ctx context.Context, tx *sql.Tx, h, id, action, actor, reason string, before, after any) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO archive_entity_history(household_id,id,object_id,action,actor,reason,before_json,after_json) VALUES(?,?,?,?,?,?,?,?)`, h, domain.NewID(), id, action, actor, reason, entityJSON(before), entityJSON(after))
	return e
}
func archiveEntityCreateIn(ctx context.Context, tx *sql.Tx, h, kind string) (string, error) {
	id := domain.NewID()
	_, e := tx.ExecContext(ctx, `INSERT INTO archive_entities(household_id,id,kind,version) VALUES(?,?,?,1)`, h, id, kind)
	return id, e
}
func archiveAttributePutIn(ctx context.Context, tx *sql.Tx, h, id, kind, value, precision, doc, legacy string, evidence []string, visible, conflict bool, linkIDs ...string) error {
	if strings.TrimSpace(value) == "" || len(value) > 2048 || len(evidence) == 0 {
		return domain.ErrConflict
	}
	var link any
	var d, l any
	if doc != "" {
		d = doc
		requested := ""
		if len(linkIDs) > 0 {
			requested = linkIDs[0]
		}
		resolved, e := archiveEvidenceLinkIn(ctx, tx, h, doc, id, requested)
		if e != nil {
			return e
		}
		link = resolved
	}
	if legacy != "" {
		l = legacy
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO archive_entity_attributes(household_id,id,entity_id,kind,value,precision,document_id,legacy_member_id,evidence_json,tv_visible,conflict,link_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, h, domain.NewID(), id, kind, value, precision, d, l, entityJSON(evidence), visible, conflict, link)
	if e != nil {
		return e
	}
	return archiveEntityTouchIn(ctx, tx, h, id)
}

// archiveApplyResolutionIn verifies the full plan against a fresh snapshot and
// dependencies BEFORE writing. T09 must call it after its lease, delegation,
// document/extraction and state fences, within its fact/checkpoint/outbox tx.
// Returned IDs are provisional until that owning transaction commits.
func archiveApplyResolutionIn(ctx context.Context, tx *sql.Tx, in archive.ResolutionInput, p archive.ResolutionPlan, identityProbes ...map[string]IdentityProbe) (map[string]string, error) {
	var probes map[string]IdentityProbe
	if len(identityProbes) > 0 {
		probes = identityProbes[0]
	}
	s, e := archivePublicationSnapshotIn(ctx, tx, in, probes)
	if e != nil {
		return nil, e
	}
	if p.HouseholdID != in.HouseholdID || p.DocumentID != in.DocumentID || s.Revision != p.Revision {
		return nil, domain.ErrConflict
	}
	fresh, e := archive.ResolveEntities(in, s)
	if e != nil {
		return nil, e
	}
	if !reflect.DeepEqual(fresh, p) {
		return nil, domain.ErrConflict
	}
	var visible bool
	e = tx.QueryRowContext(ctx, `SELECT tv_visible FROM archive_documents WHERE household_id=? AND id=?`, in.HouseholdID, in.DocumentID).Scan(&visible)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	refs := map[string]string{}
	out := map[string]string{}
	mentions := map[string]archive.ResolutionMention{}
	for _, m := range in.Mentions {
		mentions[m.Ref] = m
	}
	for _, d := range p.Decisions {
		if d.Action == "household_document" {
			continue
		}
		m := mentions[d.MentionRef]
		id := d.EntityID
		if d.Action == "create_entity" {
			id, e = archiveEntityCreateIn(ctx, tx, in.HouseholdID, m.Kind)
			if e != nil {
				return nil, e
			}
			refs[d.PlanRef] = id
		} else if d.PlanRef != "" {
			id = refs[d.PlanRef]
			if id == "" {
				return nil, domain.ErrConflict
			}
		}
		status := "linked"
		var matched any = id
		if d.Action == "needs_resolution" {
			status = "needs_resolution"
			matched = nil
		}
		var existingID, existingStatus string
		var existingTarget sql.NullString
		priorErr := tx.QueryRowContext(ctx, `SELECT id,status,matched_entity_id FROM archive_entity_links WHERE household_id=? AND document_id=? AND mention_ref=?`, in.HouseholdID, in.DocumentID, m.Ref).Scan(&existingID, &existingStatus, &existingTarget)
		if priorErr == nil {
			if d.Action == "needs_resolution" {
				continue
			}
			if existingStatus == status && existingTarget.String == id {
				if id != "" {
					out[m.Ref] = id
				}
				continue
			}
			return nil, domain.ErrConflict
		}
		if !errors.Is(priorErr, sql.ErrNoRows) {
			return nil, priorErr
		}
		linkID := domain.NewID()
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_entity_links(household_id,id,document_id,mention_ref,role,matched_entity_id,status,version,created_revision) VALUES(?,?,?,?,?,?,?,1,?)`, in.HouseholdID, linkID, in.DocumentID, m.Ref, m.Role, matched, status, p.Revision)
		if e != nil {
			return nil, e
		}
		if d.Action == "link_existing" || d.Action == "create_entity" {
			k, _, e := archiveEntityVersionIn(ctx, tx, in.HouseholdID, id)
			if e != nil {
				return nil, e
			}
			if k != m.Kind {
				return nil, domain.ErrConflict
			}
			nameKind := "name"
			if d.Action == "link_existing" {
				// A scoped correction can add a display alias, never a future
				// household-wide matching rule for that spelling.
				nameKind = "display_alias"
				for _, c := range s.Candidates {
					if c.ID == id {
						for _, n := range c.Names {
							if !n.Alias && archive.NameKey(n.Value) == archive.NameKey(m.Name) {
								nameKind = "name"
							}
						}
					}
				}
				if d.PlanRef != "" {
					for _, prior := range p.Decisions {
						if prior.Action == "create_entity" && prior.PlanRef == d.PlanRef && archive.NameKey(mentions[prior.MentionRef].Name) == archive.NameKey(m.Name) {
							nameKind = "name"
						}
					}
				}
			}
			if m.Kind == "property" && nameKind == "name" {
				nameKind = "full_address"
			}
			if e = archiveAttributePutIn(ctx, tx, in.HouseholdID, id, nameKind, m.Name, "", in.DocumentID, "", m.Evidence, visible, false, linkID); e != nil {
				return nil, e
			}
			if m.Birth.Value != "" {
				if e = archiveAttributePutIn(ctx, tx, in.HouseholdID, id, "birth", m.Birth.Value, m.Birth.Precision, in.DocumentID, "", m.Birth.Evidence, visible, m.Birth.Conflict, linkID); e != nil {
					return nil, e
				}
			}
			out[m.Ref] = id
		}

		if matched != nil {
			if e = archiveLinkMergeContextIn(ctx, tx, in.HouseholdID, linkID, id); e != nil {
				return nil, e
			}
		}
		if e = archiveEntityHistoryIn(ctx, tx, in.HouseholdID, linkID, "resolution", "core", d.Reason, nil, d); e != nil {
			return nil, e
		}
	}
	return out, nil
}
func archiveLinkMergeContextIn(ctx context.Context, tx *sql.Tx, h, link, id string) error {
	contexts, e := archiveActiveMergeContextIn(ctx, tx, h, id)
	if e != nil {
		return e
	}
	for _, merge := range contexts {
		if _, e = tx.ExecContext(ctx, `INSERT INTO archive_entity_link_merge_context VALUES(?,?,?)`, h, link, merge); e != nil {
			return e
		}
	}
	return nil
}

// Context snapshots all active merges of the target's current component.
// It must be taken at the moment of each new link or binding confirmation.
func archiveActiveMergeContextIn(ctx context.Context, tx *sql.Tx, h, id string) ([]string, error) {
	canonical, e := archiveCanonicalIn(ctx, tx, h, id)
	if e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,target_id FROM archive_entity_merges WHERE household_id=? AND active=1 ORDER BY id`, h)
	if e != nil {
		return nil, e
	}
	type merge struct{ id, target string }
	ms := []merge{}
	for rows.Next() {
		var m merge
		if e = rows.Scan(&m.id, &m.target); e != nil {
			_ = rows.Close() // Read-only query cursor cleanup.
			return nil, e
		}
		ms = append(ms, m)
	}
	e = rows.Err()
	_ = rows.Close() // Read-only query cursor cleanup.
	if e != nil {
		return nil, e
	}
	out := []string{}
	for _, m := range ms {
		root, e := archiveCanonicalIn(ctx, tx, h, m.target)
		if e != nil {
			return nil, e
		}
		if root == canonical {
			out = append(out, m.id)
		}
	}
	return out, nil
}

func archiveMergeEntitiesIn(ctx context.Context, tx *sql.Tx, h, source, target string, sv, tv int64, actor, reason string) (string, error) {
	if source == target || strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return "", domain.ErrConflict
	}
	sk, v, e := archiveEntityVersionIn(ctx, tx, h, source)
	if e != nil {
		return "", e
	}
	tk, w, e := archiveEntityVersionIn(ctx, tx, h, target)
	if e != nil {
		return "", e
	}
	if sk != tk || v != sv || w != tv {
		return "", domain.ErrConflict
	}
	a, e := archiveCanonicalIn(ctx, tx, h, source)
	if e != nil {
		return "", e
	}
	b, e := archiveCanonicalIn(ctx, tx, h, target)
	if e != nil {
		return "", e
	}
	if a != source || b != target || a == b {
		return "", domain.ErrConflict
	}
	revision, e := archiveResolutionRevisionIn(ctx, tx, h)
	if e != nil {
		return "", e
	}
	id := domain.NewID()
	_, e = tx.ExecContext(ctx, `INSERT INTO archive_entity_merges(household_id,id,source_id,target_id,version,active,actor,reason,created_revision) VALUES(?,?,?,?,1,1,?,?,?)`, h, id, source, target, actor, reason, revision)
	if e != nil {
		return "", e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO archive_entity_redirects VALUES(?,?,?,?)`, h, source, target, id)
	if e != nil {
		return "", e
	}
	if e = archiveEntityTouchIn(ctx, tx, h, source); e != nil {
		return "", e
	}
	if e = archiveEntityTouchIn(ctx, tx, h, target); e != nil {
		return "", e
	}
	e = archiveEntityHistoryIn(ctx, tx, h, id, "merge", actor, reason, nil, map[string]any{"source": source, "target": target, "version": 1})
	return id, e
}
func archiveUndoEntityMergeIn(ctx context.Context, tx *sql.Tx, h, id string, version int64, actor, reason string) error {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return domain.ErrConflict
	}
	var source, target string
	var v int64
	var active bool
	e := tx.QueryRowContext(ctx, `SELECT source_id,target_id,version,active FROM archive_entity_merges WHERE household_id=? AND id=?`, h, id).Scan(&source, &target, &v, &active)
	if errors.Is(e, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if e != nil {
		return e
	}
	if !active || v != version {
		return domain.ErrConflict
	}
	if e = archiveInvalidateMergeBindingsIn(ctx, tx, h, id, actor, reason); e != nil {
		return e
	}
	// Preserve every historical matched ID; original links and bindings resolve
	// through the restored redirect graph. New ambiguous links need clarification.
	_, e = tx.ExecContext(ctx, `UPDATE archive_entity_links SET status='needs_resolution',version=version+1 WHERE household_id=? AND id IN (SELECT link_id FROM archive_entity_link_merge_context WHERE household_id=? AND merge_id=?)`, h, h, id)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `DELETE FROM archive_entity_redirects WHERE household_id=? AND merge_id=?`, h, id)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE archive_entity_merges SET active=0,version=version+1 WHERE household_id=? AND id=?`, h, id)
	if e != nil {
		return e
	}
	if e = archiveEntityTouchIn(ctx, tx, h, source); e != nil {
		return e
	}
	if e = archiveEntityTouchIn(ctx, tx, h, target); e != nil {
		return e
	}
	return archiveEntityHistoryIn(ctx, tx, h, id, "undo_merge", actor, reason, map[string]any{"source": source, "target": target, "version": v}, map[string]any{"version": v + 1, "active": false})
}

// Bindings are scoped assertions. A document correction never creates a
// household-wide alias rule. Legacy assertions are reserved for the importer.
func archiveBindingPutIn(ctx context.Context, tx *sql.Tx, h, scope, key, id, actor string, evidence []string, expected int64) error {
	if (scope != "legacy_member" && scope != "document_mention") || strings.TrimSpace(key) == "" || actor == "" || len(evidence) == 0 {
		return domain.ErrConflict
	}
	if _, _, e := archiveEntityVersionIn(ctx, tx, h, id); e != nil {
		return e
	}
	var oldID, oldTarget, oldActor, oldEvidence string
	var v, oldConfirmationVersion int64
	var oldConflict bool
	e := tx.QueryRowContext(ctx, `SELECT id,entity_id,confirmed_by,version,evidence_json,confirmation_version,conflict FROM archive_entity_bindings WHERE household_id=? AND scope=? AND scope_key=?`, h, scope, key).Scan(&oldID, &oldTarget, &oldActor, &v, &oldEvidence, &oldConfirmationVersion, &oldConflict)
	if errors.Is(e, sql.ErrNoRows) {
		if expected != 0 {
			return domain.ErrConflict
		}
		oldID = domain.NewID()
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_entity_bindings(household_id,id,scope,scope_key,entity_id,confirmed_by,version,evidence_json) VALUES(?,?,?,?,?,?,1,?)`, h, oldID, scope, key, id, actor, entityJSON(evidence))
	} else if e == nil {
		if v != expected {
			return domain.ErrConflict
		}
		_, e = tx.ExecContext(ctx, `UPDATE archive_entity_bindings SET entity_id=?,confirmed_by=?,version=version+1,confirmation_version=version+1,evidence_json=?,conflict=0 WHERE household_id=? AND id=?`, id, actor, entityJSON(evidence), h, oldID)
	}
	if e != nil {
		return e
	}
	if e = archiveEntityTouchIn(ctx, tx, h, id); e != nil {
		return e
	}
	contexts, e := archiveActiveMergeContextIn(ctx, tx, h, id)
	if e != nil {
		return e
	}
	for _, merge := range contexts {
		if _, e = tx.ExecContext(ctx, `INSERT INTO archive_entity_binding_merge_context VALUES(?,?,?,?)`, h, oldID, v+1, merge); e != nil {
			return e
		}
	}
	return archiveEntityHistoryIn(ctx, tx, h, oldID, "binding", actor, "scoped_confirmation", map[string]any{"target": oldTarget, "version": v, "actor": oldActor, "evidence": oldEvidence, "confirmation_version": oldConfirmationVersion, "conflict": oldConflict}, map[string]any{"target": id, "version": v + 1, "confirmation_version": v + 1, "scope": scope, "key": key, "evidence": evidence, "merge_context": contexts, "conflict": false})
}

// ConfirmDocumentMention binds a version-checked document mention to a human-selected entity.
func (r *ArchiveEntities) ConfirmDocumentMention(ctx context.Context, a domain.IntegrationActor, h, doc, mention, target string, expected int64, evidence []string) error {
	return r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		if e := r.authorize(ctx, tx, a, h, true); e != nil {
			return e
		}
		var exists int
		if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_entity_links WHERE household_id=? AND document_id=? AND mention_ref=?`, h, doc, mention).Scan(&exists); e != nil {
			return e
		}
		if exists != 1 {
			return domain.ErrNotFound
		}
		return archiveBindingPutIn(ctx, tx, h, "document_mention", doc+"#"+mention, target, a.PrincipalID, evidence, expected)
	})
}

// legacy family_members is global and belongs ONLY to the configured local
// household. Arbitrary caller-supplied households cannot import its identities.
func archiveLegacyEntityIn(ctx context.Context, tx *sql.Tx, h, member string) (string, error) {
	var local string
	if e := tx.QueryRowContext(ctx, `SELECT household_id FROM local_household WHERE singleton=1`).Scan(&local); e != nil {
		return "", e
	}
	if local != h {
		return "", domain.ErrNotFound
	}
	var id string
	e := tx.QueryRowContext(ctx, `SELECT entity_id FROM archive_legacy_entities WHERE household_id=? AND legacy_member_id=?`, h, member).Scan(&id)
	if e == nil {
		return id, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	var name string
	e = tx.QueryRowContext(ctx, `SELECT name FROM family_members WHERE id=?`, member).Scan(&name)
	if errors.Is(e, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if e != nil {
		return "", e
	}
	id, e = archiveEntityCreateIn(ctx, tx, h, "person")
	if e != nil {
		return "", e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO archive_legacy_entities VALUES(?,?,?)`, h, member, id)
	if e != nil {
		return "", e
	}
	if e = archiveAttributePutIn(ctx, tx, h, id, "name", name, "", "", member, []string{"legacy_member:" + member}, false, false); e != nil {
		return "", e
	}
	e = archiveBindingPutIn(ctx, tx, h, "legacy_member", member, id, "trusted_legacy_import", []string{"legacy_member:" + member}, 0)
	return id, e
}

// IdentityProbe values never leave store comparison code. These are trusted Core inputs, not
// model candidate DTOs; even accidental JSON/String formatting omits values.
type IdentityProbe struct {
	Additional                []IdentityProbe `json:"-"`
	Kind, Issuer, RuleVersion string
	Value                     string `json:"-"`
	Evidence                  []string
	Conflict                  bool
}

func (p IdentityProbe) String() string { return "[protected identity probe]" }

// ProtectedIdentityFact holds an evidence-backed protected value for internal comparisons.
type ProtectedIdentityFact struct {
	DocumentID, Kind, Issuer, RuleVersion, LinkID string
	Value                                         string `json:"-"`
	Evidence                                      []string
	Conflict                                      bool
}

func (p ProtectedIdentityFact) String() string { return "[protected identity fact]" }
func supportedIdentity(p IdentityProbe) bool {
	if p.RuleVersion != IdentityExactVersion || len(p.Issuer) < 2 || len(p.Issuer) > 128 || strings.TrimSpace(p.Issuer) != p.Issuer || len(p.Value) == 0 || len(p.Value) > 128 || strings.TrimSpace(p.Value) != p.Value || strings.ContainsRune(p.Value, 0) {
		return false
	}
	switch p.Kind {
	case "national_id", "passport", "birth_certificate":
		return true
	}
	return false
}
func archiveProtectedIdentityPutIn(ctx context.Context, tx *sql.Tx, h, id string, p ProtectedIdentityFact) error {
	if !supportedIdentity(IdentityProbe{Kind: p.Kind, Issuer: p.Issuer, RuleVersion: p.RuleVersion, Value: p.Value}) || len(p.Evidence) == 0 {
		return domain.ErrConflict
	}
	link, e := archiveEvidenceLinkIn(ctx, tx, h, p.DocumentID, id, p.LinkID)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO archive_protected_identity_facts(household_id,id,entity_id,document_id,kind,issuer,rule_version,value,evidence_json,conflict,link_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, h, domain.NewID(), id, p.DocumentID, p.Kind, p.Issuer, p.RuleVersion, p.Value, entityJSON(p.Evidence), p.Conflict, link)
	if e != nil {
		return e
	}
	return archiveEntityTouchIn(ctx, tx, h, id)
}
func archiveIdentityParts(p IdentityProbe) []IdentityProbe {
	if len(p.Additional) > 7 {
		return nil
	}
	parts := append([]IdentityProbe{p}, p.Additional...)
	for i := range parts {
		if i > 0 && len(parts[i].Additional) > 0 {
			return nil
		}
		parts[i].Additional = nil
	}
	return parts
}
func archiveCompareIdentityIn(ctx context.Context, tx *sql.Tx, h string, p IdentityProbe) (archive.StrongIdentityMatch, error) {
	parts := archiveIdentityParts(p)
	if len(parts) == 0 {
		return archive.StrongIdentityMatch{}, nil
	}
	combined := archive.StrongIdentityMatch{Supported: true, Complete: true}
	for i, part := range parts {
		r, e := archiveCompareSingleIdentityIn(ctx, tx, h, part)
		if e != nil {
			return combined, e
		}
		combined.Supported = combined.Supported && r.Supported
		combined.Complete = combined.Complete && r.Complete
		combined.Conflict = combined.Conflict || r.Conflict
		for _, id := range r.EntityIDs {
			combined.EntityIDs = appendUnique(combined.EntityIDs, id)
		}
		for _, id := range r.ConflictingEntityIDs {
			combined.ConflictingEntityIDs = appendUnique(combined.ConflictingEntityIDs, id)
		}
		combined.Evidence = append(combined.Evidence, r.Evidence...)
		for _, previous := range parts[:i] {
			if part.Kind == previous.Kind && part.Issuer == previous.Issuer && part.Value != previous.Value && exclusiveIdentity(part.Kind) {
				combined.Conflict = true
			}
		}
	}
	sort.Strings(combined.EntityIDs)
	sort.Strings(combined.ConflictingEntityIDs)
	sort.Strings(combined.Evidence)
	return combined, nil
}

func archiveCompareSingleIdentityIn(ctx context.Context, tx *sql.Tx, h string, p IdentityProbe) (archive.StrongIdentityMatch, error) {
	r := archive.StrongIdentityMatch{Evidence: append([]string(nil), p.Evidence...)}
	if !supportedIdentity(p) {
		return r, nil
	}
	r.Supported = true
	r.Complete = true
	r.Conflict = p.Conflict
	// Bounded full-value comparison within the household/type/issuer partition.
	// No digest/index ever encodes the ID. Rule mismatches are unsupported input,
	// not proof of no match, including data restored from a future rule version.
	rows, e := tx.QueryContext(ctx, `SELECT entity_id,rule_version,value,evidence_json,conflict FROM archive_protected_identity_facts f WHERE household_id=? AND kind=? AND issuer=? AND EXISTS(SELECT 1 FROM archive_entity_links l WHERE l.household_id=f.household_id AND l.id=f.link_id AND l.status='linked') ORDER BY id LIMIT 1025`, h, p.Kind, p.Issuer)
	if e != nil {
		return r, e
	}
	type match struct {
		id              string
		evidence        []string
		conflict, equal bool
	}
	matches := []match{}
	count := 0
	for rows.Next() {
		var id, rule, value, evidence string
		var conflict bool
		if e = rows.Scan(&id, &rule, &value, &evidence, &conflict); e != nil {
			_ = rows.Close() // Read-only query cursor cleanup.
			return r, e
		}
		count++
		if count > maxIdentityFacts {
			r.Complete = false
			break
		}
		if rule != p.RuleVersion {
			r.Complete = false
			r.Supported = false
			continue
		}
		// National IDs and birth-certificate IDs are single-valued within
		// an issuer under this rule. Passport renewals may have new numbers;
		// unequal passports are not treated as negative identity proof.
		if value == p.Value || exclusiveIdentity(p.Kind) {
			var ev []string
			if e = json.Unmarshal([]byte(evidence), &ev); e != nil {
				_ = rows.Close() // Read-only query cursor cleanup.
				return r, e
			}
			matches = append(matches, match{id, ev, conflict, value == p.Value})
		}
	}
	e = rows.Err()
	_ = rows.Close() // Read-only query cursor cleanup.
	if e != nil {
		return r, e
	}
	if !r.Complete {
		return r, nil
	}
	for _, m := range matches {
		id, e := archiveCanonicalIn(ctx, tx, h, m.id)
		if e != nil {
			return r, e
		}
		if m.equal {
			r.EntityIDs = appendUnique(r.EntityIDs, id)
			r.Conflict = r.Conflict || m.conflict
		} else {
			r.ConflictingEntityIDs = appendUnique(r.ConflictingEntityIDs, id)
		}
		r.Evidence = append(r.Evidence, m.evidence...)
	}
	sort.Strings(r.EntityIDs)
	sort.Strings(r.ConflictingEntityIDs)
	sort.Strings(r.Evidence)
	return r, nil
}

// ArchiveEntityLink retains MatchedEntityID as immutable history; CanonicalEntityID is a read-time view.
// Unresolved links deliberately have no canonical identity for fact consumers.
type ArchiveEntityLink struct {
	ID, DocumentID, MentionRef, Role, MatchedEntityID, CanonicalEntityID, Status string
	Version                                                                      int64
}

func archiveEntityLinksIn(ctx context.Context, tx *sql.Tx, h, doc string) ([]ArchiveEntityLink, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id,document_id,mention_ref,role,coalesce(matched_entity_id,''),status,version FROM archive_entity_links WHERE household_id=? AND document_id=? ORDER BY mention_ref LIMIT 2049`, h, doc)
	if e != nil {
		return nil, e
	}
	out := []ArchiveEntityLink{}
	for rows.Next() {
		var l ArchiveEntityLink
		if e = rows.Scan(&l.ID, &l.DocumentID, &l.MentionRef, &l.Role, &l.MatchedEntityID, &l.Status, &l.Version); e != nil {
			_ = rows.Close() // Read-only query cursor cleanup.
			return nil, e
		}
		out = append(out, l)
	}
	e = rows.Err()
	_ = rows.Close() // Read-only query cursor cleanup.
	if e != nil {
		return nil, e
	}
	if len(out) > 2048 {
		return nil, domain.ErrConflict
	}
	for i := range out {
		if out[i].Status == "linked" {
			out[i].CanonicalEntityID, e = archiveCanonicalIn(ctx, tx, h, out[i].MatchedEntityID)
			if e != nil {
				return nil, e
			}
		}
	}
	return out, nil
}

// Evidence is attached to one exact mention link, not a document-wide guess.
// Implicit selection is allowed only for an unambiguous already-linked source.
func archiveEvidenceLinkIn(ctx context.Context, tx *sql.Tx, h, doc, entity, requested string) (string, error) {
	q := `SELECT id FROM archive_entity_links WHERE household_id=? AND document_id=? AND matched_entity_id=? AND status='linked'`
	args := []any{h, doc, entity}
	if requested != "" {
		q += ` AND id=?`
		args = append(args, requested)
	}
	q += ` LIMIT 2`
	rows, e := tx.QueryContext(ctx, q, args...)
	if e != nil {
		return "", e
	}
	defer func() { _ = rows.Close() }() // Read-only query cursor cleanup.
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return "", e
		}
		ids = append(ids, id)
	}
	if e = rows.Err(); e != nil {
		return "", e
	}
	if len(ids) != 1 {
		return "", domain.ErrConflict
	}
	return ids[0], nil
}

func archiveInvalidateMergeBindingsIn(ctx context.Context, tx *sql.Tx, h, merge, actor, reason string) error {
	rows, e := tx.QueryContext(ctx, `SELECT b.id,b.version,b.entity_id FROM archive_entity_bindings b WHERE b.household_id=? AND EXISTS(SELECT 1 FROM archive_entity_binding_merge_context c WHERE c.household_id=b.household_id AND c.binding_id=b.id AND c.confirmation_version=b.confirmation_version AND c.merge_id=?)`, h, merge)
	if e != nil {
		return e
	}
	type binding struct {
		id, target string
		version    int64
	}
	bs := []binding{}
	for rows.Next() {
		var b binding
		if e = rows.Scan(&b.id, &b.version, &b.target); e != nil {
			_ = rows.Close() // Read-only query cursor cleanup.
			return e
		}
		bs = append(bs, b)
	}
	e = rows.Err()
	_ = rows.Close() // Read-only query cursor cleanup.
	if e != nil {
		return e
	}
	for _, b := range bs {
		if _, e = tx.ExecContext(ctx, `UPDATE archive_entity_bindings SET conflict=1,version=version+1 WHERE household_id=? AND id=?`, h, b.id); e != nil {
			return e
		}
		if e = archiveEntityHistoryIn(ctx, tx, h, b.id, "undo_binding_context", actor, reason, map[string]any{"target": b.target, "version": b.version}, map[string]any{"version": b.version + 1, "conflict": true, "merge": merge}); e != nil {
			return e
		}
	}
	return nil
}

func exclusiveIdentity(kind string) bool { return kind == "national_id" || kind == "birth_certificate" }
