package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/archiveworker"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"os"
	"path/filepath"
	"time"
)

type restartState struct {
	Receipt                  domain.ArchiveIntakeReceipt
	Fence                    store.ArchiveWorkFence
	Extraction               store.ArchiveExtraction
	Fingerprint, ParseDigest string
}

func prepareRestart(ctx context.Context, d *store.DB, blobs *archive.BlobStore, dir string, s state, revoke bool) (any, error) {
	// The copied baseline must be quiescent, so the one claimed task is known.
	var active int
	e := d.SQL().QueryRowContext(ctx, `SELECT count(*) FROM archive_tasks WHERE household_id=? AND status IN ('queued','running','retry_wait')`, s.Home).Scan(&active)
	if e != nil {
		return nil, e
	}
	if active != 0 {
		return nil, fmt.Errorf("quiescent snapshot required")
	}
	r := d.ArchiveTasks()
	ref, e := r.ArchiveOriginalReference(ctx, s.Actor, s.Home, s.Receipts["H04a"].DocumentID, false)
	if e != nil {
		return nil, e
	}
	metadata := domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: domain.ArchiveInputText, Channel: "synthetic-restart", Account: "test", Conversation: "test", Sender: "test", ProviderMessageID: "restart-new-event"}, OriginalName: "H04-restart.txt", VisibilityIntent: "inherit"}
	policy, e := r.IntakePreflight(ctx, s.Actor, s.Home, metadata)
	if e != nil {
		return nil, e
	}
	rec, e := r.AcceptIntake(ctx, s.Actor, s.Home, policy, metadata, ref)
	if e != nil {
		return nil, e
	}
	ttl := time.Second
	if revoke {
		ttl = time.Minute
	}
	root, e := os.Getwd()
	if e != nil {
		return nil, e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	worker := archiveworker.Worker{Native: archive.NativeParser{HelperPath: "/tmp/atriumos-t14-preview/helper"}, Adapter: archive.OpenClawUnderstandingAdapter{NodePath: filepath.Join(dir, "capture-node.py"), OpenClawPath: "/opt/homebrew/lib/node_modules/openclaw/openclaw.mjs", PluginPath: filepath.Join(root, "adapters/openclaw/archive-worker-plugin"), ProviderPath: filepath.Join(home, ".openclaw.pre-migration/npm/projects/openclaw-deepseek-provider-2481ed984b/node_modules/@openclaw/deepseek-provider")}}
	fp, e := worker.ConfigurationFingerprint()
	if e != nil {
		return nil, e
	}
	task, e := r.ClaimKinds(ctx, s.Home, ttl, []string{"document.extract"})
	if e != nil {
		return nil, e
	}
	if task == nil || task.ID != rec.TaskID {
		return nil, fmt.Errorf("unexpected claimed task")
	}
	fence := store.ArchiveWorkFence{HouseholdID: s.Home, TaskID: task.ID, Token: task.LeaseToken, StateVersion: task.StateVersion}
	extraction, next, e := r.BeginArchiveExtraction(ctx, fence, fp)
	if e != nil {
		return nil, e
	}
	fence.StateVersion = next.StateVersion
	source, e := blobs.OpenVerified(ctx, ref)
	if e != nil {
		return nil, e
	}
	parsed, e := archive.Parse(ctx, archive.ParseSource{Reader: source, Size: ref.SizeBytes, SHA256: ref.SHA256, FormatHint: "txt"}, archive.DefaultParseLimits())
	_ = source.Close() // Read-only source cleanup; read errors are handled separately.
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(parsed)
	if e != nil {
		return nil, e
	}
	next, e = r.SaveProductAndCheckpoint(ctx, fence, store.ArchiveProduct{Key: "parse", Fingerprint: fp, Body: raw})
	if e != nil {
		return nil, e
	}
	fence.StateVersion = next.StateVersion
	saved := restartState{Receipt: *rec, Fence: fence, Extraction: extraction, Fingerprint: fp, ParseDigest: ref.SHA256}
	writeJSON(filepath.Join(dir, "restart-private-state.json"), saved)
	if !revoke {
		return map[string]any{"task_id": task.ID, "document_id": rec.DocumentID, "stage": "parsed_checkpoint_then_process_exit", "extraction_id": extraction.ID, "fingerprint": fp}, nil
	}
	if e = auth.NewIntegrationService(d, nil).Revoke(ctx, s.Actor.PrincipalID); e != nil {
		return nil, e
	}
	_, publishErr := r.PublishExtraction(ctx, fence, extraction.ID)
	_, searchErr := r.SearchArchives(ctx, s.Actor, s.Home, false, archive.SearchFilter{Limit: 25}, []byte("synthetic-e2e-cursor-secret-at-least32"))
	denied := func(e error) bool {
		var de *domain.Error
		return errors.As(e, &de) && (de.Code == domain.CodeForbidden || de.Code == domain.CodeUnauthorized)
	}
	var publications int
	e = d.SQL().QueryRowContext(ctx, `SELECT count(*) FROM archive_publications WHERE household_id=? AND document_id=?`, s.Home, rec.DocumentID).Scan(&publications)
	if e != nil {
		return nil, e
	}
	out := map[string]any{"task_id": task.ID, "processing_stage": "durable_parse", "publish_denied": denied(publishErr), "subsequent_query_denied": denied(searchErr), "new_publications": publications}
	if !denied(publishErr) || !denied(searchErr) || publications != 0 {
		return out, fmt.Errorf("revoked processing authority was not enforced")
	}
	return out, nil
}
func verifyRestart(ctx context.Context, d *store.DB, dir string, s state) (any, error) {
	var prior restartState
	readJSON(filepath.Join(dir, "restart-private-state.json"), &prior)
	r := d.ArchiveTasks()
	task, e := r.Get(ctx, s.Actor, s.Home, prior.Receipt.TaskID)
	if e != nil {
		return nil, e
	}
	_, oldFenceErr := r.PublishExtraction(ctx, prior.Fence, prior.Extraction.ID)
	var parseCount, eventCount, volumeCount, publicationCount int
	e = d.SQL().QueryRowContext(ctx, `SELECT count(*) FROM archive_products WHERE task_id=? AND product_key='parse'`, task.ID).Scan(&parseCount)
	if e != nil {
		return nil, e
	}
	e = d.SQL().QueryRowContext(ctx, `SELECT count(*) FROM archive_subject_events WHERE document_id=?`, prior.Receipt.DocumentID).Scan(&eventCount)
	if e != nil {
		return nil, e
	}
	obs, e := r.CurrentArchiveObservations(ctx, s.Actor, s.Home, prior.Receipt.DocumentID, false)
	if e != nil {
		return nil, e
	}
	for _, o := range obs {
		var b struct{ Field archive.PublicationField }
		if e = json.Unmarshal(o.Body, &b); e != nil {
			return nil, e
		}
		if b.Field.Name == "volume" {
			volumeCount++
		}
	}
	e = d.SQL().QueryRowContext(ctx, `SELECT count(*) FROM archive_publications WHERE document_id=?`, prior.Receipt.DocumentID).Scan(&publicationCount)
	if e != nil {
		return nil, e
	}
	out := map[string]any{"task_id": task.ID, "status": task.Status, "same_task": task.ID == prior.Receipt.TaskID, "parse_products": parseCount, "events": eventCount, "volume_observations": volumeCount, "publications": publicationCount, "old_fence_rejected": oldFenceErr != nil}
	if task.Status != domain.ArchiveTaskSucceeded || parseCount != 1 || eventCount != 1 || volumeCount != 1 || publicationCount != 1 || oldFenceErr == nil {
		return out, fmt.Errorf("restart did not retain one task/product/event/publication")
	}
	return out, nil
}
