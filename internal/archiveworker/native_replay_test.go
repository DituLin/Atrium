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
	"testing"
	"time"
)

// This test never calls a provider: it requires an actual retained synthetic
// completion whose manifest must exactly match today's real native parse.
func TestWorkerActualNativeWithRetainedCompletion(t *testing.T) {
	original, helper, capture := os.Getenv("ATRIUM_T09_NATIVE_IMAGE"), os.Getenv("ATRIUM_T09_NATIVE_HELPER"), os.Getenv("ATRIUM_T09_CAPTURE")
	if original == "" || helper == "" || capture == "" {
		t.Skip("opt-in retained synthetic native evidence")
	}
	raw, e := os.ReadFile(capture)
	require.NoError(t, e)
	var recorded struct {
		Plan    archive.UnderstandingPlan `json:"plan"`
		Product archive.Understanding     `json:"product"`
	}
	require.NoError(t, json.Unmarshal(raw, &recorded))
	ctx := context.Background()
	d, e := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	require.NoError(t, e)
	defer d.Close()
	_, e = d.Migrate(ctx)
	require.NoError(t, e)
	home, e := d.LocalHousehold(ctx)
	require.NoError(t, e)
	now := time.Now()
	p := &domain.IntegrationPrincipal{ID: "synthetic", Label: "synthetic", Enabled: true, Policy: domain.IntegrationPolicy{Permissions: []string{"archive.ingest"}, HouseholdIDs: []string{home.ID}}, PolicyVersion: 1, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	credential := &domain.IntegrationCredential{ID: "cred", PrincipalID: p.ID, TokenHash: "synthetic-hash", ExpiresAt: p.ExpiresAt, CreatedAt: now}
	require.NoError(t, d.Integrations().Create(ctx, p, credential))
	blobs, e := archive.NewBlobStore(filepath.Join(t.TempDir(), "originals"), 32<<20)
	require.NoError(t, e)
	defer blobs.Close()
	input, e := os.Open(original)
	require.NoError(t, e)
	ref, e := blobs.Put(ctx, home.ID, input, archive.PutOptions{})
	input.Close()
	require.NoError(t, e)
	rec, e := d.ArchiveTasks().AcceptIntake(ctx, domain.IntegrationActor{PrincipalID: p.ID, CredentialID: credential.ID}, home.ID, 1, domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: domain.ArchiveInputFile, Channel: "synthetic", Account: "fixture", Conversation: "fixture", Sender: "fixture", ProviderMessageID: "native"}, OriginalName: "report.png"}, ref)
	require.NoError(t, e)
	w := Worker{Tasks: d.ArchiveTasks(), Blobs: blobs, Household: home.ID, Fingerprint: "retained-native-v2", Lease: time.Minute, Native: archive.NativeParser{HelperPath: helper}}
	calls := 0
	w.understand = func(ctx context.Context, plan archive.UnderstandingPlan, chunk archive.UnderstandingChunk) (archive.ChunkUnderstanding, error) {
		require.Equal(t, recorded.Plan.ManifestDigest, plan.ManifestDigest, "real native output must exactly match retained inference input")
		calls++
		return recorded.Product.Chunks[chunk.Index], nil
	}
	worked, e := w.RunOne(ctx)
	require.NoError(t, e)
	require.True(t, worked)
	require.Equal(t, len(recorded.Plan.Chunks), calls)
	var status string
	require.NoError(t, d.SQL().QueryRow(`SELECT status FROM archive_tasks WHERE id=?`, rec.TaskID).Scan(&status))
	require.Equal(t, "succeeded", status)
	var entities, events, facts int
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_entities`).Scan(&entities))
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_subject_events`).Scan(&events))
	require.NoError(t, d.SQL().QueryRow(`SELECT count(*) FROM archive_observations`).Scan(&facts))
	require.Equal(t, 1, entities)
	require.Equal(t, 1, events)
	require.Greater(t, facts, 0)
	t.Logf("real_native_chunks=%d entities=%d events=%d observations=%d retained_completion=true new_provider_calls=0", calls, entities, events, facts)
}
