package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"os"
	"path/filepath"
)

// A typed human clarification is an explicit acceptance operation, never model
// input or a substitute for extracting identity evidence from source documents.
func clarify(ctx context.Context, d *store.DB, dir string, s state, verify bool) (any, error) {
	r := d.ArchiveTasks()
	rec := s.Receipts["A01"]
	if verify {
		var prior struct {
			Answer store.ArchiveAnswer
			TaskID string
		}
		readJSON(filepath.Join(dir, "clarification-operation.json"), &prior)
		task, e := r.Get(ctx, s.Actor, s.Home, rec.TaskID)
		if e != nil {
			return nil, e
		}
		_, stale := r.AnswerArchiveQuestion(ctx, s.Actor, s.Home, rec.TaskID, store.ArchiveAnswer{QuestionID: prior.Answer.QuestionID, QuestionVersion: prior.Answer.QuestionVersion, WaitingTaskVersion: prior.Answer.WaitingTaskVersion, ExtractionID: prior.Answer.ExtractionID, DocumentRevision: prior.Answer.DocumentRevision, ResolutionKey: "synthetic-stale-answer", Action: "select_subject", Target: prior.Answer.Target})
		out := map[string]any{"task": task, "same_task": task.ID == prior.TaskID, "stale_rejected": stale != nil}
		if task.ID != prior.TaskID || task.Status != domain.ArchiveTaskSucceeded || stale == nil {
			return out, fmt.Errorf("clarification did not resume same task or stale reply accepted")
		}
		return out, nil
	}
	q, e := r.ArchiveQuestion(ctx, s.Actor, s.Home, rec.TaskID)
	if e != nil {
		return nil, e
	}
	candidates, e := d.ArchiveEntities().Candidates(ctx, s.Actor, s.Home)
	if e != nil {
		return nil, e
	}
	target := ""
	for _, c := range candidates.Candidates {
		if c.Birth.Value != "1980-03-12" {
			continue
		}
		for _, name := range c.Names {
			if name.Value == "张明" {
				if target != "" && target != c.ID {
					return q, fmt.Errorf("ambiguous explicit clarification target")
				}
				target = c.ID
			}
		}
	}
	if target == "" || q.Targets[target] == 0 {
		return q, fmt.Errorf("source-derived clarification target missing")
	}
	answer := store.ArchiveAnswer{QuestionID: q.ID, QuestionVersion: q.Version, WaitingTaskVersion: q.WaitingTaskVersion, ExtractionID: q.ExtractionID, DocumentRevision: q.DocumentRevision, ResolutionKey: "synthetic-human-clarification", Action: "select_subject", Target: target}
	next, e := r.AnswerArchiveQuestion(ctx, s.Actor, s.Home, rec.TaskID, answer)
	out := map[string]any{"Question": q, "Answer": answer, "TaskID": rec.TaskID, "next": next, "synthetic_human_answer": "归属已核实出生日期1980-03-12的张明"}
	if e == nil {
		writeJSON(filepath.Join(dir, "clarification-operation.json"), out)
	}
	return out, e
}

func privateSubcontent(ctx context.Context, d *store.DB, s state) (any, error) {
	r := d.ArchiveTasks()
	doc := s.Receipts["P03"].DocumentID
	obs, e := r.CurrentArchiveObservations(ctx, s.Actor, s.Home, doc, false)
	if e != nil {
		return nil, e
	}
	if len(obs) == 0 {
		return nil, fmt.Errorf("contract has no current observations")
	}
	selected := obs[0]
	revision, e := r.ReviseArchive(ctx, s.Actor, s.Home, doc, selected.DocumentRevision, store.ArchiveRevision{Kind: "private", ObservationID: selected.ID, Reason: "synthetic subcontent privacy check"})
	if e != nil {
		return nil, e
	}
	_, publicError := r.ArchiveOriginalReference(ctx, s.Actor, s.Home, doc, true)
	_, authorizedError := r.ArchiveOriginalReference(ctx, s.Actor, s.Home, doc, false)
	public, e := r.CurrentArchiveObservations(ctx, s.Actor, s.Home, doc, true)
	if e != nil {
		return nil, e
	}
	leaked := false
	for _, o := range public {
		if o.ID == selected.ID {
			leaked = true
		}
	}
	var restricted bool
	e = d.SQL().QueryRowContext(ctx, `SELECT original_restricted FROM archive_documents WHERE household_id=? AND id=?`, s.Home, doc).Scan(&restricted)
	if e != nil {
		return nil, e
	}
	out := map[string]any{"document_id": doc, "private_observation": selected.ID, "revision": revision, "original_restricted": restricted, "public_original_denied": publicError != nil, "authorized_original_available": authorizedError == nil, "public_observations": public}
	if !restricted || publicError == nil || authorizedError != nil || leaked {
		return out, fmt.Errorf("subcontent privacy did not protect whole original")
	}
	return out, nil
}

type corpusIdentity struct {
	Document     string
	SHA          string
	Events       []string
	Observations []string
	Extractions  []string
	Sources      int
	Private      bool
	Products     map[string]string
}

func identity(ctx context.Context, d *store.DB, s state, id string) (corpusIdentity, error) {
	out := corpusIdentity{Document: s.Receipts[id].DocumentID, Products: map[string]string{}}
	e := d.SQL().QueryRowContext(ctx, `SELECT blob_sha256,tv_visible=0 FROM archive_documents WHERE household_id=? AND id=?`, s.Home, out.Document).Scan(&out.SHA, &out.Private)
	if e != nil {
		return out, e
	}
	for _, spec := range []struct {
		query string
		dst   *[]string
	}{{`SELECT id FROM archive_subject_events WHERE household_id=? AND document_id=? ORDER BY id`, &out.Events}, {`SELECT DISTINCT o.id FROM archive_observations o JOIN archive_subject_events e ON e.household_id=o.household_id AND e.id=o.event_id WHERE e.household_id=? AND e.document_id=? ORDER BY o.id`, &out.Observations}, {`SELECT id FROM archive_extractions WHERE household_id=? AND document_id=? ORDER BY id`, &out.Extractions}} {
		rows, e := d.SQL().QueryContext(ctx, spec.query, s.Home, out.Document)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var v string
			must(rows.Scan(&v))
			*spec.dst = append(*spec.dst, v)
		}
		e = rows.Err()
		_ = rows.Close() // Read-only query cursor cleanup.
		if e != nil {
			return out, e
		}
	}
	rows, e := d.SQL().QueryContext(ctx, `SELECT p.task_id,p.product_key,p.fingerprint,p.digest FROM archive_products p JOIN archive_sources s ON s.household_id=p.household_id AND s.task_id=p.task_id WHERE s.household_id=? AND s.document_id=?`, s.Home, out.Document)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var task, key, fp, digest string
		must(rows.Scan(&task, &key, &fp, &digest))
		out.Products[task+"/"+key+"/"+fp] = digest
	}
	_ = rows.Close() // Read-only query cursor cleanup.
	var mismatch int
	e = d.SQL().QueryRowContext(ctx, `SELECT count(*),coalesce(sum(sha256<>?),0) FROM archive_sources WHERE household_id=? AND document_id=?`, out.SHA, s.Home, out.Document).Scan(&out.Sources, &mismatch)
	if e != nil {
		return out, e
	}
	if mismatch > 0 {
		return out, fmt.Errorf("source hash mismatch")
	}
	return out, nil
}
func duplicateIdentity(ctx context.Context, d *store.DB, dir string, s state, verify bool) (any, error) {
	current := map[string]corpusIdentity{}
	for _, id := range []string{"H01", "H03"} {
		v, e := identity(ctx, d, s, id)
		if e != nil {
			return nil, e
		}
		current[id] = v
	}
	if !verify {
		writeJSON(filepath.Join(dir, "duplicate-before.json"), current)
		return current, nil
	}
	var before map[string]corpusIdentity
	readJSON(filepath.Join(dir, "duplicate-before.json"), &before)
	equal := func(a, b any) bool { x, _ := json.Marshal(a); y, _ := json.Marshal(b); return string(x) == string(y) }
	for id, old := range before {
		now := current[id]
		if now.Document != old.Document || now.SHA != old.SHA || !equal(now.Events, old.Events) || !equal(now.Observations, old.Observations) || now.Sources != old.Sources+2 || len(now.Extractions) <= len(old.Extractions) || now.Private != old.Private {
			return current, fmt.Errorf("reingestion identity/version/privacy mismatch: %s", id)
		}
		for k, v := range old.Products {
			if now.Products[k] != v {
				return current, fmt.Errorf("old immutable product changed")
			}
		}
	}
	return map[string]any{"before": before, "after": current, "external_transport": false}, nil
}

func formula(ctx context.Context, blobs *archive.BlobStore, s state, dir string) (any, error) {
	if len(os.Args) != 5 {
		return nil, fmt.Errorf("formula requires synthetic fixtures directory")
	}
	var m struct {
		Synthetic bool      `json:"synthetic_only"`
		Fixtures  []fixture `json:"fixtures"`
	}
	readJSON(filepath.Join(os.Args[4], "manifest.json"), &m)
	if !m.Synthetic {
		return nil, fmt.Errorf("synthetic manifest required")
	}
	var out archive.ParseResult
	for _, f := range m.Fixtures {
		if f.ID != "F01-missing" {
			continue
		}
		if filepath.Base(f.File) != f.File {
			return nil, fmt.Errorf("invalid fixture filename")
		}
		file, e := os.Open(filepath.Join(os.Args[4], f.File)) // #nosec G703 -- The synthetic manifest is operator-selected; the filename is checked above to be a single basename, matching intake.
		if e != nil {
			return nil, e
		}
		ref, e := blobs.Put(ctx, s.Home, file, archive.PutOptions{})
		_ = file.Close() // Read-only source cleanup; read errors are handled separately.
		if e != nil {
			return nil, e
		}
		if ref.SHA256 != f.SHA256 {
			return nil, fmt.Errorf("fixture hash mismatch")
		}
		original, e := blobs.OpenVerified(ctx, ref)
		if e != nil {
			return nil, e
		}
		out, e = archive.Parse(ctx, archive.ParseSource{Reader: original, Size: ref.SizeBytes, SHA256: ref.SHA256, FormatHint: "xlsx"}, archive.DefaultParseLimits())
		_ = original.Close() // Read-only source cleanup; read errors are handled separately.
		if e != nil {
			return out, e
		}
		break
	}
	found := false
	for _, seg := range out.Segments {
		if seg.Locator.XLSX != nil && seg.Locator.XLSX.Cell == "B4" {
			if seg.Cell != nil && seg.Cell.Formula != nil && seg.Cell.Formula.Text == "SUM(B2:B3)" && !seg.Cell.Formula.CachePresent && seg.Cell.Formula.CachedRaw == "" {
				found = true
			}
		}
	}
	if !found {
		return out, fmt.Errorf("missing formula cache status not established")
	}
	return out, nil
}
