package familyarchive

import (
	"context"
	"encoding/json"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportAndRetryWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	dir := t.TempDir()
	now := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(filepath.Join(dir, MarkerFile), []byte("house-archive"), 0600))
	repo := db.FamilyRecords()
	require.NoError(t, repo.PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, now))
	rec, _, err := repo.Record(ctx, "im", "event", domain.FamilyRecordInput{MemberID: "abu", Category: "growth", OccurredAt: now, Title: "身高", Measurements: []domain.FamilyMeasurement{{Metric: "height", Value: 70, Unit: "cm"}}}, now)
	require.NoError(t, err)
	e, err := New(Options{DB: db, Root: dir, Identity: "house-archive", AllowLocal: true, Now: func() time.Time { return now }})
	require.NoError(t, err)
	n, err := e.Export(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	data, err := os.ReadFile(filepath.Join(dir, "abu", "2026", "10", rec.ID+".json"))
	require.NoError(t, err)
	var doc Document
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Equal(t, "1", doc.SchemaVersion)
	require.Equal(t, rec.ID, doc.Record.ID)
	pending, err := repo.PendingExports(ctx, 50)
	require.NoError(t, err)
	require.Empty(t, pending)
	// Simulate publication succeeding immediately before a crash, before DB ack.
	_, err = db.SQL().ExecContext(ctx, `UPDATE family_record_exports SET exported_at=NULL`)
	require.NoError(t, err)
	n, err = e.Export(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	again, err := os.ReadFile(filepath.Join(dir, "abu", "2026", "10", rec.ID+".json"))
	require.NoError(t, err)
	require.Equal(t, data, again)
}
func TestExportRejectsMissingIdentityLocalVolumeAndCorruptExistingFile(t *testing.T) {
	for _, mode := range []string{"missing", "wrong", "local", "corrupt", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			db := testutil.NewDB(t)
			dir := t.TempDir()
			now := time.Now().UTC()
			repo := db.FamilyRecords()
			require.NoError(t, repo.PutMember(ctx, domain.FamilyMember{ID: "abu", Name: "阿布"}, now))
			rec, _, err := repo.Record(ctx, "im", "event", domain.FamilyRecordInput{MemberID: "abu", Category: "note", OccurredAt: now, Title: "记事"}, now)
			require.NoError(t, err)
			if mode != "missing" {
				id := "archive"
				if mode == "wrong" {
					id = "other"
				}
				require.NoError(t, os.WriteFile(filepath.Join(dir, MarkerFile), []byte(id), 0600))
			}
			if mode == "corrupt" {
				path := filepath.Join(dir, "abu", now.Format("2006"), now.Format("01"))
				require.NoError(t, os.MkdirAll(path, 0700))
				require.NoError(t, os.WriteFile(filepath.Join(path, rec.ID+".json"), []byte("broken"), 0600))
			}
			if mode == "symlink" {
				require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(dir, "abu")))
			}
			e, err := New(Options{DB: db, Root: dir, Identity: "archive", AllowLocal: mode != "local"})
			require.NoError(t, err)
			_, err = e.Export(ctx)
			require.Error(t, err)
			pending, err := repo.PendingExports(ctx, 50)
			require.NoError(t, err)
			require.Len(t, pending, 1)
		})
	}
}

func TestRevisionExportKeepsOriginalImmutableAndRestoresLatest(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)
	root := t.TempDir()
	now := time.Now().UTC()
	require.NoError(t, os.WriteFile(filepath.Join(root, MarkerFile), []byte("revision-archive"), 0600))
	repo := db.FamilyRecords()
	member := domain.FamilyMember{ID: "abu", Name: "阿布"}
	require.NoError(t, repo.PutMember(ctx, member, now))
	in := domain.FamilyRecordInput{MemberID: "abu", Category: "growth", OccurredAt: now, Title: "身高", DisplayOnTV: true}
	old, _, err := repo.Record(ctx, "im", "old", in, now)
	require.NoError(t, err)
	exporter, err := New(Options{DB: db, Root: root, Identity: "revision-archive", AllowLocal: true})
	require.NoError(t, err)
	_, err = exporter.Export(ctx)
	require.NoError(t, err)
	path := filepath.Join(root, "abu", now.Format("2006/01"), old.ID+".json")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	in.SupersedesID = old.ID
	in.RevisionReason = "纠正标题"
	in.Title = "身高更正"
	latest, _, err := repo.Record(ctx, "im", "new", in, now.Add(time.Second))
	require.NoError(t, err)
	// Re-export the original too: dynamic superseded_by must not change its bytes.
	_, err = db.SQL().ExecContext(ctx, `UPDATE family_record_exports SET exported_at=NULL`)
	require.NoError(t, err)
	n, err := exporter.Export(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after)
	output := filepath.Join(t.TempDir(), "recovered.db")
	n, err = Restore(ctx, RestoreOptions{Root: root, Identity: "revision-archive", AllowLocal: true, Destination: output, Members: []domain.FamilyMember{member}})
	require.NoError(t, err)
	require.Equal(t, 2, n)
	recovered, err := store.Open(ctx, output)
	require.NoError(t, err)
	defer recovered.Close()
	tv, err := recovered.FamilyRecords().List(ctx, "abu", true, 50)
	require.NoError(t, err)
	require.Len(t, tv, 1)
	require.Equal(t, latest.ID, tv[0].ID)
}
