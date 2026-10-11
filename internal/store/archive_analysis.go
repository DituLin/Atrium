package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
)

func (r *ArchiveTasks) AnalyzeArchives(ctx context.Context, a domain.IntegrationActor, h string, public bool, q archive.AnalysisQuery) (archive.AnalysisResult, error) {
	return r.analyzeArchives(ctx, archiveSearchActor{Integration: a}, h, public, q)
}
func (r *ArchiveTasks) AnalyzeArchivesForScreen(ctx context.Context, id, hash, h string, q archive.AnalysisQuery) (archive.AnalysisResult, error) {
	return r.analyzeArchives(ctx, archiveSearchActor{ScreenID: id, ScreenHash: hash}, h, true, q)
}
func (r *ArchiveTasks) authorizeAnalysisIn(ctx context.Context, tx *sql.Tx, a archiveSearchActor, h string) (string, error) {
	who, e := r.authorizeSearchIn(ctx, tx, a, h)
	if e != nil {
		return "", e
	}
	if a.ScreenID == "" {
		now, e := r.currentTime()
		if e != nil {
			return "", e
		}
		if _, e = r.authorizeArchiveClient(ctx, tx, a.Integration, h, "archive.analyze", now); e != nil {
			return "", e
		}
	}
	return who, nil
}
func (r *ArchiveTasks) analyzeArchives(ctx context.Context, a archiveSearchActor, h string, public bool, q archive.AnalysisQuery) (archive.AnalysisResult, error) {
	var result archive.AnalysisResult
	var e error
	q, e = q.Normalize()
	if e != nil {
		return result, domain.Errorf(domain.CodeInvalidRequest, "invalid analysis query")
	}
	var who string
	var epoch, generation int64
	input := archive.AnalysisInput{Complete: true, Facts: []archive.AnalysisFact{}, Documents: []string{}}
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	e = r.db.InReadTx(readCtx, func(tx *sql.Tx) error {
		var e error
		who, e = r.authorizeAnalysisIn(readCtx, tx, a, h)
		if e != nil {
			return e
		}
		if e = tx.QueryRowContext(readCtx, `SELECT epoch,generation FROM archive_search_state WHERE household_id=?`, h).Scan(&epoch, &generation); e != nil {
			return e
		}
		rows, e := tx.QueryContext(readCtx, `SELECT id FROM archive_documents WHERE household_id=? AND withdrawn=0 AND (?=0 OR tv_visible=1) ORDER BY id LIMIT 4097`, h, public)
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
		if len(ids) > 4096 {
			return domain.ErrConflict
		}
		filter := archive.SearchFilter{Query: q.Query, EntityID: q.EntityID, Domain: q.Domain, DocumentType: q.DocumentType}
		for _, id := range ids {
			p, e := r.searchProjectionIn(readCtx, tx, h, id, public)
			if e != nil {
				return e
			}
			if !searchMatches(p.Item, p.Text, filter) {
				continue
			}
			input.Documents = append(input.Documents, id)
			if p.Item.ReviewRequired || p.Item.Truncated || p.Item.MetadataOnly || p.Item.ParseStatus != "parsed" {
				input.Complete = false
				input.Reasons = appendUnique(input.Reasons, "partial_or_pending_sources")
			}
			facts, e := r.analysisFactsIn(readCtx, tx, h, id, public, p, q.EntityID)
			if e != nil {
				return e
			}
			for _, f := range facts {
				input.Facts = append(input.Facts, f)
			}
			if len(input.Facts) > 4096 {
				return domain.ErrConflict
			}
		}
		return r.analysisLegacyIn(readCtx, tx, h, public, q, &input)
	})
	if e != nil {
		return result, e
	}
	result, e = archive.Analyze(q, input)
	if e != nil {
		return result, domain.Errorf(domain.CodeConflict, "analysis bounds or inconsistent facts")
	}
	result.Projection = "authorized"
	if public {
		result.Projection = "public"
	}
	result.DataEpoch = epoch
	result.Generation = generation
	raw, e := json.Marshal(input)
	if e != nil {
		return result, e
	}
	hash := sha256.Sum256(raw)
	result.InputDigest = hex.EncodeToString(hash[:])
	result.SnapshotID = domain.NewID()
	body, e := json.Marshal(result)
	if e != nil {
		return result, e
	}
	if len(body) > 32<<10 {
		return archive.AnalysisResult{}, domain.Errorf(domain.CodeConflict, "analysis result exceeds bound; narrow query")
	}
	query, _ := json.Marshal(q)
	e = r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		current, e := r.authorizeAnalysisIn(ctx, tx, a, h)
		if e != nil {
			return e
		}
		var ep, gen int64
		if e = tx.QueryRowContext(ctx, `SELECT epoch,generation FROM archive_search_state WHERE household_id=?`, h).Scan(&ep, &gen); e != nil {
			return e
		}
		if current != who || ep != epoch || gen != generation {
			return domain.ErrConflict
		}
		now, e := r.currentTime()
		if e != nil {
			return e
		}
		// Snapshots are disposable; keep a bounded recent set without retaining stale private totals indefinitely.
		if _, e = tx.ExecContext(ctx, `DELETE FROM archive_analysis_snapshots WHERE household_id=? AND (epoch<>? OR generation<>? OR id IN (SELECT id FROM archive_analysis_snapshots WHERE household_id=? ORDER BY created_at DESC,id DESC LIMIT -1 OFFSET 127))`, h, epoch, generation, h); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO archive_analysis_snapshots(household_id,id,authority,public,epoch,generation,algorithm_version,query_json,input_digest,result_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, h, result.SnapshotID, who, public, epoch, generation, archive.AnalysisAlgorithmVersion, string(query), result.InputDigest, string(body), now.UTC().Format(time.RFC3339Nano))
		return e
	})
	if e != nil {
		return archive.AnalysisResult{}, e
	}
	return result, nil
}
func (r *ArchiveTasks) ArchiveAnalysisSnapshot(ctx context.Context, a domain.IntegrationActor, h string, public bool, id string) (archive.AnalysisResult, error) {
	return r.analysisSnapshot(ctx, archiveSearchActor{Integration: a}, h, public, id)
}
func (r *ArchiveTasks) ArchiveAnalysisSnapshotForScreen(ctx context.Context, id, hash, h, snapshot string) (archive.AnalysisResult, error) {
	return r.analysisSnapshot(ctx, archiveSearchActor{ScreenID: id, ScreenHash: hash}, h, true, snapshot)
}
func (r *ArchiveTasks) analysisSnapshot(ctx context.Context, a archiveSearchActor, h string, public bool, id string) (archive.AnalysisResult, error) {
	var out archive.AnalysisResult
	e := r.db.InReadTx(ctx, func(tx *sql.Tx) error {
		who, e := r.authorizeAnalysisIn(ctx, tx, a, h)
		if e != nil {
			return e
		}
		var raw string
		e = tx.QueryRowContext(ctx, `SELECT s.result_json FROM archive_analysis_snapshots s JOIN archive_search_state g ON g.household_id=s.household_id AND g.epoch=s.epoch AND g.generation=s.generation WHERE s.household_id=? AND s.id=? AND s.authority=? AND s.public=? AND s.algorithm_version=?`, h, id, who, public, archive.AnalysisAlgorithmVersion).Scan(&raw)
		if e == sql.ErrNoRows {
			return domain.ErrNotFound
		}
		if e != nil {
			return e
		}
		return json.Unmarshal([]byte(raw), &out)
	})
	return out, e
}
func (r *ArchiveTasks) analysisFactsIn(ctx context.Context, tx *sql.Tx, h, doc string, public bool, p searchProjection, entityID string) ([]archive.AnalysisFact, error) {
	observations, e := archiveCurrentObservationsIn(ctx, tx, h, doc, public)
	if e != nil {
		return nil, e
	}
	out := []archive.AnalysisFact{}
	type currentBody struct {
		Field          archive.PublicationField
		Entities       []string
		AssertedValue  string
		ValueAssertion *ArchiveAssertionProvenance
	}
	bodies := map[string]currentBody{}
	events := map[string][]ArchiveCurrentObservation{}
	for _, o := range observations {
		var b currentBody
		if e = json.Unmarshal(o.Body, &b); e != nil {
			return nil, e
		}
		bodies[o.ID] = b
		events[o.EventID] = append(events[o.EventID], o)
	}
	nonExactValues := map[string]bool{}
	for _, o := range observations {
		b := bodies[o.ID]
		if b.ValueAssertion != nil {
			continue
		}
		bundle, err := searchBundleIn(ctx, tx, h, o.ExtractionID)
		if err != nil {
			return nil, err
		}
		nonExactValues[o.ID] = analysisQualifiedValue(bundle.Parsed, b.Field)
	}
	hiddenDimensions := map[string]bool{}
	if public {
		all, err := archiveCurrentObservationsIn(ctx, tx, h, doc, false)
		if err != nil {
			return nil, err
		}
		for _, peer := range all {
			if _, visible := bodies[peer.ID]; visible {
				continue
			}
			var body currentBody
			if err = json.Unmarshal(peer.Body, &body); err != nil {
				return nil, err
			}
			switch body.Field.Name {
			case "sample", "specimen", "subject", "project", "item", "unit", "currency", "full_score", "maximum_score", "满分":
				hiddenDimensions[peer.EventID] = true
			}
			if analysisEventDateField(body.Field.Name) || analysisBoundaryField(body.Field.Name) != "" {
				hiddenDimensions[peer.EventID] = true
			}
		}
	}
	evidence := func(id string) []archive.SearchEvidence {
		out := []archive.SearchEvidence{}
		for _, ev := range p.Item.Evidence {
			if ev.ObservationID == id && ev.Confirmed {
				out = append(out, ev)
			}
		}
		return out
	}
	for _, o := range observations {
		b := bodies[o.ID]
		if entityID != "" {
			matched := false
			for _, id := range b.Entities {
				matched = matched || id == entityID
			}
			if !matched {
				continue
			}
		}
		f := b.Field
		if len(evidence(o.ID)) == 0 || f.RequiresReview {
			continue
		}
		value := f.Raw
		for _, n := range f.Normalizations {
			if n.Kind == "decimal" {
				value = n.Value
			}
		}
		if b.ValueAssertion != nil {
			value = b.AssertedValue
		}
		item := archive.AnalysisFact{AggregateBoundary: map[string]string{}, Unavailable: hiddenDimensions[o.EventID], NonExact: nonExactValues[o.ID], DocumentID: doc, EventID: o.EventID, ObservationID: o.ID, Revision: o.Revision, Metric: f.Name, Value: value, Domain: p.Item.Domain, Evidence: evidence(o.ID)}
		if len(b.Entities) > 0 {
			owners := append([]string{}, b.Entities...)
			sort.Strings(owners)
			item.AggregateBoundary["owners"] = strings.Join(owners, ",")
		}
		if item.Domain == "health" || item.Domain == "education" {
			item.Sample = "owners:" + strings.Join(b.Entities, ",") + ";"
		}
		for _, dim := range f.Dimensions {
			switch dim.Kind {
			case "unit":
				item.Unit = dim.Raw
			case "currency":
				item.Currency = dim.Raw
			}
		}
		dates := []string{}
		denominatorCount := 0
		for _, peer := range events[o.EventID] {
			pb := bodies[peer.ID]
			if len(pb.Entities) > 0 {
				same := len(pb.Entities) == len(b.Entities)
				owners := map[string]bool{}
				for _, id := range b.Entities {
					owners[id] = true
				}
				for _, id := range pb.Entities {
					same = same && owners[id]
				}
				if !same {
					continue
				}
			}
			if len(evidence(peer.ID)) == 0 || pb.Field.RequiresReview {
				if analysisBoundaryField(pb.Field.Name) != "" {
					item.AggregateBoundaryUnknown = true
				}
				continue
			}
			for _, n := range pb.Field.Normalizations {
				if (n.Kind == "date" || n.Kind == "date_partial") && analysisEventDateField(pb.Field.Name) {
					v := n.Value
					if pb.ValueAssertion != nil {
						v = pb.AssertedValue
					}
					dates = appendUnique(dates, v)
					item.Evidence = append(item.Evidence, evidence(peer.ID)...)
				}
			}
			peerValue := pb.Field.Raw
			if pb.ValueAssertion != nil {
				peerValue = pb.AssertedValue
			}
			if key := analysisBoundaryField(pb.Field.Name); key != "" {
				v := strings.TrimSpace(peerValue)
				if old, exists := item.AggregateBoundary[key]; exists && old != v || v == "" {
					item.AggregateBoundaryUnknown = true
				}
				item.AggregateBoundary[key] = v
				item.BoundaryEvidence = append(item.BoundaryEvidence, evidence(peer.ID)...)
			}
			for _, dim := range f.Dimensions {
				for _, de := range dim.Evidence {
					for _, pe := range pb.Field.Evidence {
						if de.ParentSegmentID == pe.ParentSegmentID && de.Start == pe.Start && de.End == pe.End {
							if dim.Kind == "unit" {
								item.Unit = peerValue
							}
							if dim.Kind == "currency" {
								item.Currency = peerValue
							}
							item.Evidence = append(item.Evidence, evidence(peer.ID)...)
						}
					}
				}
			}
			switch pb.Field.Name {
			case "full_score", "maximum_score", "满分":
				if peer.ID != o.ID {
					denominatorCount++
					item.Denominator = pb.Field.Raw
					if pb.ValueAssertion != nil {
						item.Denominator = pb.AssertedValue
					}
					item.DenominatorEvidence = evidence(peer.ID)
					item.DenominatorNonExact = nonExactValues[peer.ID]
				}
			case "sample", "specimen", "subject", "project", "item":
				item.Sample += pb.Field.Name + ":" + peerValue + ";"
				item.Evidence = append(item.Evidence, evidence(peer.ID)...)
			}
		}
		if denominatorCount > 1 {
			item.Denominator = ""
			item.DenominatorEvidence = nil
		}
		if len(dates) == 1 {
			item.Date = dates[0]
			item.AggregateBoundary["event_date"] = dates[0]
		} else if len(dates) > 1 {
			item.AggregateBoundaryUnknown = true
		}
		// Roles must appear in current, visible evidence. Model field names alone do not establish them.
		for _, ev := range item.Evidence {
			role := analysisLiteralRole(ev.Excerpt)
			if role != "" {
				if item.Role != "" && item.Role != role {
					item.Role = "unknown"
					break
				}
				item.Role = role
				item.RoleEvidence = append(item.RoleEvidence, ev)
			}
		}
		if item.Role == "" {
			role, ev, err := r.analysisRoleEvidenceIn(ctx, tx, h, doc, public, o, f)
			if err != nil {
				return nil, err
			}
			item.Role = role
			item.RoleEvidence = ev
		}
		if public && item.Role == "total" {
			var restricted bool
			if err := tx.QueryRowContext(ctx, `SELECT original_restricted FROM archive_documents WHERE household_id=? AND id=?`, h, o.SourceDocumentID).Scan(&restricted); err != nil {
				return nil, err
			}
			if restricted {
				item.Role = ""
				item.RoleEvidence = nil
			}
		}
		out = append(out, item)
	}
	analysisTableAggregates(out)
	if err := r.analysisFormulaAggregatesIn(ctx, tx, h, doc, public, out); err != nil {
		return nil, err
	}
	return out, nil
}
func analysisLiteralRole(s string) string {
	found := ""
	for _, p := range []struct {
		role   string
		labels []string
	}{{"total", []string{"合计", "总计", "total"}}, {"payment", []string{"付款", "支付", "payment"}}, {"line_item", []string{"费用", "支出", "明细", "expense", "line item", "charge"}}} {
		for _, label := range p.labels {
			if strings.Contains(strings.ToLower(s), label) {
				if found != "" && found != p.role {
					return ""
				}
				found = p.role
			}
		}
	}
	return found
}
func (r *ArchiveTasks) analysisLegacyIn(ctx context.Context, tx *sql.Tx, h string, public bool, q archive.AnalysisQuery, in *archive.AnalysisInput) error {
	var pending int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM family_records f JOIN local_household l ON l.singleton=1 WHERE l.household_id=? AND json_extract(f.payload_json,'$.retracted') IS NOT 1 AND (?=0 OR json_extract(f.payload_json,'$.display_on_tv')=1) AND NOT EXISTS(SELECT 1 FROM family_records n WHERE n.supersedes_id=f.id) AND NOT EXISTS(SELECT 1 FROM archive_legacy_projections p WHERE p.household_id=? AND p.current_record_id=f.id AND p.active=1)`, h, public, h).Scan(&pending); e != nil {
		return e
	}
	if pending > 0 {
		in.Complete = false
		in.Reasons = appendUnique(in.Reasons, "legacy_projection_pending")
	}

	rows, e := tx.QueryContext(ctx, `SELECT p.root_record_id,p.current_record_id,f.payload_json,p.event_id FROM archive_legacy_projections p JOIN family_records f ON f.id=p.current_record_id JOIN local_household l ON l.singleton=1 AND l.household_id=p.household_id WHERE p.household_id=? AND p.active=1 AND json_extract(f.payload_json,'$.retracted') IS NOT 1 AND (?=0 OR json_extract(f.payload_json,'$.display_on_tv')=1) AND NOT EXISTS(SELECT 1 FROM family_records n WHERE n.supersedes_id=f.id) ORDER BY p.root_record_id LIMIT 4097`, h, public)
	if e != nil {
		return e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 4096 {
			return domain.ErrConflict
		}
		var root, record, raw, event string
		if e = rows.Scan(&root, &record, &raw, &event); e != nil {
			return e
		}
		p, e := legacySearchProjectionIn(ctx, tx, h, "legacy:"+root, public)
		if e != nil {
			return e
		}
		if !searchMatches(p.Item, p.Text, archive.SearchFilter{Query: q.Query, Domain: q.Domain, DocumentType: q.DocumentType, EntityID: q.EntityID}) {
			continue
		}
		in.Documents = append(in.Documents, "legacy:"+root)
		var body struct {
			OccurredAt   string `json:"occurred_at"`
			Measurements []struct {
				Metric string      `json:"metric"`
				Value  json.Number `json:"value"`
				Unit   string      `json:"unit"`
			} `json:"measurements"`
		}
		if e = json.Unmarshal([]byte(raw), &body); e != nil {
			return e
		}
		for i, m := range body.Measurements {
			ev := p.Item.Evidence[0]
			ev.SegmentID = fmt.Sprintf("measurement:%d", i)
			ev.ObservationID = event + ":" + ev.SegmentID
			ev.ObservationRevision = 1
			in.Facts = append(in.Facts, archive.AnalysisFact{DocumentID: "legacy:" + root, EventID: event, ObservationID: ev.ObservationID, Revision: 1, Metric: m.Metric, Value: m.Value.String(), Unit: m.Unit, Domain: p.Item.Domain, Date: body.OccurredAt, Evidence: []archive.SearchEvidence{ev}})
		}
	}
	return rows.Err()
}

var analysisRolePrefix = regexp.MustCompile(`(?i)(费用|支出|明细|expense|line item|charge|合计|总计|total|付款|支付|payment)\s*[:：]?\s*$`)

func (r *ArchiveTasks) analysisRoleEvidenceIn(ctx context.Context, tx *sql.Tx, h, doc string, public bool, o ArchiveCurrentObservation, f archive.PublicationField) (string, []archive.SearchEvidence, error) {
	b, e := searchBundleIn(ctx, tx, h, o.ExtractionID)
	if e != nil {
		return "", nil, e
	}
	private := []archive.PhysicalAnchor{}
	if public {
		private, e = archivePrivateAnchorsIn(ctx, tx, h, o.SourceDocumentID)
		if e != nil {
			return "", nil, e
		}
	}
	var refs []archive.SearchEvidence
	role := ""
	for _, ev := range f.Evidence {
		for _, seg := range b.Parsed.Segments {
			if seg.ID != ev.ParentSegmentID {
				continue
			}
			runes := []rune(seg.Text)
			if ev.Start < 0 || ev.Start > len(runes) {
				continue
			}
			prefix := string(runes[:ev.Start])
			loc := analysisRolePrefix.FindStringIndex(prefix)
			if loc == nil {
				continue
			}
			quote := strings.TrimSpace(prefix[loc[0]:loc[1]])
			start := utf8.RuneCountInString(prefix[:loc[0]])
			end := utf8.RuneCountInString(prefix[:loc[1]])
			anchor := archive.PhysicalAnchor{Locator: seg.Locator, Component: ev.ComponentKind, Start: start, End: end}
			hidden := false
			for _, secret := range private {
				if !archiveAnchorsDisjoint(secret, anchor) {
					hidden = true
				}
			}
			if hidden {
				continue
			}
			current := analysisLiteralRole(quote)
			if current == "" {
				continue
			}
			if role != "" && role != current {
				return "", nil, nil
			}
			role = current
			refs = append(refs, archive.SearchEvidence{DocumentID: o.SourceDocumentID, PublicationID: o.PublicationID, PublicationRevision: o.DocumentRevision, ObservationID: o.ID, ObservationRevision: o.Revision, SourceExtractionID: o.ExtractionID, SegmentID: seg.ID, Locator: seg.Locator, Start: start, End: end, Excerpt: quote, Confirmed: true, SourceKind: "analysis_role_label"})
		}
	}

	if role == "" {
		for _, ev := range f.Evidence {
			var valueSegment *archive.Segment
			for i := range b.Parsed.Segments {
				if b.Parsed.Segments[i].ID == ev.ParentSegmentID {
					valueSegment = &b.Parsed.Segments[i]
					break
				}
			}
			if valueSegment == nil {
				continue
			}
			table, row, col := analysisTablePosition(valueSegment.Locator)
			if table == "" {
				continue
			}
			for _, priority := range []int{0, 1} {
				for _, label := range b.Parsed.Segments {
					lt, lr, lc := analysisTablePosition(label.Locator)
					if lt != table || priority == 0 && (lr != row || lc >= col) || priority == 1 && (lr != 1 || lc != col) {
						continue
					}
					literal := strings.TrimSpace(label.Text)
					if len(literal) > 32 {
						continue
					}
					match := analysisRolePrefix.FindStringIndex(literal)
					if match == nil || match[0] != 0 {
						continue
					}
					current := analysisLiteralRole(literal)
					if current == "" {
						continue
					}
					anchor := archive.PhysicalAnchor{Locator: label.Locator, Component: "text", Start: 0, End: utf8.RuneCountInString(label.Text)}
					hidden := false
					for _, secret := range private {
						hidden = hidden || !archiveAnchorsDisjoint(secret, anchor)
					}
					if hidden {
						continue
					}
					if role != "" && role != current {
						return "", nil, nil
					}
					role = current
					refs = append(refs, archive.SearchEvidence{DocumentID: o.SourceDocumentID, PublicationID: o.PublicationID, PublicationRevision: o.DocumentRevision, ObservationID: o.ID, ObservationRevision: o.Revision, SourceExtractionID: o.ExtractionID, SegmentID: label.ID, Locator: label.Locator, Start: 0, End: utf8.RuneCountInString(label.Text), Excerpt: label.Text, Confirmed: true, SourceKind: "analysis_role_label"})
				}
				if role != "" {
					break
				}
			}
		}
	}
	return role, refs, nil
}

var analysisCell = regexp.MustCompile(`^([A-Z]+)([1-9][0-9]*)$`)

func analysisTablePosition(l archive.Locator) (string, int, int) {
	if l.CSV != nil {
		return "csv", l.CSV.Record, l.CSV.Column
	}
	if l.DOCX != nil && l.DOCX.Row > 0 {
		return l.DOCX.PartURI + fmt.Sprint(l.DOCX.TablePath), l.DOCX.Row, l.DOCX.GridColumn
	}
	if l.XLSX != nil {
		p := analysisCell.FindStringSubmatch(l.XLSX.Cell)
		if p != nil {
			row, _ := strconv.Atoi(p[2])
			col := 0
			for _, v := range p[1] {
				col = col*26 + int(v-'A'+1)
			}
			return l.XLSX.PartURI, row, col
		}
	}
	return "", 0, 0
}

func analysisEventDateField(name string) bool {
	switch name {
	case "date", "report_date", "event_date", "occurred_at", "transaction_date", "exam_date", "issue_date", "报告日期", "交易日期", "发生日期", "检查日期":
		return true
	}
	return false
}

// Only an immediately adjacent qualifier belongs to this numeric evidence. Never
// scan another field or line, and never turn source inequalities into exact values.
var analysisQualifier = regexp.MustCompile(`(?i)([<>≤≥≈≃~]|约|大约|大于|小于|至少|至多|不超过|不少于|approximately|about|at least|at most)[=＝]?[ \t]*$`)

var analysisPostfixQualifier = regexp.MustCompile(`^[ \t]*(左右|以上|以下|上下)`)

func analysisQualifiedValue(parsed archive.ParseResult, f archive.PublicationField) bool {
	for _, ev := range f.Evidence {
		quote := []rune(ev.Quote)
		firstDigit := 0
		for firstDigit < len(quote) && (quote[firstDigit] < '0' || quote[firstDigit] > '9') {
			firstDigit++
		}
		if analysisQualifier.MatchString(string(quote[:firstDigit])) {
			return true
		}
		for _, seg := range parsed.Segments {
			if seg.ID != ev.ParentSegmentID {
				continue
			}
			text := []rune(seg.Text)
			if ev.Start < 0 || ev.Start > len(text) {
				continue
			}
			start := ev.Start - 24
			if start < 0 {
				start = 0
			}
			if analysisQualifier.MatchString(string(text[start:ev.Start])) {
				return true
			}
			if ev.End < ev.Start || ev.End > len(text) {
				continue
			}
			end := ev.End + 24
			if end > len(text) {
				end = len(text)
			}
			if analysisPostfixQualifier.MatchString(string(text[ev.End:end])) {
				return true
			}
			// The only material allowed between a number and its suffix is its own
			// source-bound unit/currency, separated solely by horizontal whitespace.
			// Never cross a field label, punctuation, newline or another observation.
			for _, dim := range f.Dimensions {
				for _, de := range dim.Evidence {
					if de.ParentSegmentID != ev.ParentSegmentID || de.ComponentID != ev.ComponentID || de.AlternativeID != ev.AlternativeID || de.Start < ev.End || de.End > end || de.End < de.Start {
						continue
					}
					if strings.Trim(string(text[ev.End:de.Start]), " \t") != "" {
						continue
					}
					if analysisPostfixQualifier.MatchString(string(text[de.End:end])) {
						return true
					}
				}
			}
		}
	}
	return false
}

// A literal total closes a contiguous run of explicitly labelled charge rows in
// the same physical table and value column. Document/event co-location and equal
// amounts alone never establish aggregation. Missing or incompatible rows break
// the proof, so independently scoped totals remain observations.
func analysisTableAggregates(facts []archive.AnalysisFact) {
	for i := range facts {
		total := &facts[i]
		if total.Role != "total" || len(total.RoleEvidence) == 0 || len(total.Evidence) == 0 {
			continue
		}
		table, row, col := analysisTablePosition(total.Evidence[0].Locator)
		if table == "" {
			continue
		}
		members := []string{}
		refs := append([]archive.SearchEvidence{}, total.RoleEvidence...)
		for prior := row - 1; prior > 0; prior-- {
			found := -1
			for j := range facts {
				f := facts[j]
				if len(f.Evidence) == 0 || f.DocumentID != total.DocumentID {
					continue
				}
				t, r, c := analysisTablePosition(f.Evidence[0].Locator)
				if t == table && r == prior && c == col {
					if found >= 0 {
						found = -2
						break
					}
					found = j
				}
			}
			if found < 0 {
				break
			}
			f := facts[found]
			if f.Role == "total" {
				break
			}
			if !archive.CompatibleAggregateBoundary(*total, f) || f.Role != "line_item" || len(f.RoleEvidence) == 0 || f.Metric != total.Metric || f.Unit != total.Unit || f.Currency != total.Currency || f.Sample != total.Sample {
				members = nil
				break
			}
			members = append(members, f.ObservationID)
			refs = append(refs, f.RoleEvidence...)
			refs = append(refs, f.BoundaryEvidence...)
		}
		if len(members) > 0 {
			total.AggregateMembers = members
			total.AggregateEvidence = refs
		}
	}
}

// Names describe boundary axes; values come only from current visible assertions
// with field evidence. Alias normalization never infers identity from amounts.
func analysisBoundaryField(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "invoice_id", "invoice_number", "invoice_no", "bill_id", "bill_number", "bill_no", "账单号", "发票号", "发票号码":
		return "invoice"
	case "transaction_id", "transaction_number", "transaction_no", "交易号", "交易编号", "流水号":
		return "transaction"
	case "order_id", "order_number", "order_no", "订单号":
		return "order"
	case "statement_id", "statement_number", "对账单号":
		return "statement"
	case "receipt_id", "receipt_number", "receipt_no", "收据号":
		return "receipt"
	case "account_id", "account_number", "账户号", "账号":
		return "account"
	case "merchant", "vendor", "payee", "counterparty", "商户", "收款方", "交易对方":
		return "counterparty"
	}
	// A separately extracted identifier is explicit event-scope evidence even if
	// this version has no alias for its application-specific spelling.
	if strings.HasSuffix(name, "_id") || strings.HasSuffix(name, "_number") || strings.HasSuffix(name, "_no") {
		return name
	}
	return ""
}
