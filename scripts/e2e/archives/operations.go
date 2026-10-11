package main

import (
	"context"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/store"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// These checks inspect the real accepted corpus. They never supply classification,
// identity, fields or model answers. Destructive checks run explicitly after queries.
func operations(ctx context.Context, d *store.DB, blobs *archive.BlobStore, dir string, s state, phase string) {
	r := d.ArchiveTasks()
	key := []byte("synthetic-e2e-cursor-secret-at-least32")
	results := map[string]any{}
	check := func(id string, run func() (any, error)) {
		evidence, e := run()
		status := "pass"
		reason := ""
		if e != nil {
			status = "fail"
			reason = e.Error()
		}
		results[id] = map[string]any{"status": status, "evidence": evidence, "reason": reason}
	}
	search := func(public bool, f archive.SearchFilter) (archive.SearchPage, error) {
		return r.SearchArchives(ctx, s.Actor, s.Home, public, f, key)
	}
	allPages := func(public bool, f archive.SearchFilter) (archive.SearchPage, error) {
		var result archive.SearchPage
		seen := map[string]bool{}
		for i := 0; i < 100; i++ {
			p, e := search(public, f)
			if e != nil {
				return result, e
			}
			for _, item := range p.Items {
				if seen[item.DocumentID] {
					return result, fmt.Errorf("duplicate baseline document")
				}
				seen[item.DocumentID] = true
				result.Items = append(result.Items, item)
			}
			if p.NextCursor == "" {
				return result, nil
			}
			f.Cursor = p.NextCursor
		}
		return result, fmt.Errorf("page limit exceeded")
	}

	if phase == "checks" {
		check("37", func() (any, error) {
			base, e := allPages(false, archive.SearchFilter{Limit: 25})
			if e != nil {
				return nil, e
			}
			ids := map[string]bool{}
			cursor := ""
			pages := []archive.SearchPage{}
			for i := 0; i < 100; i++ {
				p, e := search(false, archive.SearchFilter{Limit: 2, Cursor: cursor})
				if e != nil {
					return pages, e
				}
				pages = append(pages, p)
				for _, v := range p.Items {
					if ids[v.DocumentID] {
						return pages, fmt.Errorf("duplicate document")
					}
					ids[v.DocumentID] = true
				}
				if i == 0 && p.NextCursor != "" {
					_, e = search(false, archive.SearchFilter{Limit: 2, Cursor: p.NextCursor, Domain: "health"})
					if e == nil {
						return pages, fmt.Errorf("changed filter accepted old cursor")
					}
				}
				cursor = p.NextCursor
				if cursor == "" {
					break
				}
			}
			if cursor != "" || len(ids) != len(base.Items) {
				return pages, fmt.Errorf("pagination corpus mismatch")
			}
			return pages, nil
		})
		check("38", func() (any, error) {
			out := map[string]archive.SearchPage{}
			for _, q := range []string{`?`, `"`, `张明 OR *`, `NEAR(体检,报告)`, `" OR 1=1 --`} {
				p, e := search(true, archive.SearchFilter{Query: q, Limit: 25})
				if e != nil {
					return out, e
				}
				out[q] = p

				for _, v := range p.Items {
					literal := false
					for _, ev := range v.Evidence {
						if strings.Contains(ev.Excerpt, q) {
							literal = true
						}
					}
					if !literal {
						return out, fmt.Errorf("literal match lacks source evidence: %q", q)
					}
					if v.DocumentID == s.Receipts["H03"].DocumentID || v.DocumentID == s.Receipts["F04"].DocumentID {
						return out, fmt.Errorf("private document leaked")
					}
				}
			}
			return out, nil
		})
		check("39", func() (any, error) {
			out := map[string]any{}
			for _, id := range []string{"A02-encrypted", "A02-corrupt", "A02-unknown"} {
				rec := s.Receipts[id]
				ref, e := r.ArchiveOriginalReference(ctx, s.Actor, s.Home, rec.DocumentID, false)
				if e != nil {
					return out, e
				}
				original, e := blobs.OpenVerified(ctx, ref)
				if e != nil {
					return out, e
				}
				n, e := io.Copy(io.Discard, original)
				original.Close()
				if e != nil || n != ref.SizeBytes {
					return out, fmt.Errorf("original hash/size verification failed for %s", id)
				}
				p, e := r.ArchiveEvidence(ctx, s.Actor, s.Home, false, rec.DocumentID, archive.SearchFilter{}, key)
				if e != nil {
					return out, e
				}
				out[id] = map[string]any{"original": ref, "current": p}
				if len(p.Items) != 1 || len(p.Items[0].Evidence) != 0 || !p.Items[0].OriginalSaved || (p.Items[0].ParseStatus != "unsupported" && p.Items[0].ParseStatus != "invalid" && p.Items[0].ParseStatus != "encrypted" && p.Items[0].ParseStatus != "limit_exceeded") {
					return out, fmt.Errorf("saved/processing status not separated for %s", id)
				}
			}
			return out, nil
		})
		check("33", func() (any, error) {
			p, e := allPages(true, archive.SearchFilter{Limit: 25})
			if e != nil {
				return p, e
			}
			for _, v := range p.Items {
				if v.DocumentID == s.Receipts["H03"].DocumentID || v.DocumentID == s.Receipts["F04"].DocumentID {
					return p, fmt.Errorf("private document in TV projection")
				}
			}
			a, e := r.AnalyzeArchives(ctx, s.Actor, s.Home, true, archive.AnalysisQuery{Operation: "sum", Metric: "amount", Timezone: "Asia/Singapore"})
			out := map[string]any{"public": p, "analysis": a}
			if e != nil {
				return out, e
			}
			found := false
			for _, g := range a.Groups {
				if g.Currency == "CNY" && g.Value == "200.00" {
					found = true
				}
			}
			if !found {
				return out, fmt.Errorf("public CNY 200.00 not established")
			}
			return out, nil
		})
	} else if phase == "withdraw" {
		check("36", func() (any, error) {
			doc := s.Receipts["H01"].DocumentID
			before, e := r.ArchiveEvidence(ctx, s.Actor, s.Home, false, doc, archive.SearchFilter{}, key)
			if e != nil {
				return before, e
			}
			if len(before.Items) != 1 {
				return before, fmt.Errorf("H01 unavailable")
			}
			var history int
			must(d.SQL().QueryRow(`SELECT count(*) FROM archive_observation_revisions WHERE household_id=?`, s.Home).Scan(&history))
			rev, e := r.ReviseArchive(ctx, s.Actor, s.Home, doc, before.Items[0].DocumentRevision, store.ArchiveRevision{Kind: "withdraw", Reason: "synthetic acceptance withdrawal"})
			if e != nil {
				return before, e
			}
			after, e := allPages(false, archive.SearchFilter{Limit: 25})
			if e != nil {
				return after, e
			}
			for _, v := range after.Items {
				if v.DocumentID == doc {
					return after, fmt.Errorf("withdrawn current document leaked")
				}
			}
			var current int
			must(d.SQL().QueryRow(`SELECT count(*) FROM archive_observation_revisions WHERE household_id=?`, s.Home).Scan(&current))
			if current != history {
				return after, fmt.Errorf("immutable audit history changed")
			}
			return map[string]any{"before": before, "after": after, "revision": rev, "history_count": history}, nil
		})
	} else {
		panic("unknown controlled operation phase")
	}
	writeJSON(filepath.Join(dir, fmt.Sprintf("operations-%s-%d.json", phase, time.Now().UnixNano())), results)
	for id, v := range results {
		fmt.Println(id, v.(map[string]any)["status"])
	}
}
