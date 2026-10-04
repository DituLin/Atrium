package archiveworker

import (
	"context"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func workerFixture(t *testing.T) (*store.DB, *Worker, string, string) {
	t.Helper()
	ctx := context.Background()
	dbpath := filepath.Join(t.TempDir(), "test.db")
	d, e := store.Open(ctx, dbpath)
	require.NoError(t, e)
	t.Cleanup(func() { _ = d.Close() })
	_, e = d.Migrate(ctx)
	require.NoError(t, e)
	home, e := d.LocalHousehold(ctx)
	require.NoError(t, e)
	h := home.ID
	now := time.Now()
	p := &domain.IntegrationPrincipal{ID: "ingest", Label: "ingest", Enabled: true, Policy: domain.IntegrationPolicy{Permissions: []string{"archive.ingest"}, HouseholdIDs: []string{h}}, PolicyVersion: 1, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	c := &domain.IntegrationCredential{ID: "cred", PrincipalID: p.ID, TokenHash: "hash", ExpiresAt: p.ExpiresAt, CreatedAt: now}
	require.NoError(t, d.Integrations().Create(ctx, p, c))
	blobs, e := archive.NewBlobStore(filepath.Join(t.TempDir(), "blobs"), 1<<20)
	require.NoError(t, e)
	t.Cleanup(func() { _ = blobs.Close() })
	ref, e := blobs.Put(ctx, h, strings.NewReader("体积70 ml"), archive.PutOptions{})
	require.NoError(t, e)
	rec, e := d.ArchiveTasks().AcceptIntake(ctx, domain.IntegrationActor{PrincipalID: p.ID, CredentialID: c.ID}, h, 1, domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: domain.ArchiveInputText, Channel: "test", Account: "a", Conversation: "c", Sender: "s", ProviderMessageID: "one"}, OriginalName: "note.txt"}, ref)
	require.NoError(t, e)
	require.Empty(t, rec.DocumentID)
	w := Worker{Tasks: d.ArchiveTasks(), Blobs: blobs, Household: h, Fingerprint: "offline-test-v1", Lease: time.Minute}
	w.understand = func(ctx context.Context, p archive.UnderstandingPlan, c archive.UnderstandingChunk) (archive.ChunkUnderstanding, error) {
		v := c.Owned[0]
		x := archive.Extraction{Labels: []string{}, Domain: "health", DocumentType: "record", Subjects: []archive.LocalSubject{}, Fields: []archive.ExtractedField{{ID: "f", Name: "volume", Raw: "70", SubjectRefs: []string{}, Dimensions: []archive.LiteralDimension{}, Normalizations: []archive.Normalization{{Kind: "decimal", Value: "70"}}, Evidence: []archive.EvidenceReference{{ViewID: v.ViewID, Start: 2, End: 4, Quote: "70"}}}}, Events: []archive.EventProposal{{ID: "e", Type: "measurement", FieldRefs: []string{"f"}, SubjectRefs: []string{}, SectionEvidence: []archive.EvidenceReference{{ViewID: v.ViewID, Start: 0, End: 7, Quote: "体积70 ml"}}}}, Coverage: []archive.UnitCoverage{{ViewID: v.ViewID, Disposition: "extracted"}}, Issues: []archive.ExtractionIssue{}}
		b, _ := json.Marshal(x)
		return archive.ValidateChunk(p, c, b)
	}
	return d, &w, rec.TaskID, dbpath
}

func TestWorkerRealTextPipelineOfflineModelBoundary(t *testing.T) {
	ctx := context.Background()
	d, w, taskID, _ := workerFixture(t)
	worked, e := w.RunOne(ctx)
	require.NoError(t, e)
	require.True(t, worked)
	var status string
	require.NoError(t, d.SQL().QueryRow(`SELECT status FROM archive_tasks WHERE id=?`, taskID).Scan(&status))
	require.Equal(t, "succeeded", status)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&n))
	require.Equal(t, 1, n)
	worked, e = w.RunOne(ctx)
	require.NoError(t, e)
	require.False(t, worked)
}
func TestWorkerFingerprintChangesWithHelperBytes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "helper")
	require.NoError(t, os.WriteFile(p, []byte("v1"), 0600))
	w := Worker{Native: archive.NativeParser{HelperPath: p}}
	a, e := w.ConfigurationFingerprint()
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(p, []byte("v2"), 0600))
	b, e := w.ConfigurationFingerprint()
	require.NoError(t, e)
	require.NotEqual(t, a, b)
}
func TestWorkerFingerprintAcceptsPluginDirectoryAndTracksResources(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.mjs")
	require.NoError(t, os.WriteFile(p, []byte("v1"), 0600))
	w := Worker{Adapter: archive.OpenClawUnderstandingAdapter{PluginPath: dir}}
	a, e := w.ConfigurationFingerprint()
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(p, []byte("v2"), 0600))
	b, e := w.ConfigurationFingerprint()
	require.NoError(t, e)
	require.NotEqual(t, a, b)
}

func TestWorkerUnknownCallSurvivesDatabaseRestart(t *testing.T) {
	ctx := context.Background()
	d, w, taskID, path := workerFixture(t)
	calls := 0
	understand := w.understand
	w.understand = func(ctx context.Context, p archive.UnderstandingPlan, c archive.UnderstandingChunk) (archive.ChunkUnderstanding, error) {
		calls++
		return understand(ctx, p, c)
	}
	_, e := d.SQL().Exec(`CREATE TRIGGER fail_chunk_commit BEFORE INSERT ON archive_products WHEN NEW.product_key='chunk-000' BEGIN SELECT RAISE(ABORT,'synthetic post-call crash'); END`)
	require.NoError(t, e)
	worked, e := w.RunOne(ctx)
	require.True(t, worked)
	require.Error(t, e)
	require.Equal(t, 1, calls)
	var spent int
	require.NoError(t, d.SQL().QueryRow(`SELECT spent FROM archive_budgets WHERE task_id=?`, taskID).Scan(&spent))
	require.Equal(t, 1, spent)
	_, e = d.SQL().Exec(`DROP TRIGGER fail_chunk_commit`)
	require.NoError(t, e)
	require.NoError(t, d.Close())
	reopened, e := store.Open(ctx, path)
	require.NoError(t, e)
	defer reopened.Close()
	w.Tasks = reopened.ArchiveTasks()
	_, e = reopened.SQL().Exec(`UPDATE archive_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, taskID)
	require.NoError(t, e)
	worked, e = w.RunOne(ctx)
	require.True(t, worked)
	require.NoError(t, e)
	require.Equal(t, 2, calls)
	require.NoError(t, reopened.SQL().QueryRow(`SELECT spent FROM archive_budgets WHERE task_id=?`, taskID).Scan(&spent))
	require.Equal(t, 2, spent)
	var unknown int
	require.NoError(t, reopened.SQL().QueryRow(`SELECT count(*) FROM archive_invocations WHERE task_id=? AND status='reserved'`, taskID).Scan(&unknown))
	require.Equal(t, 1, unknown)
	worked, e = w.RunOne(ctx)
	require.NoError(t, e)
	require.False(t, worked)
	require.Equal(t, 2, calls)
}
func TestWorkerRevocationCancelsExternalCall(t *testing.T) {
	d, w, _, _ := workerFixture(t)
	w.Lease = 300 * time.Millisecond
	cancelled := false
	w.understand = func(ctx context.Context, p archive.UnderstandingPlan, c archive.UnderstandingChunk) (archive.ChunkUnderstanding, error) {
		_, e := d.SQL().Exec(`UPDATE integration_principals SET enabled=0 WHERE id='ingest'`)
		require.NoError(t, e)
		select {
		case <-ctx.Done():
			cancelled = true
			return archive.ChunkUnderstanding{}, ctx.Err()
		case <-time.After(3 * time.Second):
			t.Fatal("revoked external call not cancelled")
			return archive.ChunkUnderstanding{}, context.DeadlineExceeded
		}
	}
	_, e := w.RunOne(context.Background())
	require.Error(t, e)
	require.True(t, cancelled)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&n))
	require.Zero(t, n)
}

func TestWorkerCallBudgetRemainsExhaustedAfterRestart(t *testing.T) {
	ctx := context.Background()
	d, w, taskID, path := workerFixture(t)
	calls := 0
	w.understand = func(context.Context, archive.UnderstandingPlan, archive.UnderstandingChunk) (archive.ChunkUnderstanding, error) {
		calls++
		return archive.ChunkUnderstanding{}, context.DeadlineExceeded
	}
	for i := 0; i < 2; i++ {
		worked, e := w.RunOne(ctx)
		require.True(t, worked)
		require.NoError(t, e)
		_, e = d.SQL().Exec(`UPDATE archive_tasks SET available_at='2000-01-01T00:00:00Z' WHERE id=?`, taskID)
		require.NoError(t, e)
	}
	require.Equal(t, 2, calls)
	require.NoError(t, d.Close())
	reopened, e := store.Open(ctx, path)
	require.NoError(t, e)
	defer reopened.Close()
	w.Tasks = reopened.ArchiveTasks()
	worked, e := w.RunOne(ctx)
	require.NoError(t, e)
	require.True(t, worked)
	require.Equal(t, 2, calls)
	var status, code string
	require.NoError(t, reopened.SQL().QueryRow(`SELECT status,last_error_code FROM archive_tasks WHERE id=?`, taskID).Scan(&status, &code))
	require.Equal(t, "failed", status)
	require.Equal(t, "call_budget_exhausted", code)
	worked, e = w.RunOne(ctx)
	require.NoError(t, e)
	require.False(t, worked)
}

func TestWorkerCompletedCheckpointRestartsWithoutExternalCall(t *testing.T) {
	ctx := context.Background()
	d, w, taskID, path := workerFixture(t)
	calls := 0
	understand := w.understand
	w.understand = func(ctx context.Context, p archive.UnderstandingPlan, c archive.UnderstandingChunk) (archive.ChunkUnderstanding, error) {
		calls++
		return understand(ctx, p, c)
	}
	_, e := d.SQL().Exec(`CREATE TRIGGER fail_publication BEFORE INSERT ON archive_outbox BEGIN SELECT RAISE(ABORT,'synthetic publish interruption'); END`)
	require.NoError(t, e)
	_, e = w.RunOne(ctx)
	require.Error(t, e)
	require.Equal(t, 1, calls)
	var n int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&n))
	require.Zero(t, n)
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_products WHERE product_key='publication'`).Scan(&n))
	require.Equal(t, 1, n)
	_, e = d.SQL().Exec(`DROP TRIGGER fail_publication`)
	require.NoError(t, e)
	require.NoError(t, d.Close())
	reopened, e := store.Open(ctx, path)
	require.NoError(t, e)
	defer reopened.Close()
	w.Tasks = reopened.ArchiveTasks()
	_, e = reopened.SQL().Exec(`UPDATE archive_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, taskID)
	require.NoError(t, e)
	_, e = w.RunOne(ctx)
	require.NoError(t, e)
	require.Equal(t, 1, calls)
	require.NoError(t, reopened.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&n))
	require.Equal(t, 1, n)
}
