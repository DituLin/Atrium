package archive_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/stretchr/testify/require"
)

type intakeHarness struct {
	db        *store.DB
	blobs     *archive.BlobStore
	service   *archive.Intake
	household string
	actor     domain.IntegrationActor
	auth      *auth.IntegrationService
}

func newIntake(t *testing.T, permissions ...string) *intakeHarness {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Migrate(ctx)
	require.NoError(t, err)
	home, err := db.LocalHousehold(ctx)
	require.NoError(t, err)
	blobs, err := archive.NewBlobStore(filepath.Join(t.TempDir(), "originals"), 1024)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, blobs.Close()) })
	h := &intakeHarness{db: db, blobs: blobs, household: home.ID, auth: auth.NewIntegrationService(db, nil)}
	h.service = archive.NewIntake(blobs, db.ArchiveTasks())
	h.actor = h.newActor(t, permissions...)
	return h
}
func (h *intakeHarness) newActor(t *testing.T, permissions ...string) domain.IntegrationActor {
	p, token, err := h.auth.Issue(context.Background(), "intake test", domain.IntegrationPolicy{Permissions: permissions, HouseholdIDs: []string{h.household}}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	var cid string
	require.NoError(t, h.db.SQL().QueryRow(`SELECT id FROM integration_credentials WHERE token_hash=?`, auth.HashToken(token)).Scan(&cid))
	return domain.IntegrationActor{PrincipalID: p.ID, CredentialID: cid}
}
func metadata(id string, kind domain.ArchiveInputKind) domain.ArchiveIntakeMetadata {
	return domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: kind, Channel: "wechat", Account: "a", Conversation: "c", Sender: "s", ProviderMessageID: id, SlotIndex: 0}, OriginalName: "note.txt"}
}
func (h *intakeHarness) accept(t *testing.T, m domain.ArchiveIntakeMetadata, text string) *domain.ArchiveIntakeReceipt {
	t.Helper()
	r, err := h.service.Receive(context.Background(), h.actor, h.household, m, strings.NewReader(text), func() error { return nil })
	require.NoError(t, err)
	return r
}
func count(t *testing.T, h *intakeHarness, table string) int {
	t.Helper()
	var n int
	require.NoError(t, h.db.SQL().QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
	return n
}
func TestIntakeRealBytesReplayLookupAndFileVersusTextIdentity(t *testing.T) {
	h := newIntake(t, "archive.ingest", "archive.read")
	m := metadata("one", domain.ArchiveInputFile)
	text := "  原文\r\n70 ml\n"
	first := h.accept(t, m, text)
	require.Equal(t, "saved", first.OriginalStatus)
	require.Equal(t, int64(len(text)), first.SizeBytes)
	require.Equal(t, domain.ArchiveTaskQueued, first.Task.Status)
	again := h.accept(t, m, text)
	require.Equal(t, first, again)
	lookup, err := h.service.Lookup(context.Background(), h.actor, h.household, m.ArchiveSourceIdentity)
	require.NoError(t, err)
	require.Equal(t, first, lookup)
	m.Channel = "discord"
	second := h.accept(t, m, text)
	require.NotEqual(t, first.TaskID, second.TaskID)
	require.Equal(t, first.DocumentID, second.DocumentID)
	h.actor = h.newActor(t, "archive.ingest", "archive.read")
	third := h.accept(t, m, text)
	require.NotEqual(t, second.TaskID, third.TaskID)
	require.Equal(t, first.DocumentID, third.DocumentID)
	require.Equal(t, 1, count(t, h, "archive_documents"))
	require.Equal(t, 3, count(t, h, "archive_sources"))
	require.Equal(t, 3, count(t, h, "archive_tasks"))
	m.Kind = domain.ArchiveInputText
	m.ProviderMessageID = "text-one"
	a := h.accept(t, m, text)
	m.ProviderMessageID = "text-two"
	b := h.accept(t, m, text)
	require.NotEqual(t, a.DocumentID, b.DocumentID)
	require.Equal(t, 1, count(t, h, "archive_blobs"))
	ref := domain.ArchiveBlobRef{HouseholdID: h.household, SHA256: first.SHA256, SizeBytes: first.SizeBytes, MIME: "text/plain; charset=utf-8", StorageVersion: 1}
	f, err := h.blobs.OpenVerified(context.Background(), ref)
	require.NoError(t, err)
	defer f.Close()
	bytes, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, text, string(bytes))
}
func TestIntakeOwnMinimalReceiptUnchangedByWorkerAndOtherPrincipal(t *testing.T) {
	h := newIntake(t, "archive.ingest")
	m := metadata("m", domain.ArchiveInputFile)
	r := h.accept(t, m, "bytes")
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	for _, field := range []string{"document", "dedup", "visibility", "household", "principal", "queued", "stage"} {
		require.NotContains(t, string(raw), field)
	}
	require.Nil(t, r.Task)
	_, err = h.db.SQL().Exec(`UPDATE archive_tasks SET status='succeeded',stage='/private/path',result_json='{"secret":"/server"}' WHERE id=?`, r.TaskID)
	require.NoError(t, err)
	got, err := h.service.Lookup(context.Background(), h.actor, h.household, m.ArchiveSourceIdentity)
	require.NoError(t, err)
	require.Equal(t, r, got)
	_, err = h.db.ArchiveTasks().Get(context.Background(), h.actor, h.household, r.TaskID)
	require.Error(t, err)
	h.actor = h.newActor(t, "archive.ingest")
	_, err = h.service.Lookup(context.Background(), h.actor, h.household, m.ArchiveSourceIdentity)
	require.ErrorIs(t, err, domain.ErrNotFound)
	other := h.accept(t, m, "bytes")
	require.NotEqual(t, r.TaskID, other.TaskID)
	require.Empty(t, other.DocumentID)
}
func TestIntakeConflictBeforePrivacyAndAtomicAudit(t *testing.T) {
	h := newIntake(t, "archive.ingest", "archive.read", "archive.organize")
	m := metadata("m", domain.ArchiveInputFile)
	first := h.accept(t, m, "bytes")
	for _, change := range []string{"bytes", "intent", "name"} {
		bad := m
		body := "bytes"
		switch change {
		case "bytes":
			body = "changed"
		case "intent":
			bad.VisibilityIntent = "private"
		case "name":
			bad.OriginalName = "changed.txt"
		}
		_, err := h.service.Receive(context.Background(), h.actor, h.household, bad, strings.NewReader(body), func() error { return nil })
		var de *domain.Error
		require.ErrorAs(t, err, &de)
		require.Equal(t, domain.CodeIdempotencyConflict, de.Code)
	}
	require.Zero(t, count(t, h, "archive_visibility_events"))
	var visible bool
	var version int
	require.NoError(t, h.db.SQL().QueryRow(`SELECT tv_visible,visibility_version FROM archive_documents WHERE id=?`, first.DocumentID).Scan(&visible, &version))
	require.True(t, visible)
	require.Equal(t, 1, version)
	m.ProviderMessageID = "private"
	m.VisibilityIntent = "private"
	second := h.accept(t, m, "bytes")
	require.Equal(t, first.DocumentID, second.DocumentID)
	require.Equal(t, 1, count(t, h, "archive_visibility_events"))
	require.NoError(t, h.db.SQL().QueryRow(`SELECT tv_visible,visibility_version FROM archive_documents WHERE id=?`, first.DocumentID).Scan(&visible, &version))
	require.False(t, visible)
	require.Equal(t, 2, version)
	var auditActor, auditSource, auditTime string
	var beforeVisible, afterVisible bool
	require.NoError(t, h.db.SQL().QueryRow(`SELECT actor_principal_id,source_key,before_visible,after_visible,created_at FROM archive_visibility_events WHERE household_id=? AND document_id=? AND version=2`, h.household, first.DocumentID).Scan(&auditActor, &auditSource, &beforeVisible, &afterVisible, &auditTime))
	sourceKey, keyErr := m.Key(h.household, h.actor.PrincipalID)
	require.NoError(t, keyErr)
	require.Equal(t, h.actor.PrincipalID, auditActor)
	require.Equal(t, sourceKey, auditSource)
	require.True(t, beforeVisible)
	require.False(t, afterVisible)
	require.Equal(t, store.FormatTime(second.ReceivedAt), auditTime)
	m.ProviderMessageID = "private-again"
	h.accept(t, m, "bytes")
	require.Equal(t, 1, count(t, h, "archive_visibility_events"))
	m.ProviderMessageID = "inherit-again"
	m.VisibilityIntent = "inherit"
	h.accept(t, m, "bytes")
	require.NoError(t, h.db.SQL().QueryRow(`SELECT tv_visible,visibility_version FROM archive_documents WHERE id=?`, first.DocumentID).Scan(&visible, &version))
	require.False(t, visible)
	require.Equal(t, 2, version)
	m.ProviderMessageID = "new-private"
	m.VisibilityIntent = "private"
	h.accept(t, m, "other bytes")
	require.Equal(t, 2, count(t, h, "archive_visibility_events"))
}
func TestIntakePrivateUniformPermissionsAndUploadFences(t *testing.T) {
	for _, permissions := range [][]string{{"archive.ingest"}, {"archive.ingest", "archive.read"}, {"archive.ingest", "archive.organize"}} {
		h := newIntake(t, permissions...)
		for _, id := range []string{"unknown", "existing"} {
			m := metadata(id, domain.ArchiveInputFile)
			if id == "existing" {
				h.accept(t, m, "bytes")
			}
			m.VisibilityIntent = "private"
			_, err := h.service.Receive(context.Background(), h.actor, h.household, m, strings.NewReader("bytes"), func() error { return nil })
			var de *domain.Error
			require.ErrorAs(t, err, &de)
			require.Equal(t, domain.CodeForbidden, de.Code)
		}
	}
	for _, change := range []string{"revoke", "rotate", "policy", "expiry"} {
		t.Run(change, func(t *testing.T) {
			h := newIntake(t, "archive.ingest")
			m := metadata("m", domain.ArchiveInputFile)
			_, err := h.service.Receive(context.Background(), h.actor, h.household, m, strings.NewReader("bytes"), func() error {
				switch change {
				case "revoke":
					return h.auth.Revoke(context.Background(), h.actor.PrincipalID)
				case "rotate":
					_, _, e := h.auth.Rotate(context.Background(), h.actor.PrincipalID, time.Now().Add(30*time.Minute))
					return e
				case "policy":
					_, e := h.auth.UpdatePolicy(context.Background(), h.actor.PrincipalID, domain.IntegrationPolicy{Permissions: []string{"archive.ingest"}, HouseholdIDs: []string{h.household}})
					return e
				default:
					_, e := h.db.SQL().Exec(`UPDATE integration_credentials SET expires_at=? WHERE id=?`, store.FormatTime(time.Now().Add(-time.Minute)), h.actor.CredentialID)
					return e
				}
			})
			var denied *domain.Error
			require.ErrorAs(t, err, &denied)
			expectedCode := domain.CodeUnauthorized
			if change == "policy" {
				expectedCode = domain.CodeForbidden
			}
			require.Equal(t, expectedCode, denied.Code)
			require.Zero(t, count(t, h, "archive_sources"))
			require.Zero(t, count(t, h, "archive_tasks"))
		})
	}
}
func TestIntakeFailuresLeaveNoAcceptanceAndPublishedBlobSurvives(t *testing.T) {
	h := newIntake(t, "archive.ingest")
	m := metadata("m", domain.ArchiveInputFile)
	for _, body := range []string{"", strings.Repeat("a", 1025)} {
		_, err := h.service.Receive(context.Background(), h.actor, h.household, m, strings.NewReader(body), func() error { return nil })
		require.Error(t, err)
	}
	wrong := int64(8)
	m.ExpectedSize = &wrong
	_, err := h.service.Receive(context.Background(), h.actor, h.household, m, strings.NewReader("bytes"), func() error { return nil })
	require.Error(t, err)
	m.ExpectedSize = nil
	_, err = h.service.Receive(context.Background(), h.actor, h.household, m, strings.NewReader("bytes"), func() error { return errors.New("invalid multipart end") })
	require.Error(t, err)
	require.Zero(t, count(t, h, "archive_sources"))
	_, err = h.db.SQL().Exec(`CREATE TRIGGER fail_accept BEFORE INSERT ON archive_sources BEGIN SELECT RAISE(ABORT,'injected commit failure'); END`)
	require.NoError(t, err)
	_, err = h.service.Receive(context.Background(), h.actor, h.household, m, strings.NewReader("bytes"), func() error { return nil })
	require.Error(t, err)
	for _, table := range []string{"archive_blobs", "archive_documents", "archive_sources", "archive_tasks", "archive_visibility_events"} {
		require.Zero(t, count(t, h, table))
	}
	ref, err := h.blobs.Put(context.Background(), h.household, strings.NewReader("bytes"), archive.PutOptions{})
	require.NoError(t, err)
	f, err := h.blobs.OpenVerified(context.Background(), ref)
	require.NoError(t, err)
	before, err := f.Stat()
	require.NoError(t, err)
	require.NoError(t, f.Close())
	_, err = h.db.SQL().Exec(`DROP TRIGGER fail_accept`)
	require.NoError(t, err)
	h.accept(t, m, "bytes")
	f, err = h.blobs.OpenVerified(context.Background(), ref)
	require.NoError(t, err)
	after, err := f.Stat()
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.True(t, os.SameFile(before, after))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.ProviderMessageID = "cancelled"
	_, err = h.service.Receive(ctx, h.actor, h.household, m, strings.NewReader("bytes"), func() error { return nil })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, count(t, h, "archive_sources"))
}

func TestIntakeTaskRejectsUnbackedGeneralTask(t *testing.T) {
	h := newIntake(t, "archive.ingest", "archive.read")
	task, _, err := h.db.ArchiveTasks().Enqueue(context.Background(), h.actor, h.household, "other.work", "unbacked", json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = h.service.Task(context.Background(), h.actor, h.household, task.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestIntakeUploadDoesNotHoldWriterLockAndFencesMidStreamRevocation(t *testing.T) {
	h := newIntake(t, "archive.ingest")
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		_, err := h.service.Receive(context.Background(), h.actor, h.household, metadata("midstream", domain.ArchiveInputFile), reader, func() error { return nil })
		done <- err
	}()
	// Write returns only once Put has entered the body reader after preflight.
	_, err := io.WriteString(writer, "first bytes")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, h.auth.Revoke(ctx, h.actor.PrincipalID), "a streaming upload must not own SQLite's writer lock")
	require.NoError(t, writer.Close())
	select {
	case err := <-done:
		var denied *domain.Error
		require.ErrorAs(t, err, &denied)
		require.Equal(t, domain.CodeUnauthorized, denied.Code)
	case <-time.After(time.Second):
		t.Fatal("upload did not finish")
	}
	require.Zero(t, count(t, h, "archive_sources"))
}
func TestIntakeAssertionsDoNotAlterIdentityAndFailedSlotIsIndependent(t *testing.T) {
	h := newIntake(t, "archive.ingest")
	m := metadata("slots", domain.ArchiveInputFile)
	first := h.accept(t, m, "bytes")
	size := first.SizeBytes
	m.ExpectedSize = &size
	m.ExpectedSHA256 = first.SHA256
	m.VisibilityIntent = "inherit"
	again := h.accept(t, m, "bytes")
	require.Equal(t, first, again)
	m.SlotIndex = 1
	m.ExpectedSize = nil
	m.ExpectedSHA256 = ""
	_, err := h.service.Receive(context.Background(), h.actor, h.household, m, strings.NewReader(""), func() error { return nil })
	require.Error(t, err)
	_, err = h.service.Lookup(context.Background(), h.actor, h.household, m.ArchiveSourceIdentity)
	require.ErrorIs(t, err, domain.ErrNotFound)
	m.SlotIndex = 0
	lookup, err := h.service.Lookup(context.Background(), h.actor, h.household, m.ArchiveSourceIdentity)
	require.NoError(t, err)
	require.Equal(t, first, lookup)
	require.Equal(t, 1, count(t, h, "archive_sources"))
}
func TestIntakeVisibilityAuditFailureRollsBackDocumentAndSource(t *testing.T) {
	h := newIntake(t, "archive.ingest", "archive.read", "archive.organize")
	m := metadata("public", domain.ArchiveInputFile)
	first := h.accept(t, m, "bytes")
	_, err := h.db.SQL().Exec(`CREATE TRIGGER fail_visibility BEFORE INSERT ON archive_visibility_events BEGIN SELECT RAISE(ABORT,'injected audit failure'); END`)
	require.NoError(t, err)
	m.ProviderMessageID = "private"
	m.VisibilityIntent = "private"
	_, err = h.service.Receive(context.Background(), h.actor, h.household, m, strings.NewReader("bytes"), func() error { return nil })
	require.Error(t, err)
	var visible bool
	var version int
	require.NoError(t, h.db.SQL().QueryRow(`SELECT tv_visible,visibility_version FROM archive_documents WHERE id=?`, first.DocumentID).Scan(&visible, &version))
	require.True(t, visible)
	require.Equal(t, 1, version)
	require.Equal(t, 1, count(t, h, "archive_sources"))
	require.Equal(t, 1, count(t, h, "archive_tasks"))
	require.Zero(t, count(t, h, "archive_visibility_events"))
}
func TestIntakeSourceDatabaseConstraintsRejectNULAndCrossHousehold(t *testing.T) {
	h := newIntake(t, "archive.ingest")
	h.accept(t, metadata("source", domain.ArchiveInputFile), "bytes")
	// Changing immutable receipts is never an alternative to a source conflict.
	_, err := h.db.SQL().Exec(`UPDATE archive_sources SET size_bytes=size_bytes+1`)
	require.Error(t, err)
	for _, key := range []string{strings.Repeat("a", 64) + "\x00", strings.Repeat("a", 63) + "\x00x", strings.Repeat("A", 64)} {
		_, err = h.db.SQL().Exec(`INSERT INTO archive_sources SELECT household_id,?,principal_id,'copy',document_id,task_id,metadata_json,sha256,size_bytes,received_at FROM archive_sources`, key)
		require.ErrorContains(t, err, "CHECK constraint failed")
	}
	var n int
	require.NoError(t, h.db.SQL().QueryRow(`SELECT count(*) FROM archive_sources WHERE length(CAST(source_key AS BLOB))=64 AND source_key NOT GLOB '*[^0-9a-f]*'`).Scan(&n))
	require.Equal(t, 1, n)
	_, err = h.db.SQL().Exec(`INSERT INTO households(id) VALUES('other-home')`)
	require.NoError(t, err)
	_, err = h.db.SQL().Exec(`INSERT INTO archive_sources SELECT 'other-home',?,principal_id,'cross-home',document_id,task_id,metadata_json,sha256,size_bytes,received_at FROM archive_sources`, strings.Repeat("a", 64))
	require.ErrorContains(t, err, "FOREIGN KEY constraint failed")
}

func TestIntakeRotationRecoversOwnReceiptWithNewCredential(t *testing.T) {
	h := newIntake(t, "archive.ingest")
	m := metadata("rotate", domain.ArchiveInputText)
	first := h.accept(t, m, "bytes")
	_, token, err := h.auth.Rotate(context.Background(), h.actor.PrincipalID, time.Now().Add(30*time.Minute))
	require.NoError(t, err)
	_, err = h.service.Lookup(context.Background(), h.actor, h.household, m.ArchiveSourceIdentity)
	require.Error(t, err)
	require.NoError(t, h.db.SQL().QueryRow(`SELECT id FROM integration_credentials WHERE token_hash=?`, auth.HashToken(token)).Scan(&h.actor.CredentialID))
	recovered, err := h.service.Lookup(context.Background(), h.actor, h.household, m.ArchiveSourceIdentity)
	require.NoError(t, err)
	require.Equal(t, first, recovered)
	require.Equal(t, first, h.accept(t, m, "bytes"))
}
