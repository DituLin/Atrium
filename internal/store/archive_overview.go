package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
)

const archiveOverviewDocumentLimit = 1000
const archiveOverviewByteLimit = 1 << 20
const archiveOverviewEntityLimit = 256
const archiveOverviewEntityIDByteLimit = 128
const archiveOverviewIdentityByteLimit = 64 << 10
const archiveOverviewIdentityReadLimit = 1024
const archiveOverviewRedirectDepthLimit = 64

// ArchiveOverviewForScreen reads the current public document projection in the
// same bounded, read-only snapshot as screen authorization and index generation.
// An incomplete index never supplies stale classification or attribution. Total
// remains the exact visible document count; uninspectable documents stay pending.
func (r *ArchiveTasks) ArchiveOverviewForScreen(ctx context.Context, screen, hash, h string) (archive.Overview, error) {
	out := archive.Overview{Domains: []archive.OverviewDomain{}, Entities: []archive.OverviewEntity{}, Coverage: archive.SearchCoverage{Complete: true}}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := r.db.InReadTx(ctx, func(tx *sql.Tx) error {
		if _, err := r.authorizeSearchIn(ctx, tx, archiveSearchActor{ScreenID: screen, ScreenHash: hash}, h); err != nil {
			return err
		}
		var epoch, generation int64
		if err := tx.QueryRowContext(ctx, `SELECT epoch,generation FROM archive_search_state WHERE household_id=?`, h).Scan(&epoch, &generation); err != nil {
			return err
		}
		out.Generation = fmt.Sprintf("%d:%d", epoch, generation)
		// Legacy records are counted at their current leaf, even while the
		// disposable legacy projection is pending or still names an older leaf.
		if err := tx.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM archive_documents d WHERE d.household_id=? AND d.withdrawn=0 AND d.tv_visible=1)+
(SELECT count(*) FROM family_records f JOIN local_household l ON l.singleton=1 WHERE l.household_id=? AND json_extract(f.payload_json,'$.retracted') IS NOT 1 AND json_extract(f.payload_json,'$.display_on_tv')=1 AND NOT EXISTS(SELECT 1 FROM family_records newer WHERE newer.supersedes_id=f.id))`, h, h).Scan(&out.Total); err != nil {
			return err
		}
		out.Pending = out.Total
		// Avoid presenting an arbitrary first subset as complete household
		// statistics. No document bodies are loaded above this fixed boundary.
		if out.Total > archiveOverviewDocumentLimit {
			out.Coverage = archive.SearchCoverage{Complete: false, Reason: "scan_budget"}
			return nil
		}
		identity, complete, err := archiveOverviewVisibleEntitiesIn(ctx, tx, h)
		if err != nil {
			return err
		}
		if !complete {
			out.Coverage = archive.SearchCoverage{Complete: false, Reason: "entity_budget"}
			return nil
		}
		rows, err := tx.QueryContext(ctx, `SELECT * FROM (
SELECT d.id,CASE WHEN s.id IS NULL THEN '' WHEN length(s.body)>65536 THEN '#' ELSE json_remove(s.body,'$.evidence','$.dates') END AS body,'' AS legacy_entity,'' AS legacy_record
FROM archive_documents d LEFT JOIN archive_search_rows s ON s.household_id=d.household_id AND s.document_id=d.id AND s.public=1 AND s.epoch=? AND s.generation=? WHERE d.household_id=? AND d.withdrawn=0 AND d.tv_visible=1
UNION ALL
SELECT 'legacy:'||f.id,substr(f.payload_json,1,65537),coalesce((SELECT p.entity_id FROM archive_legacy_projections p WHERE p.household_id=? AND p.current_record_id=f.id AND p.active=1 ORDER BY p.root_record_id LIMIT 1),''),f.id
FROM family_records f JOIN local_household l ON l.singleton=1 WHERE l.household_id=? AND json_extract(f.payload_json,'$.retracted') IS NOT 1 AND json_extract(f.payload_json,'$.display_on_tv')=1 AND NOT EXISTS(SELECT 1 FROM family_records newer WHERE newer.supersedes_id=f.id)
) ORDER BY id LIMIT 1001`, epoch, generation, h, h, h)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }() // Read-only query cursor cleanup.
		domains, entities := map[string]int{}, map[string]int{}
		bytes := 0
		for rows.Next() {
			var id, body, legacyEntity, legacyRecord string
			if err = rows.Scan(&id, &body, &legacyEntity, &legacyRecord); err != nil {
				return err
			}
			bytes += len(id) + len(body) + len(legacyEntity) + len(legacyRecord)
			if bytes+identity.bytes > archiveOverviewByteLimit || len(body) > 65536 || body == "#" {
				// Partial accumulators are discarded. Confirmed must never imply
				// that documents beyond the bounded read were inspected.
				out.Coverage = archive.SearchCoverage{Complete: false, Reason: "scan_budget"}
				out.Confirmed = 0
				out.Ownership = archive.OverviewOwnership{}
				return nil
			}
			item := metadataSearchItem(id, 1)
			indexed := body != "" && (legacyRecord == "" || legacyEntity != "")
			if !indexed {
				out.Coverage = archive.SearchCoverage{Complete: false, Reason: "index_pending"}
			} else if legacyRecord != "" {
				canonical, complete, e := identity.canonicalIn(ctx, tx, h, legacyEntity)
				if e != nil {
					return e
				}
				if !complete || bytes+identity.bytes > archiveOverviewByteLimit {
					out.Coverage = archive.SearchCoverage{Complete: false, Reason: "entity_budget"}
					out.Confirmed = 0
					out.Ownership = archive.OverviewOwnership{}
					return nil
				}
				item, _, err = legacySearchItem(id, canonical, legacyRecord, body)
				if err != nil {
					return err
				}
			} else if err = json.Unmarshal([]byte(body), &item); err != nil {
				return err
			}
			if item.Truncated && out.Coverage.Complete {
				out.Coverage = archive.SearchCoverage{Complete: false, Reason: "projection_partial"}
			}
			publicEntities := map[string]bool{}
			for _, entity := range item.EntityIDs {
				if identity.visible[entity] {
					publicEntities[entity] = true
				}
			}
			for entity := range publicEntities {
				entities[entity]++
			}
			// Attribution to an entity whose labels are entirely private must
			// neither expose its ID nor count as publicly confirmed ownership.
			ownership := item.Ownership
			if ownership == "confirmed" && len(publicEntities) == 0 {
				ownership = "unassigned"
			}
			domain := item.Domain
			switch domain {
			case "health", "finance", "education", "property", "other", "unknown":
			default:
				domain = "unknown"
			}
			domains[domain]++
			if indexed {
				switch ownership {
				case "household":
					out.Ownership.Household++
				case "waiting":
					out.Ownership.Waiting++
				case "unassigned":
					out.Ownership.Unassigned++
				}
				if !item.MetadataOnly && !item.ReviewRequired && !item.Truncated && item.ParseStatus == "parsed" && (ownership == "confirmed" || ownership == "household") {
					out.Confirmed++
				}
			}
		}
		if err = rows.Err(); err != nil {
			return err
		}
		out.Pending = out.Total - out.Confirmed
		for _, domain := range []string{"health", "finance", "education", "property", "other", "unknown"} {
			if count := domains[domain]; count > 0 {
				out.Domains = append(out.Domains, archive.OverviewDomain{Domain: domain, Count: count})
			}
		}
		for id, count := range entities {
			out.Entities = append(out.Entities, archive.OverviewEntity{ID: id, Count: count})
		}
		sort.Slice(out.Entities, func(i, j int) bool { return out.Entities[i].ID < out.Entities[j].ID })
		return nil
	})
	return out, err
}

// The overview only needs the existence of a public name, never label values,
// aliases, precision or evidence payloads. Keep the same current provenance and
// privacy predicates as entity search, but select bounded IDs only.
func archiveOverviewVisibleEntitiesIn(ctx context.Context, tx *sql.Tx, h string) (*archiveOverviewEntityProjection, bool, error) {
	out := &archiveOverviewEntityProjection{visible: map[string]bool{}, canonical: map[string]string{}}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT CASE WHEN length(CAST(a.entity_id AS BLOB))<=128 THEN a.entity_id ELSE '' END,length(CAST(a.entity_id AS BLOB))
FROM archive_entity_attributes a WHERE a.household_id=? AND a.kind='name' AND a.conflict=0
AND (a.link_id IS NULL OR EXISTS(SELECT 1 FROM archive_entity_links l WHERE l.household_id=a.household_id AND l.id=a.link_id AND l.status='linked')) AND (
(a.document_id IS NOT NULL AND EXISTS(SELECT 1 FROM archive_documents d WHERE d.household_id=a.household_id AND d.id=a.document_id AND d.withdrawn=0 AND d.tv_visible=1 AND d.original_restricted=0 AND a.tv_visible=1) AND NOT EXISTS(SELECT 1 FROM archive_protected_identity_facts p WHERE p.household_id=a.household_id AND p.document_id=a.document_id AND instr(a.value,p.value)>0))
OR (a.legacy_member_id IS NOT NULL AND EXISTS(SELECT 1 FROM local_household l WHERE l.singleton=1 AND l.household_id=a.household_id) AND EXISTS(SELECT 1 FROM family_records f WHERE json_extract(f.payload_json,'$.member_id')=a.legacy_member_id AND json_extract(f.payload_json,'$.display_on_tv')=1 AND json_extract(f.payload_json,'$.retracted') IS NOT 1 AND NOT EXISTS(SELECT 1 FROM family_records n WHERE n.supersedes_id=f.id))))
ORDER BY 1 LIMIT 257`, h)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }() // Read-only query cursor cleanup.
	count := 0
	for rows.Next() {
		var source string
		var size int
		if err = rows.Scan(&source, &size); err != nil {
			return nil, false, err
		}
		count++
		out.bytes += size
		if count > archiveOverviewEntityLimit || size > archiveOverviewEntityIDByteLimit || out.bytes > archiveOverviewIdentityByteLimit {
			return out, false, nil
		}
		id, complete, err := out.canonicalIn(ctx, tx, h, source)
		if err != nil || !complete {
			return out, complete, err
		}
		out.visible[id] = true
	}
	return out, true, rows.Err()
}

// Identity reads have a separate bounded budget, included in the overview's
// overall byte budget. Redirects use the same limit rather than loading an
// arbitrarily long target ID through the general-purpose identity resolver.
type archiveOverviewEntityProjection struct {
	visible   map[string]bool
	canonical map[string]string
	bytes     int
	reads     int
}

func (p *archiveOverviewEntityProjection) canonicalIn(ctx context.Context, tx *sql.Tx, h, id string) (string, bool, error) {
	path := []string{}
	seen := map[string]bool{}
	for depth := 0; depth < archiveOverviewRedirectDepthLimit; depth++ {
		if canonical, ok := p.canonical[id]; ok {
			for _, source := range path {
				p.canonical[source] = canonical
			}
			return canonical, true, nil
		}
		if len(id) > archiveOverviewEntityIDByteLimit || p.bytes > archiveOverviewIdentityByteLimit || p.reads >= archiveOverviewIdentityReadLimit {
			return "", false, nil
		}
		if seen[id] {
			return "", false, domain.ErrConflict
		}
		seen[id] = true
		path = append(path, id)
		var redirected bool
		var target string
		var size int
		p.reads++
		err := tx.QueryRowContext(ctx, `SELECT r.target_id IS NOT NULL,CASE WHEN length(CAST(r.target_id AS BLOB))<=128 THEN r.target_id ELSE '' END,coalesce(length(CAST(r.target_id AS BLOB)),0) FROM archive_entities e LEFT JOIN archive_entity_redirects r ON r.household_id=e.household_id AND r.source_id=e.id WHERE e.household_id=? AND e.id=?`, h, id).Scan(&redirected, &target, &size)
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, domain.ErrNotFound
		}
		if err != nil {
			return "", false, err
		}
		p.bytes += size
		if size > archiveOverviewEntityIDByteLimit || p.bytes > archiveOverviewIdentityByteLimit {
			return "", false, nil
		}
		if !redirected {
			for _, source := range path {
				p.canonical[source] = id
			}
			return id, true, nil
		}
		id = target
	}
	return "", false, nil
}
