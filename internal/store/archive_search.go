package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
)

// InReadTx uses modernc's read-only BEGIN path, which deliberately bypasses the
// connection's default immediate writer lock. Authorization is read inside it.
func (d *DB) InReadTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, e := d.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}

type searchProjection struct {
	Item archive.SearchItem `json:"item"`
	Text string             `json:"text"`
}

func metadataSearchItem(id string, rev int64) archive.SearchItem {
	return archive.SearchItem{DocumentID: id, DocumentRevision: rev, Domain: "unknown", DocumentType: "unknown", Ownership: "unassigned", ParseStatus: "pending", EntityIDs: []string{}, Dates: []string{}, Evidence: []archive.SearchEvidence{}, MetadataOnly: true}
}
func searchBound(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// Search rows are disposable projections. A household epoch invalidates every
// dependency (including assertions carried by another document) atomically.
// Rebuilding writes bounded batches alongside the old generation; stale rows
// are never treated as current or counted as complete zero results.
func (r *ArchiveTasks) IndexArchives(ctx context.Context, h string, limit int) (int, error) {
	if limit < 1 || limit > 32 {
		return 0, domain.ErrConflict
	}
	type builtRow struct {
		id         string
		public     bool
		body, text string
	}
	built := []builtRow{}
	var epoch, generation int64
	count := 0
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// Compile outside the writer lock. Publication commits fence this work by
	// epoch, so a concurrent withdrawal/reassignment cannot publish stale rows.
	e := r.db.InReadTx(readCtx, func(tx *sql.Tx) error {
		if e := tx.QueryRowContext(readCtx, `SELECT epoch,generation FROM archive_search_state WHERE household_id=?`, h).Scan(&epoch, &generation); e != nil {
			return e
		}
		rows, e := tx.QueryContext(readCtx, `SELECT d.id FROM archive_documents d WHERE d.household_id=? AND (SELECT count(*) FROM archive_search_rows s WHERE s.household_id=d.household_id AND s.document_id=d.id AND s.epoch=? AND s.generation=?)<2 ORDER BY d.id LIMIT ?`, h, epoch, generation, limit)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		started := time.Now()
		for _, id := range ids {
			if count > 0 && time.Since(started) > 500*time.Millisecond {
				break
			}
			for _, public := range []bool{false, true} {
				p, e := r.searchProjectionIn(readCtx, tx, h, id, public)
				if e != nil {
					return e
				}
				body, e := json.Marshal(p.Item)
				if e != nil {
					return e
				}
				built = append(built, builtRow{id, public, string(body), p.Text})
			}
			count++
		}
		return nil
	})
	if e != nil {
		return 0, e
	}
	e = r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		var currentEpoch, currentGeneration int64
		if e := tx.QueryRowContext(ctx, `SELECT epoch,generation FROM archive_search_state WHERE household_id=?`, h).Scan(&currentEpoch, &currentGeneration); e != nil {
			return e
		}
		if currentEpoch != epoch || currentGeneration != generation {
			return domain.ErrConflict
		}
		for _, row := range built {
			if _, e := tx.ExecContext(ctx, `INSERT INTO archive_search_rows(household_id,document_id,public,epoch,generation,body,text) VALUES(?,?,?,?,?,?,?) ON CONFLICT(household_id,document_id,public) DO UPDATE SET epoch=excluded.epoch,generation=excluded.generation,body=excluded.body,text=excluded.text`, h, row.id, row.public, epoch, generation, row.body, row.text); e != nil {
				return e
			}
		}
		if count > 0 {
			if _, e := tx.ExecContext(ctx, `UPDATE archive_search_state SET index_revision=index_revision+1 WHERE household_id=?`, h); e != nil {
				return e
			}
		}
		_, e := tx.ExecContext(ctx, `UPDATE archive_outbox AS o SET status='completed' WHERE o.household_id=? AND o.kind='document_changed' AND EXISTS(SELECT 1 FROM archive_documents d WHERE d.household_id=o.household_id AND d.id=o.document_id AND d.document_revision>=o.revision AND (SELECT count(*) FROM archive_search_rows s WHERE s.household_id=d.household_id AND s.document_id=d.id AND s.epoch=? AND s.generation=?)=2)`, h, epoch, generation)
		return e
	})
	if e != nil {
		return 0, e
	}
	return count, nil
}
func (r *ArchiveTasks) RebuildArchiveSearch(ctx context.Context, h string) error {
	return r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE archive_search_state SET generation=generation+1 WHERE household_id=?`, h)
		return e
	})
}
func searchBundleIn(ctx context.Context, tx *sql.Tx, h, x string) (ArchivePublicationBundle, error) {
	var b ArchivePublicationBundle
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT p.body FROM archive_extraction_products b JOIN archive_products p ON p.household_id=b.household_id AND p.task_id=b.task_id AND p.product_key=b.product_key AND p.fingerprint=b.fingerprint AND p.digest=b.digest WHERE b.household_id=? AND b.extraction_id=? AND b.product_key='publication' AND length(p.body)<=33554432`, h, x).Scan(&raw)
	if e != nil {
		return b, e
	}
	e = json.Unmarshal(raw, &b)
	return b, e
}
func (r *ArchiveTasks) searchProjectionIn(ctx context.Context, tx *sql.Tx, h, id string, public bool) (searchProjection, error) {
	var p searchProjection
	var rev int64
	var pub sql.NullString
	var visible, withdrawn, restricted bool
	e := tx.QueryRowContext(ctx, `SELECT document_revision,active_publication_id,tv_visible,withdrawn,original_restricted FROM archive_documents WHERE household_id=? AND id=?`, h, id).Scan(&rev, &pub, &visible, &withdrawn, &restricted)
	if e != nil {
		return p, e
	}
	p.Item = metadataSearchItem(id, rev)
	var taskStatus sql.NullString
	if e = tx.QueryRowContext(ctx, `SELECT (SELECT t.status FROM archive_sources s JOIN archive_tasks t ON t.household_id=s.household_id AND t.id=s.task_id WHERE s.household_id=? AND s.document_id=? ORDER BY s.received_at DESC,s.task_id DESC LIMIT 1)`, h, id).Scan(&taskStatus); e != nil {
		return p, e
	}
	p.Item.TaskStatus = taskStatus.String
	if withdrawn || public && !visible {
		return p, nil
	}
	if !pub.Valid {
		return p, nil
	}
	var x, status string
	var pubrev int64
	if e = tx.QueryRowContext(ctx, `SELECT extraction_id,status,document_revision FROM archive_publications WHERE household_id=? AND id=?`, h, pub.String).Scan(&x, &status, &pubrev); e != nil {
		return p, e
	}
	p.Item.PublicationID = pub.String
	p.Item.ParseStatus = status
	p.Item.MetadataOnly = false
	b, e := searchBundleIn(ctx, tx, h, x)
	if e != nil {
		return p, e
	}
	p.Item.ReviewRequired = b.Understanding.RequiresReview || !b.Understanding.Complete || p.Item.TaskStatus == "waiting_input"
	// A classification has whole-document provenance. Regional privacy therefore
	// suppresses it in the public projection until narrower evidence is available.
	if !public || !restricted {
		for _, ch := range b.Understanding.Chunks {
			if p.Item.Domain == "unknown" {
				p.Item.Domain = ch.Result.Domain
			}
			if p.Item.DocumentType == "unknown" {
				p.Item.DocumentType = ch.Result.DocumentType
			}
		}
	}
	blocked := map[string][]archive.EvidenceReference{}
	secrets := []string{}
	addProtected := func(b ArchivePublicationBundle) {
		for _, ch := range b.Understanding.Chunks {
			for _, f := range ch.Result.Fields {
				if f.Name != "national_id" && f.Name != "passport" && f.Name != "birth_certificate" && f.Name != "identity_issuer" {
					continue
				}
				if f.Name != "identity_issuer" {
					secrets = append(secrets, f.Raw)
				}
				for _, ev := range f.Evidence {
					for _, c := range b.Plan.Chunks {
						for _, v := range append(append([]archive.EvidenceView{}, c.Owned...), c.Context...) {
							if v.ViewID == ev.ViewID {
								blocked[v.ParentSegmentID+":"+v.ComponentKind] = append(blocked[v.ParentSegmentID+":"+v.ComponentKind], ev)
							}
						}
					}
				}
			}
		}
	}

	blockedEvidence := func(ev archive.CanonicalEvidence) bool {
		for _, span := range blocked[ev.ParentSegmentID+":"+ev.ComponentKind] {
			if ev.Start < span.End && span.Start < ev.End {
				return true
			}
		}
		return false
	}

	addProtected(b)
	rows, e := tx.QueryContext(ctx, `SELECT value FROM archive_protected_identity_facts WHERE household_id=? AND document_id=? LIMIT 4097`, h, id)
	if e != nil {
		return p, e
	}
	for rows.Next() {
		var v string
		if e = rows.Scan(&v); e != nil {
			rows.Close()
			return p, e
		}
		secrets = append(secrets, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return p, e
	}
	safeText := func(s string) bool {
		for _, v := range secrets {
			if v != "" && strings.Contains(s, v) {
				return false
			}
		}
		return true
	}
	observations, e := archiveCurrentObservationsIn(ctx, tx, h, id, public)
	if e != nil {
		return p, e
	}
	bundles := map[string]ArchivePublicationBundle{x: b}
	text := []string{}
	seenFields := map[string]bool{}
	for _, o := range observations {
		var body struct {
			Field          archive.PublicationField
			Entities       []string
			AssertedValue  string
			ValueAssertion *ArchiveAssertionProvenance
		}
		if e = json.Unmarshal(o.Body, &body); e != nil {
			return p, e
		}
		f := body.Field
		seenFields[o.ExtractionID+":"+f.Ref] = true
		source, ok := bundles[o.ExtractionID]
		if !ok {
			source, e = searchBundleIn(ctx, tx, h, o.ExtractionID)
			if e != nil {
				return p, e
			}
			bundles[o.ExtractionID] = source
			addProtected(source)
		}
		if !safeText(f.Raw) || !safeText(f.Name) || !safeText(body.AssertedValue) {
			continue
		}
		for _, ev := range f.Evidence {
			if blockedEvidence(ev) || !safeText(ev.Quote) {
				continue
			}
			var locator archive.Locator
			for _, seg := range source.Parsed.Segments {
				if seg.ID == ev.ParentSegmentID {
					locator = seg.Locator
					break
				}
			}
			if locator.Kind == "" {
				return p, domain.ErrConflict
			}
			if len(p.Item.Evidence) >= 64 {
				p.Item.Truncated = true
				break
			}
			quote := searchBound(ev.Quote, 256)
			end := ev.Start + utf8.RuneCountInString(quote)
			sourceRev := pubrev
			if o.PublicationID != pub.String {
				if e = tx.QueryRowContext(ctx, `SELECT document_revision FROM archive_publications WHERE household_id=? AND id=?`, h, o.PublicationID).Scan(&sourceRev); e != nil {
					return p, e
				}
			}
			p.Item.Evidence = append(p.Item.Evidence, archive.SearchEvidence{FieldName: f.Name, DocumentID: o.SourceDocumentID, PublicationID: o.PublicationID, PublicationRevision: sourceRev, ObservationID: o.ID, ObservationRevision: o.Revision, SourceExtractionID: o.ExtractionID, SegmentID: ev.ParentSegmentID, ComponentID: ev.ComponentID, ComponentKind: ev.ComponentKind, AlternativeID: ev.AlternativeID, Locator: locator, Start: ev.Start, End: end, Excerpt: quote, Confirmed: true})
			if body.ValueAssertion != nil {
				last := &p.Item.Evidence[len(p.Item.Evidence)-1]
				last.AssertedValue = searchBound(body.AssertedValue, 256)
				last.AssertionID = body.ValueAssertion.ID
				last.SourceNormalizations = f.Normalizations
			}
			text = append(text, f.Name, quote, searchBound(body.AssertedValue, 256))
		}
		p.Item.EntityIDs = append(p.Item.EntityIDs, body.Entities...)
		for _, n := range f.Normalizations {
			if n.Kind != "date" && n.Kind != "date_partial" {
				continue
			}
			if body.ValueAssertion != nil {
				// A current assertion replaces the source normalization for filters.
				// Accept only explicit ISO precision; never guess missing components.
				if searchAssertionDate(body.AssertedValue) {
					p.Item.Dates = appendUnique(p.Item.Dates, body.AssertedValue)
				} else {
					p.Item.ReviewRequired = true
				}
			} else {
				p.Item.Dates = appendUnique(p.Item.Dates, n.Value)
			}
		}
	}

	// Unresolved fields are searchable source evidence, never confirmed facts or
	// entity/date filters. Only the active publication's own candidate is used.
	candidate, e := archive.CompilePublication(h, id, b.Parsed, b.Plan, b.Understanding)
	if e != nil {
		return p, e
	}
	private := []archive.PhysicalAnchor{}
	if public {
		private, e = archivePrivateAnchorsIn(ctx, tx, h, id)
		if e != nil {
			return p, e
		}
	}
	for _, event := range candidate.Events {
		for _, field := range event.Fields {
			if seenFields[x+":"+field.Ref] || field.Name == "national_id" || field.Name == "passport" || field.Name == "birth_certificate" || field.Name == "identity_issuer" {
				continue
			}
			hidden := false
			for _, secret := range private {
				for _, anchor := range field.VisibilityAnchors {
					hidden = hidden || !archiveAnchorsDisjoint(secret, anchor)
				}
			}
			if hidden || !safeText(field.Raw) || !safeText(field.Name) {
				continue
			}
			for _, ev := range field.Evidence {
				if blockedEvidence(ev) || !safeText(ev.Quote) {
					continue
				}
				var loc archive.Locator
				for _, seg := range b.Parsed.Segments {
					if seg.ID == ev.ParentSegmentID {
						loc = seg.Locator
						break
					}
				}
				if len(p.Item.Evidence) >= 64 {
					p.Item.Truncated = true
					break
				}
				quote := searchBound(ev.Quote, 256)
				p.Item.Evidence = append(p.Item.Evidence, archive.SearchEvidence{DocumentID: id, PublicationID: pub.String, PublicationRevision: pubrev, SourceExtractionID: x, SegmentID: ev.ParentSegmentID, ComponentID: ev.ComponentID, ComponentKind: ev.ComponentKind, AlternativeID: ev.AlternativeID, Locator: loc, Start: ev.Start, End: ev.Start + utf8.RuneCountInString(quote), Excerpt: quote, Confirmed: false})
				text = append(text, field.Name, quote)
			}
		}
	}

	// Only a validated, publication-bound understanding plan can expose original
	// text. Views intersecting known protected identity ranges or private source
	// regions are excluded; arbitrary parsed text is never a fallback index.
	for _, chunk := range b.Plan.Chunks {
		for _, v := range chunk.Owned {
			if v.Text == "" {
				continue
			}
			if !safeText(v.Text) {
				p.Item.Truncated = true
				continue
			}
			ce := archive.CanonicalEvidence{ParentSegmentID: v.ParentSegmentID, ComponentKind: v.ComponentKind, Start: v.Start, End: v.End}
			if blockedEvidence(ce) {
				p.Item.Truncated = true
				continue
			}
			anchor := archive.PhysicalAnchor{Locator: v.Structure.Locator, Component: v.ComponentKind, Start: v.Start, End: v.End}
			hidden := false
			for _, secret := range private {
				hidden = hidden || !archiveAnchorsDisjoint(secret, anchor)
			}
			if hidden {
				continue
			}
			runes := []rune(v.Text)
			for start := 0; start < len(runes); start += 256 {
				if len(p.Item.Evidence) >= 64 {
					p.Item.Truncated = true
					break
				}
				end := start + 512
				if end > len(runes) {
					end = len(runes)
				}
				quote := string(runes[start:end])
				text = append(text, quote)
				p.Item.Evidence = append(p.Item.Evidence, archive.SearchEvidence{DocumentID: id, PublicationID: pub.String, PublicationRevision: pubrev, SourceExtractionID: x, SourceKind: "original", SegmentID: v.ParentSegmentID, ComponentID: v.ComponentID, ComponentKind: v.ComponentKind, AlternativeID: v.AlternativeID, Locator: v.Structure.Locator, Start: v.Start + start, End: v.Start + end, Excerpt: quote, Confirmed: false})
			}
		}
	}
	p.Item.EntityIDs = searchUnique(p.Item.EntityIDs)
	sort.Strings(p.Item.Dates)
	var unresolved int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_entity_links WHERE household_id=? AND document_id=? AND status='needs_resolution'`, h, id).Scan(&unresolved); e != nil {
		return p, e
	}
	if unresolved > 0 {
		p.Item.Ownership = "waiting"
	} else if len(p.Item.EntityIDs) > 0 {
		p.Item.Ownership = "confirmed"
	} else {
		p.Item.Ownership = "unassigned"
		if len(candidate.Resolution.Mentions) == 0 && (!public || !restricted) {
			household := p.Item.DocumentType == "household_notice" || p.Item.DocumentType == "school_notice" || p.Item.DocumentType == "family_notice"
			for _, label := range b.Understanding.Labels {
				household = household || label.Label == "household"
			}
			if household {
				p.Item.Ownership = "household"
			}
		}
	}
	attrs, complete, e := searchAttributesIn(ctx, tx, h, public, p.Item.EntityIDs)
	if e != nil {
		return p, e
	}
	if !complete {
		p.Item.Truncated = true
	}
	for _, a := range attrs {
		if a.Conflict {
			continue
		}
		if a.Kind != "name" && a.Kind != "display_alias" {
			continue
		}
		canonical, e := archiveCanonicalIn(ctx, tx, h, a.EntityID)
		if e != nil {
			return p, e
		}
		for _, entity := range p.Item.EntityIDs {
			if canonical == entity && safeText(a.Value) {
				text = append(text, a.Value)
			}
		}
	}
	if safeText(p.Item.Domain) && safeText(p.Item.DocumentType) {
		text = append(text, p.Item.Domain, p.Item.DocumentType)
	} else {
		p.Item.Domain = "unknown"
		p.Item.DocumentType = "unknown"
	}
	if len(p.Item.EntityIDs) > 128 {
		p.Item.EntityIDs = p.Item.EntityIDs[:128]
		p.Item.Truncated = true
	}
	if len(p.Item.Dates) > 64 {
		p.Item.Dates = p.Item.Dates[:64]
		p.Item.Truncated = true
	}
	p.Text = strings.ToLower(strings.Join(text, "\n"))
	if len(p.Text) > 65536 {
		p.Text = searchBound(p.Text, 16384)
		p.Item.Truncated = true
	}
	for {
		encoded, _ := json.Marshal(p.Item)
		if len(encoded) <= 65536 || len(p.Item.Evidence) == 0 {
			break
		}
		p.Item.Evidence = p.Item.Evidence[:len(p.Item.Evidence)-1]
		p.Item.Truncated = true
	}
	return p, nil
}
func searchUnique(in []string) []string {
	out := []string{}
	for _, s := range in {
		out = appendUnique(out, s)
	}
	sort.Strings(out)
	return out
}

type searchCursor struct {
	Binding                         string `json:"b"`
	Epoch, Generation, IndexVersion int64
	After                           string  `json:"a"`
	AfterTime                       float64 `json:"t,omitempty"`
}

func signSearchCursor(c searchCursor, key []byte) string {
	b, _ := json.Marshal(c)
	m := hmac.New(sha256.New, key)
	m.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func readSearchCursor(raw string, want searchCursor, key []byte) (searchCursor, error) {
	if raw == "" {
		return want, nil
	}
	bad := domain.Errorf(domain.CodeInvalidRequest, "invalid or stale archive cursor; restart search")
	a, b, ok := strings.Cut(raw, ".")
	if !ok {
		return want, bad
	}
	payload, e := base64.RawURLEncoding.DecodeString(a)
	if e != nil {
		return want, bad
	}
	sig, e := base64.RawURLEncoding.DecodeString(b)
	if e != nil {
		return want, bad
	}
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	var got searchCursor
	if !hmac.Equal(sig, m.Sum(nil)) || json.Unmarshal(payload, &got) != nil || got.Binding != want.Binding || got.Epoch != want.Epoch || got.Generation != want.Generation || got.IndexVersion != want.IndexVersion {
		return want, bad
	}
	return got, nil
}

type archiveSearchActor struct {
	Integration          domain.IntegrationActor
	ScreenID, ScreenHash string
}

func (r *ArchiveTasks) SearchArchives(ctx context.Context, a domain.IntegrationActor, h string, public bool, f archive.SearchFilter, key []byte) (archive.SearchPage, error) {
	page, e := r.searchArchives(ctx, archiveSearchActor{Integration: a}, h, public, f, key, "")
	if e == nil {
		page.QueryRef, e = r.saveArchiveQuery(ctx, a, h, f)
	}
	return page, e
}
func (r *ArchiveTasks) SearchArchivesForScreen(ctx context.Context, id, hash, h string, f archive.SearchFilter, key []byte) (archive.SearchPage, error) {
	return r.searchArchives(ctx, archiveSearchActor{ScreenID: id, ScreenHash: hash}, h, true, f, key, "")
}
func (r *ArchiveTasks) authorizeSearchIn(ctx context.Context, tx *sql.Tx, a archiveSearchActor, h string) (string, error) {
	if a.ScreenID != "" {
		var n int
		e := tx.QueryRowContext(ctx, `SELECT count(*) FROM screens s JOIN local_household l ON l.singleton=1 WHERE s.id=? AND s.token_hash=? AND s.status='active' AND s.revoked_at IS NULL AND l.household_id=?`, a.ScreenID, a.ScreenHash, h).Scan(&n)
		if e != nil {
			return "", e
		}
		if n != 1 {
			return "", domain.Errorf(domain.CodeUnauthorized, "screen unavailable")
		}
		return "screen:" + a.ScreenID + ":" + a.ScreenHash, nil
	}
	now, e := r.currentTime()
	if e != nil {
		return "", e
	}
	p, e := r.authorizeArchiveClient(ctx, tx, a.Integration, h, "archive.read", now)
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("integration:%s:%s:%d", a.Integration.PrincipalID, a.Integration.CredentialID, p.PolicyVersion), nil
}
func (r *ArchiveTasks) searchArchives(ctx context.Context, a archiveSearchActor, h string, public bool, f archive.SearchFilter, key []byte, document string) (archive.SearchPage, error) {
	page := archive.SearchPage{Items: []archive.SearchItem{}, Coverage: archive.SearchCoverage{Complete: true}, IndexStatus: "ready"}
	f, e := f.Normalize()
	if e != nil {
		return page, domain.Errorf(domain.CodeInvalidRequest, "invalid archive search")
	}
	if len(key) == 0 {
		return page, domain.ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	e = r.db.InReadTx(ctx, func(tx *sql.Tx) error {
		identity, e := r.authorizeSearchIn(ctx, tx, a, h)
		if e != nil {
			return e
		}
		var epoch, generation, indexVersion int64
		if e = tx.QueryRowContext(ctx, `SELECT epoch,generation,index_revision FROM archive_search_state WHERE household_id=?`, h).Scan(&epoch, &generation, &indexVersion); e != nil {
			return e
		}
		page.Generation = fmt.Sprintf("%d:%d", epoch, generation)
		criteria := f
		criteria.Cursor = ""
		bind, _ := json.Marshal([]any{"recent-v1", h, identity, public, criteria, document})
		hash := sha256.Sum256(bind)
		c, e := readSearchCursor(f.Cursor, searchCursor{Binding: fmt.Sprintf("%x", hash), Epoch: epoch, Generation: generation, IndexVersion: indexVersion}, key)
		if e != nil {
			return e
		}
		var pending int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_documents d LEFT JOIN archive_search_rows s ON s.household_id=d.household_id AND s.document_id=d.id AND s.public=? AND s.epoch=? AND s.generation=? WHERE d.household_id=? AND d.withdrawn=0 AND (?=0 OR d.tv_visible=1) AND s.id IS NULL`, public, epoch, generation, h, public).Scan(&pending); e != nil {
			return e
		}
		var legacyPending int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM family_records f JOIN local_household l ON l.singleton=1 WHERE l.household_id=? AND NOT EXISTS(SELECT 1 FROM family_records n WHERE n.supersedes_id=f.id) AND json_extract(f.payload_json,'$.retracted') IS NOT 1 AND (?=0 OR json_extract(f.payload_json,'$.display_on_tv')=1) AND NOT EXISTS(SELECT 1 FROM archive_legacy_projections p WHERE p.household_id=? AND p.current_record_id=f.id)`, h, public, h).Scan(&legacyPending); e != nil {
			return e
		}
		pending += legacyPending
		if pending > 0 {
			page.Coverage = archive.SearchCoverage{Complete: false, Reason: "index_pending"}
			page.IndexStatus = "indexing"
		}
		rows, e := tx.QueryContext(ctx, `SELECT * FROM (
SELECT d.id AS sort_id,d.document_revision,coalesce(s.body,''),coalesce(s.text,''),CASE WHEN ?='' THEN 1 WHEN length(?)>2 THEN (coalesce(s.id,0) IN (SELECT rowid FROM archive_search_fts WHERE archive_search_fts MATCH ?) OR instr(coalesce(s.text,''),?)>0) ELSE instr(coalesce(s.text,''),?)>0 END,'' AS legacy_entity,'' AS legacy_record,coalesce((SELECT max(julianday(j.value)) FROM json_each(coalesce(s.body,'{}'),'$.dates') j),julianday(d.created_at),0) AS sort_time
FROM archive_documents d LEFT JOIN archive_search_rows s ON s.household_id=d.household_id AND s.document_id=d.id AND s.public=? AND s.epoch=? AND s.generation=? WHERE d.household_id=? AND d.withdrawn=0 AND (?=0 OR d.tv_visible=1)
UNION ALL
SELECT 'legacy:'||p.root_record_id,1,f.payload_json,'',1,p.entity_id,f.id,coalesce(julianday(json_extract(f.payload_json,'$.occurred_at')),0) FROM archive_legacy_projections p JOIN family_records f ON f.id=p.current_record_id JOIN local_household l ON l.singleton=1 AND l.household_id=p.household_id WHERE p.household_id=? AND p.active=1 AND json_extract(f.payload_json,'$.retracted') IS NOT 1 AND (?=0 OR json_extract(f.payload_json,'$.display_on_tv')=1) AND NOT EXISTS(SELECT 1 FROM family_records newer WHERE newer.supersedes_id=f.id)
) WHERE (?='' OR sort_time<? OR (sort_time=? AND sort_id<?)) ORDER BY sort_time DESC,sort_id DESC LIMIT 1001`, f.Query, f.Query, archive.LiteralFTS(strings.ToLower(f.Query)), strings.ToLower(f.Query), strings.ToLower(f.Query), public, epoch, generation, h, public, h, public, c.After, c.AfterTime, c.AfterTime, c.After)
		if e != nil {
			return e
		}
		defer rows.Close()
		scanned, chars := 0, 0
		last := c.After
		lastTime := c.AfterTime
		for rows.Next() {
			var id, body, text, legacyEntity, legacyRecord string
			var matched bool
			var rev int64
			var sortTime float64
			if e = rows.Scan(&id, &rev, &body, &text, &matched, &legacyEntity, &legacyRecord, &sortTime); e != nil {
				return e
			}
			scanned++
			chars += utf8.RuneCountInString(text)
			if scanned > 1000 || chars > 1<<20 {
				page.Coverage = archive.SearchCoverage{Complete: false, Reason: "scan_budget; narrow filters or continue"}
				c.After = last
				c.AfterTime = lastTime
				page.NextCursor = signSearchCursor(c, key)
				break
			}
			item := metadataSearchItem(id, rev)
			if legacyRecord != "" {
				canonical, err := archiveCanonicalIn(ctx, tx, h, legacyEntity)
				if err != nil {
					return err
				}
				item, text, e = legacySearchItem(id, canonical, legacyRecord, body)
				if e != nil {
					return e
				}
				attrs, complete, err := searchAttributesIn(ctx, tx, h, public, []string{canonical})
				if err != nil {
					return err
				}
				if !complete {
					item.Truncated = true
				}
				for _, attr := range attrs {
					text += "\n" + strings.ToLower(attr.Value)
				}
				chars += utf8.RuneCountInString(text)
			} else if body != "" {
				if e = json.Unmarshal([]byte(body), &item); e != nil {
					return e
				}
			}
			if chars > 1<<20 {
				page.Coverage = archive.SearchCoverage{Complete: false, Reason: "scan_budget; continue or narrow filters"}
				c.After = last
				c.AfterTime = lastTime
				page.NextCursor = signSearchCursor(c, key)
				break
			}
			if item.MetadataOnly || item.Truncated || item.ReviewRequired || item.ParseStatus != "parsed" || item.Ownership == "waiting" {
				page.Coverage = archive.SearchCoverage{Complete: false, Reason: "partial_or_pending"}
			}
			if !matched || !searchMatches(item, text, f) {
				last = id
				lastTime = sortTime
				continue
			}
			if f.Query != "" {
				sort.SliceStable(item.Evidence, func(i, j int) bool {
					return strings.Contains(strings.ToLower(item.Evidence[i].Excerpt), strings.ToLower(f.Query)) && !strings.Contains(strings.ToLower(item.Evidence[j].Excerpt), strings.ToLower(f.Query))
				})
			}
			if len(item.Evidence) > 3 {
				item.Evidence = item.Evidence[:3]
				item.Truncated = true
			}
			boundArchiveSummary(&item, f.EntityID)
			if item.Truncated {
				page.Coverage = archive.SearchCoverage{Complete: false, Reason: "partial_or_pending"}
			}
			candidate := page
			candidate.Items = append(append([]archive.SearchItem{}, page.Items...), item)
			c.After = id
			c.AfterTime = sortTime
			candidate.NextCursor = signSearchCursor(c, key)
			wire, _ := json.Marshal(candidate)
			if len(page.Items) == 0 && len(wire) > 8<<10 {
				return domain.Errorf(domain.CodePreviewUnavailable, "archive item exceeds bounded page; open original")
			}
			if len(page.Items) >= f.Limit || len(wire) > 8<<10 {
				c.After = last
				c.AfterTime = lastTime
				page.NextCursor = signSearchCursor(c, key)
				break
			}
			if a.ScreenID != "" {
				// Task transitions need not rebuild the semantic search index.
				// Read only the current coarse status in this authorized snapshot.
				if !strings.HasPrefix(item.DocumentID, "legacy:") {
					if e = tx.QueryRowContext(ctx, `SELECT coalesce((SELECT t.status FROM archive_sources s JOIN archive_tasks t ON t.household_id=s.household_id AND t.id=s.task_id WHERE s.household_id=? AND s.document_id=? ORDER BY s.received_at DESC,s.task_id DESC LIMIT 1),'')`, h, item.DocumentID).Scan(&item.TaskStatus); e != nil {
						return e
					}
				}
				archive.PresentScreenItem(&item)
			}
			page.Items = append(page.Items, item)
			last = id
			lastTime = sortTime
		}
		return rows.Err()
	})
	return page, e
}

// searchAssertionDate validates the asserted precision without broad parsing or
// converting partial dates to a fabricated first day/month.
func searchAssertionDate(value string) bool {
	layout := ""
	switch len(value) {
	case 4:
		layout = "2006"
	case 7:
		layout = "2006-01"
	case 10:
		layout = "2006-01-02"
	default:
		return false
	}
	parsed, err := time.Parse(layout, value)
	return err == nil && parsed.Year() > 0 && parsed.Format(layout) == value
}

func searchMatches(i archive.SearchItem, text string, f archive.SearchFilter) bool {
	if f.Domain != "" && i.Domain != f.Domain || f.DocumentType != "" && i.DocumentType != f.DocumentType || f.Ownership != "" && i.Ownership != f.Ownership {
		return false
	}
	if f.Query != "" && !strings.Contains(text, strings.ToLower(f.Query)) {
		return false
	}
	if f.EntityID != "" {
		found := false
		for _, v := range i.EntityIDs {
			found = found || v == f.EntityID
		}
		if !found {
			return false
		}
	}
	if f.DateFrom != "" || f.DateTo != "" {
		found := false
		for _, v := range i.Dates {
			if archive.DateWithinBounds(v, f.DateFrom, f.DateTo, f.Timezone) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ArchiveEvidence resolves a document's currently authorized publication even
// while the disposable search index is rebuilding. It never opens an original.
func (r *ArchiveTasks) ArchiveEvidence(ctx context.Context, a domain.IntegrationActor, h string, public bool, id string, f archive.SearchFilter, key []byte) (archive.SearchPage, error) {
	return r.archiveEvidence(ctx, archiveSearchActor{Integration: a}, h, public, id, f, key)
}
func (r *ArchiveTasks) ArchiveEvidenceForScreen(ctx context.Context, screen, hash, h, id string, f archive.SearchFilter, key []byte) (archive.SearchPage, error) {
	return r.archiveEvidence(ctx, archiveSearchActor{ScreenID: screen, ScreenHash: hash}, h, true, id, f, key)
}
func (r *ArchiveTasks) archiveEvidence(ctx context.Context, a archiveSearchActor, h string, public bool, id string, f archive.SearchFilter, key []byte) (archive.SearchPage, error) {
	out := archive.SearchPage{Items: []archive.SearchItem{}, Coverage: archive.SearchCoverage{Complete: true}, IndexStatus: "current"}
	f, e := f.Normalize()
	if e != nil || len(key) == 0 {
		return out, domain.Errorf(domain.CodeInvalidRequest, "invalid evidence page")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	e = r.db.InReadTx(ctx, func(tx *sql.Tx) error {
		who, e := r.authorizeSearchIn(ctx, tx, a, h)
		if e != nil {
			return e
		}
		var legacy *searchProjection
		if strings.HasPrefix(id, "legacy:") {
			p, e := legacySearchProjectionIn(ctx, tx, h, id, public)
			if e != nil {
				return e
			}
			legacy = &p
		} else {
			var n int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_documents WHERE household_id=? AND id=? AND withdrawn=0 AND (?=0 OR tv_visible=1)`, h, id, public).Scan(&n); e != nil {
				return e
			}
			if n != 1 {
				return domain.ErrNotFound
			}
		}

		var epoch, gen int64
		if e = tx.QueryRowContext(ctx, `SELECT epoch,generation FROM archive_search_state WHERE household_id=?`, h).Scan(&epoch, &gen); e != nil {
			return e
		}
		fcopy := f
		fcopy.Cursor = ""
		raw, _ := json.Marshal([]any{"evidence", who, h, public, id, fcopy})
		hash := sha256.Sum256(raw)
		c, e := readSearchCursor(f.Cursor, searchCursor{Binding: fmt.Sprintf("%x", hash), Epoch: epoch, Generation: gen}, key)
		if e != nil {
			return e
		}
		offset := 0
		if c.After != "" {
			if _, e = fmt.Sscanf(c.After, "%d", &offset); e != nil || offset < 0 {
				return domain.ErrConflict
			}
		}
		var p searchProjection
		if legacy != nil {
			p = *legacy
		} else {
			p, e = r.searchProjectionIn(ctx, tx, h, id, public)
			if e != nil {
				return e
			}
		}

		all := p.Item.Evidence
		if offset > len(all) {
			return domain.ErrConflict
		}
		p.Item.Evidence = []archive.SearchEvidence{}
		boundArchiveMetadata(&p.Item, f.EntityID)
		for i := offset; i < len(all); i++ {
			p.Item.Evidence = append(p.Item.Evidence, all[i])
			probe, _ := json.Marshal(p.Item)
			if len(p.Item.Evidence) == 1 && len(probe) > 6<<10 {
				return domain.Errorf(domain.CodePreviewUnavailable, "single archive evidence exceeds bounded page; open original")
			}
			if len(p.Item.Evidence) > f.Limit || len(probe) > 6<<10 {
				p.Item.Evidence = p.Item.Evidence[:len(p.Item.Evidence)-1]
				c.After = fmt.Sprint(i)
				out.NextCursor = signSearchCursor(c, key)
				break
			}
		}
		if a.ScreenID != "" {
			archive.PresentScreenItem(&p.Item)
		}
		out.Items = append(out.Items, p.Item)
		if p.Item.MetadataOnly || p.Item.Truncated || p.Item.ReviewRequired || p.Item.ParseStatus != "parsed" {
			out.Coverage = archive.SearchCoverage{Complete: false, Reason: "partial_or_pending"}
		}
		return nil
	})
	return out, e
}

func (r *ArchiveTasks) SearchArchiveEntities(ctx context.Context, a domain.IntegrationActor, h string, public bool, f archive.SearchFilter, key []byte) (archive.EntityPage, error) {
	return r.searchEntities(ctx, archiveSearchActor{Integration: a}, h, public, f, key)
}
func (r *ArchiveTasks) SearchArchiveEntitiesForScreen(ctx context.Context, id, hash, h string, f archive.SearchFilter, key []byte) (archive.EntityPage, error) {
	return r.searchEntities(ctx, archiveSearchActor{ScreenID: id, ScreenHash: hash}, h, true, f, key)
}
func (r *ArchiveTasks) searchEntities(ctx context.Context, a archiveSearchActor, h string, public bool, f archive.SearchFilter, key []byte) (archive.EntityPage, error) {
	out := archive.EntityPage{Items: []archive.SearchEntity{}, Coverage: archive.SearchCoverage{Complete: true}}
	f, e := f.Normalize()
	if e != nil || len(key) == 0 || f.Domain != "" || f.DocumentType != "" || f.DateFrom != "" || f.DateTo != "" || f.Ownership != "" {
		return out, domain.Errorf(domain.CodeInvalidRequest, "invalid entity search")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	e = r.db.InReadTx(ctx, func(tx *sql.Tx) error {
		who, e := r.authorizeSearchIn(ctx, tx, a, h)
		if e != nil {
			return e
		}
		var epoch, gen int64
		if e = tx.QueryRowContext(ctx, `SELECT epoch,generation FROM archive_search_state WHERE household_id=?`, h).Scan(&epoch, &gen); e != nil {
			return e
		}
		criteria := f
		criteria.Cursor = ""
		raw, _ := json.Marshal([]any{"entities", who, h, public, criteria})
		hash := sha256.Sum256(raw)
		c, e := readSearchCursor(f.Cursor, searchCursor{Binding: fmt.Sprintf("%x", hash), Epoch: epoch, Generation: gen}, key)
		if e != nil {
			return e
		}
		attrs, complete, e := searchAttributesIn(ctx, tx, h, public, nil)
		if e != nil {
			return e
		}
		if !complete {
			out.Coverage = archive.SearchCoverage{Complete: false, Reason: "entity_budget"}
			return nil
		}
		items := map[string]*archive.SearchEntity{}
		for _, attr := range attrs {
			if attr.Conflict || attr.Kind != "name" && attr.Kind != "display_alias" {
				continue
			}
			if attr.DocumentID.Valid {
				var n int
				if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_documents d WHERE d.household_id=? AND d.id=? AND d.withdrawn=0 AND (?=0 OR (d.tv_visible=1 AND d.original_restricted=0)) AND NOT EXISTS(SELECT 1 FROM archive_protected_identity_facts p WHERE p.household_id=d.household_id AND p.document_id=d.id AND instr(?,p.value)>0)`, h, attr.DocumentID.String, public, attr.Value).Scan(&n); e != nil {
					return e
				}
				if n == 0 {
					continue
				}
			} else {
				var local string
				if e = tx.QueryRowContext(ctx, `SELECT household_id FROM local_household WHERE singleton=1`).Scan(&local); e != nil {
					return e
				}
				if local != h {
					continue
				}
			}
			id, e := archiveCanonicalIn(ctx, tx, h, attr.EntityID)
			if e != nil {
				return e
			}
			if id <= c.After || f.EntityID != "" && id != f.EntityID {
				continue
			}
			item := items[id]
			if item == nil {
				kind, _, e := archiveEntityVersionIn(ctx, tx, h, id)
				if e != nil {
					return e
				}
				item = &archive.SearchEntity{ID: id, Kind: kind, Names: []string{}, Aliases: []string{}}
				items[id] = item
			}
			if attr.Kind == "name" {
				item.Names = appendUnique(item.Names, attr.Value)
			} else {
				item.Aliases = appendUnique(item.Aliases, attr.Value)
			}
		}
		ids := []string{}
		for id := range items {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		last := c.After
		for _, id := range ids {
			item := items[id]
			if len(item.Names) == 0 {
				continue
			}
			text := strings.ToLower(strings.Join(append(append([]string{}, item.Names...), item.Aliases...), "\n"))
			if f.Query != "" && !strings.Contains(text, strings.ToLower(f.Query)) {
				continue
			}
			sort.Strings(item.Names)
			sort.Strings(item.Aliases)
			if len(item.Names) > 16 {
				item.Names = item.Names[:16]
				out.Coverage = archive.SearchCoverage{Complete: false, Reason: "entity_labels_truncated"}
			}
			if len(item.Aliases) > 16 {
				item.Aliases = item.Aliases[:16]
				out.Coverage = archive.SearchCoverage{Complete: false, Reason: "entity_labels_truncated"}
			}
			// Bound a single entity before paging so a legal long alias set cannot
			// produce an empty page with a cursor that never advances.
			for {
				b, _ := json.Marshal(item)
				if len(b) <= 4<<10 {
					break
				}
				out.Coverage = archive.SearchCoverage{Complete: false, Reason: "entity_labels_truncated"}
				if len(item.Aliases) > 0 {
					item.Aliases = item.Aliases[:len(item.Aliases)-1]
				} else if len(item.Names) > 1 {
					item.Names = item.Names[:len(item.Names)-1]
				} else {
					return domain.Errorf(domain.CodePreviewUnavailable, "entity label exceeds bounded page")
				}
			}
			candidate := out
			candidate.Items = append(append([]archive.SearchEntity{}, out.Items...), *item)
			c.After = id
			candidate.NextCursor = signSearchCursor(c, key)
			wire, _ := json.Marshal(candidate)
			if len(out.Items) >= f.Limit || len(wire) > 8<<10 {
				c.After = last
				out.NextCursor = signSearchCursor(c, key)
				break
			}
			out.Items = append(out.Items, *item)
			last = id
		}
		return nil
	})
	return out, e
}

func legacySearchItem(id, entity, record, raw string) (archive.SearchItem, string, error) {
	var in domain.FamilyRecordInput
	if e := json.Unmarshal([]byte(raw), &in); e != nil {
		return archive.SearchItem{}, "", e
	}
	item := metadataSearchItem(id, 1)
	item.Domain = "health"
	if in.Category == "note" {
		item.Domain = "other"
	}
	item.DocumentType = in.Category
	item.Ownership = "confirmed"
	item.ParseStatus = "parsed"
	item.MetadataOnly = false
	item.EntityIDs = []string{entity}
	item.Dates = []string{in.OccurredAt.Format(time.RFC3339Nano)}
	text := in.Title + "\n" + in.Notes
	for _, m := range in.Measurements {
		text += fmt.Sprintf("\n%s %g %s", m.Metric, m.Value, m.Unit)
	}
	quote := searchBound(text, 2048)
	item.Truncated = len(quote) < len(text)
	item.Evidence = []archive.SearchEvidence{{DocumentID: id, LegacyRecordID: record, SourceKind: "legacy", Locator: archive.Locator{Kind: "legacy_record"}, Excerpt: quote, End: utf8.RuneCountInString(quote), Confirmed: true}}
	return item, strings.ToLower(text), nil
}

// Name search is display matching, not identity resolution. Every contributing
// alias retains its source's current privacy and withdrawal dependencies.
func searchAttributesIn(ctx context.Context, tx *sql.Tx, h string, public bool, entityIDs []string) ([]entityAttribute, bool, error) {
	query := `WITH RECURSIVE selected(id) AS (SELECT value FROM json_each(?) UNION SELECT r.source_id FROM archive_entity_redirects r JOIN selected s ON s.id=r.target_id WHERE r.household_id=?) SELECT a.id,a.entity_id,a.kind,a.value,a.precision,a.document_id,a.legacy_member_id,a.evidence_json,a.tv_visible,a.conflict FROM archive_entity_attributes a WHERE a.household_id=? AND a.kind IN ('name','display_alias') AND a.conflict=0 AND (?=1 OR a.entity_id IN (SELECT id FROM selected)) AND (a.link_id IS NULL OR EXISTS(SELECT 1 FROM archive_entity_links l WHERE l.household_id=a.household_id AND l.id=a.link_id AND l.status='linked')) AND (
(a.document_id IS NOT NULL AND EXISTS(SELECT 1 FROM archive_documents d WHERE d.household_id=a.household_id AND d.id=a.document_id AND d.withdrawn=0 AND (?=0 OR (d.tv_visible=1 AND d.original_restricted=0 AND a.tv_visible=1))) AND NOT EXISTS(SELECT 1 FROM archive_protected_identity_facts p WHERE p.household_id=a.household_id AND p.document_id=a.document_id AND instr(a.value,p.value)>0))
OR (a.legacy_member_id IS NOT NULL AND EXISTS(SELECT 1 FROM local_household l WHERE l.singleton=1 AND l.household_id=a.household_id) AND (?=0 OR EXISTS(SELECT 1 FROM family_records f WHERE json_extract(f.payload_json,'$.member_id')=a.legacy_member_id AND json_extract(f.payload_json,'$.display_on_tv')=1 AND json_extract(f.payload_json,'$.retracted') IS NOT 1 AND NOT EXISTS(SELECT 1 FROM family_records n WHERE n.supersedes_id=f.id))))) ORDER BY a.id LIMIT 16385`
	rows, e := tx.QueryContext(ctx, query, entityJSON(entityIDs), h, h, entityIDs == nil, public, public)
	if e != nil {
		return nil, false, e
	}
	defer rows.Close()
	out := []entityAttribute{}
	for rows.Next() {
		var a entityAttribute
		var raw string
		if e = rows.Scan(&a.ID, &a.EntityID, &a.Kind, &a.Value, &a.Precision, &a.DocumentID, &a.LegacyID, &raw, &a.Visible, &a.Conflict); e != nil {
			return nil, false, e
		}
		if e = json.Unmarshal([]byte(raw), &a.Evidence); e != nil {
			return nil, false, e
		}
		out = append(out, a)
	}
	if len(out) > 16384 {
		return nil, false, nil
	}
	return out, true, rows.Err()
}

func legacySearchProjectionIn(ctx context.Context, tx *sql.Tx, h, id string, public bool) (searchProjection, error) {
	var out searchProjection
	var entity, record, raw string
	e := tx.QueryRowContext(ctx, `SELECT p.entity_id,f.id,f.payload_json FROM archive_legacy_projections p JOIN family_records f ON f.id=p.current_record_id JOIN local_household l ON l.singleton=1 AND l.household_id=p.household_id WHERE p.household_id=? AND p.root_record_id=? AND p.active=1 AND json_extract(f.payload_json,'$.retracted') IS NOT 1 AND (?=0 OR json_extract(f.payload_json,'$.display_on_tv')=1) AND NOT EXISTS(SELECT 1 FROM family_records n WHERE n.supersedes_id=f.id)`, h, strings.TrimPrefix(id, "legacy:"), public).Scan(&entity, &record, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return out, domain.ErrNotFound
	}
	if e != nil {
		return out, e
	}
	canonical, err := archiveCanonicalIn(ctx, tx, h, entity)
	if err != nil {
		return out, err
	}
	out.Item, out.Text, e = legacySearchItem(id, canonical, record, raw)
	return out, e
}

// Page summaries are bounded separately from immutable evidence. Omitted
// metadata is explicitly marked and never used as a complete relationship list.
func boundArchiveMetadata(item *archive.SearchItem, requestedEntity string) {
	for {
		meta := *item
		meta.Evidence = nil
		b, _ := json.Marshal(meta)
		if len(b) <= 2<<10 {
			return
		}
		item.Truncated = true
		if len(item.Dates) > 0 {
			item.Dates = item.Dates[:len(item.Dates)-1]
			continue
		}
		if len(item.EntityIDs) > 1 {
			remove := len(item.EntityIDs) - 1
			if item.EntityIDs[remove] == requestedEntity {
				remove--
			}
			item.EntityIDs = append(append([]string{}, item.EntityIDs[:remove]...), item.EntityIDs[remove+1:]...)
			continue
		}
		return
	}
}
func boundArchiveSummary(item *archive.SearchItem, requestedEntity string) {
	boundArchiveMetadata(item, requestedEntity)
	for {
		b, _ := json.Marshal(item)
		if len(b) <= 6<<10 || len(item.Evidence) == 0 {
			break
		}
		item.Evidence = item.Evidence[:len(item.Evidence)-1]
		item.Truncated = true
	}
	if len(item.Evidence) == 0 {
		item.MetadataOnly = true
	}
}
