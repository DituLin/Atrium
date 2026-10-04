// Package archiveworker composes parser/understanding with the transactional
// repository. The archive package itself never imports store.
package archiveworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"path/filepath"
	"strings"
	"time"
)

type Worker struct {
	Tasks                  *store.ArchiveTasks
	Blobs                  *archive.BlobStore
	Household, Fingerprint string
	Lease                  time.Duration
	Native                 archive.NativeParser
	Adapter                archive.OpenClawUnderstandingAdapter
	understand             func(context.Context, archive.UnderstandingPlan, archive.UnderstandingChunk) (archive.ChunkUnderstanding, error)
}

func (w *Worker) RunOne(ctx context.Context) (bool, error) {
	if w.Fingerprint == "" || w.understand == nil {
		fp, e := w.ConfigurationFingerprint()
		if e != nil {
			return false, e
		}
		w.Fingerprint = fp
	}
	ttl := w.Lease
	if ttl <= 0 {
		ttl = time.Minute
	}
	if _, e := w.Tasks.RefreshStaleArchiveQuestions(ctx, w.Household, 32); e != nil {
		return false, e
	}
	task, e := w.Tasks.ClaimKinds(ctx, w.Household, ttl, []string{"document.extract"})
	if e != nil || task == nil {
		return false, e
	}
	f := store.ArchiveWorkFence{HouseholdID: w.Household, TaskID: task.ID, Token: task.LeaseToken, StateVersion: task.StateVersion}
	// Renew independently while parsers/provider are blocked. Revocation or lease
	// loss cancels the process group through the existing adapter context.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopped := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(ttl / 3)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				if _, e := w.Tasks.Renew(ctx, f.HouseholdID, f.TaskID, f.Token, ttl); e != nil {
					cancel()
					return
				}
			case <-stopped:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() { close(stopped); <-done }()
	update := func(t *domain.ArchiveTask) { task = t; f.StateVersion = t.StateVersion }
	fail := func(code string, retry bool) error {
		u := domain.ArchiveTaskUpdate{ExpectedStateVersion: f.StateVersion, Stage: "processing", StateJSON: task.StateJSON, ResultJSON: json.RawMessage(`{}`), LastErrorCode: code}
		if retry {
			_, e := w.Tasks.Retry(ctx, f.HouseholdID, f.TaskID, f.Token, u, time.Now().Add(time.Minute))
			return e
		}
		_, e := w.Tasks.Finish(ctx, f.HouseholdID, f.TaskID, f.Token, domain.ArchiveTaskFailed, u)
		return e
	}
	x, next, e := w.Tasks.BeginArchiveExtraction(ctx, f, w.Fingerprint)
	if errors.Is(e, domain.ErrConflict) {
		x, next, e = w.Tasks.RebaseArchiveExtraction(ctx, f, w.Fingerprint)
	}
	if e != nil {
		return true, e
	}
	update(next)
	products, e := w.Tasks.ArchiveProducts(ctx, f, w.Fingerprint)
	if e != nil {
		return true, e
	}
	saved := map[string]json.RawMessage{}
	for _, p := range products {
		saved[p.Key] = p.Body
	}
	save := func(key string, value any, invocation string) error {
		b, e := json.Marshal(value)
		if e != nil {
			return e
		}
		t, e := w.Tasks.SaveProductAndCheckpoint(ctx, f, store.ArchiveProduct{Key: key, Fingerprint: w.Fingerprint, Body: b, InvocationID: invocation})
		if e == nil {
			update(t)
			saved[key] = b
		}
		return e
	}
	var parsed archive.ParseResult
	if raw := saved["parse"]; raw != nil {
		if e = json.Unmarshal(raw, &parsed); e != nil {
			return true, e
		}
	} else {
		ref, hint, e := w.Tasks.ArchiveWorkSource(ctx, f)
		hint = strings.TrimPrefix(strings.ToLower(filepath.Ext(hint)), ".")
		if e != nil {
			return true, e
		}
		file, e := w.Blobs.OpenVerified(ctx, ref)
		if e != nil {
			return true, fail("original_unavailable", false)
		}
		parsed, e = archive.Parse(ctx, archive.ParseSource{Reader: file, Size: ref.SizeBytes, SHA256: ref.SHA256, FormatHint: hint}, archive.DefaultParseLimits())
		if e == nil && parsed.Status == archive.ParseUnsupported && (strings.HasPrefix(ref.MIME, "image/") || ref.MIME == "application/pdf") {
			parsed, e = w.Native.Parse(ctx, archive.NativeSource{File: file, SHA256: ref.SHA256})
		}
		file.Close()
		if e != nil {
			return true, fail("parse_failed", false)
		}
		if e = save("parse", parsed, ""); e != nil {
			return true, e
		}
	}
	plan, e := archive.PlanChunks(parsed)
	if e != nil {
		return true, fail("parse_status_"+string(parsed.Status), false)
	}
	if saved["plan"] == nil {
		if e = save("plan", plan, ""); e != nil {
			return true, e
		}
	}
	chunks := []archive.ChunkUnderstanding{}
	for _, chunk := range plan.Chunks {
		key := fmt.Sprintf("chunk-%03d", chunk.Index)
		var result archive.ChunkUnderstanding
		if raw := saved[key]; raw != nil {
			if e = json.Unmarshal(raw, &result); e != nil {
				return true, e
			}
		} else {
			inv, next, e := w.Tasks.ReserveArchiveInvocation(ctx, f, key, 1, 512)
			if e != nil {
				return true, fail("call_budget_exhausted", false)
			}
			update(next)
			if w.understand != nil {
				result, e = w.understand(ctx, plan, chunk)
			} else {
				result, e = archive.UnderstandChunk(ctx, plan, chunk, w.Adapter)
			}
			if e != nil {
				return true, fail("understanding_failed", true)
			}
			if e = save(key, result, inv.ID); e != nil {
				return true, e
			}
		}
		chunks = append(chunks, result)
	}
	merged, e := archive.MergeChunks(plan, chunks)
	if e != nil {
		return true, fail("understanding_invalid", false)
	}
	if saved["publication"] == nil {
		if e = save("publication", store.ArchivePublicationBundle{Parsed: parsed, Plan: plan, Understanding: merged}, ""); e != nil {
			return true, e
		}
	}
	_, e = w.Tasks.PublishExtraction(ctx, f, x.ID)
	if errors.Is(e, domain.ErrConflict) {
		x, next, rebaseErr := w.Tasks.RebaseArchiveExtraction(ctx, f, w.Fingerprint)
		if rebaseErr != nil {
			return true, rebaseErr
		}
		update(next)
		_, e = w.Tasks.PublishExtraction(ctx, f, x.ID)
	}
	return true, e
}
