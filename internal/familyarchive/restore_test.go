package familyarchive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/stretchr/testify/require"
)

func restoreFixture(t *testing.T) (RestoreOptions, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, MarkerFile), []byte("archive"), 0600))
	record := domain.FamilyRecord{ID: domain.NewID(), CreatedAt: time.Now().UTC(), FamilyRecordInput: domain.FamilyRecordInput{MemberID: "abu", Category: "growth", OccurredAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Title: "合成记录", Measurements: []domain.FamilyMeasurement{{Metric: "height", Value: 70, Unit: "cm"}}, DisplayOnTV: true}}
	raw, err := json.Marshal(Document{SchemaVersion: "1", Record: record})
	require.NoError(t, err)
	path := filepath.Join(root, "abu", "2026", "10", record.ID+".json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, raw, 0600))
	return RestoreOptions{Root: root, Identity: "archive", AllowLocal: true, Destination: filepath.Join(t.TempDir(), "recovered.db"), Members: []domain.FamilyMember{{ID: "abu", Name: "阿布"}}}, path
}
func TestRestorePreservesArchivedRecordAndNeverOverwrites(t *testing.T) {
	o, path := restoreFixture(t)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	n, err := Restore(context.Background(), o)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	db, err := store.Open(context.Background(), o.Destination)
	require.NoError(t, err)
	defer db.Close()
	rows, err := db.FamilyRecords().List(context.Background(), "abu", true, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, filepath.Base(path), rows[0].ID+".json")
	require.NotNil(t, rows[0].ExportedAt)
	var archived Document
	require.NoError(t, json.Unmarshal(before, &archived))
	require.Equal(t, archived.Record.FamilyRecordInput, rows[0].FamilyRecordInput)
	require.True(t, archived.Record.CreatedAt.Equal(rows[0].CreatedAt))
	_, err = Restore(context.Background(), o)
	require.Error(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestRestoreRevisionGraphAndTVProjection(t *testing.T) {
	for _, mode := range []string{"corrected", "retracted", "missing-target", "fork", "cycle", "cross-member"} {
		t.Run(mode, func(t *testing.T) {
			o, path := restoreFixture(t)
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			var original Document
			require.NoError(t, json.Unmarshal(raw, &original))
			correction := original.Record
			correction.ID = domain.NewID()
			correction.SupersedesID = original.Record.ID
			correction.RevisionReason = "更正"
			correction.Measurements = []domain.FamilyMeasurement{{Metric: "height", Value: 71, Unit: "cm"}}
			// Earlier observation month ensures the replacement is imported first.
			correction.OccurredAt = correction.OccurredAt.AddDate(0, -1, 0)
			write := func(r domain.FamilyRecord) {
				p := filepath.Join(o.Root, r.MemberID, r.OccurredAt.Format("2006/01"), r.ID+".json")
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0700))
				b, err := json.Marshal(Document{SchemaVersion: "2", Record: r})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(p, b, 0600))
			}
			switch mode {
			case "retracted":
				correction.Retracted = true
				correction.DisplayOnTV = false
			case "missing-target":
				correction.SupersedesID = domain.NewID()
			case "fork":
				fork := correction
				fork.ID = domain.NewID()
				write(fork)
			case "cycle":
				original.Record.SupersedesID = correction.ID
				original.Record.RevisionReason = "循环"
				write(original.Record)
			case "cross-member":
				correction.MemberID = "parent"
				o.Members = append(o.Members, domain.FamilyMember{ID: "parent", Name: "家长"})
			}
			write(correction)
			n, err := Restore(context.Background(), o)
			if mode != "corrected" && mode != "retracted" {
				require.Error(t, err)
				_, err = os.Stat(o.Destination)
				require.True(t, os.IsNotExist(err))
				return
			}
			require.NoError(t, err)
			require.Equal(t, 2, n)
			db, err := store.Open(context.Background(), o.Destination)
			require.NoError(t, err)
			defer db.Close()
			history, err := db.FamilyRecords().List(context.Background(), "abu", false, 50)
			require.NoError(t, err)
			require.Len(t, history, 2)
			for _, r := range history {
				if r.ID == original.Record.ID {
					require.Equal(t, correction.ID, r.SupersededBy)
				}
			}
			tv, err := db.FamilyRecords().List(context.Background(), "abu", true, 50)
			require.NoError(t, err)
			if mode == "retracted" {
				require.Empty(t, tv)
			} else {
				require.Len(t, tv, 1)
				require.Equal(t, correction.ID, tv[0].ID)
			}
		})
	}
}
func TestRestoreRejectsInvalidArchiveWithoutPublishingPartialDatabase(t *testing.T) {
	for _, mode := range []string{"marker", "local", "unknown-member", "corrupt", "moved", "symlink", "unknown-field", "extra-json", "cancelled", "inside-archive"} {
		t.Run(mode, func(t *testing.T) {
			o, path := restoreFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "marker":
				o.Identity = "wrong"
			case "local":
				o.AllowLocal = false
			case "unknown-member":
				o.Members = nil
			case "corrupt":
				require.NoError(t, os.WriteFile(path, []byte("broken"), 0600))
			case "moved":
				require.NoError(t, os.Rename(path, filepath.Join(filepath.Dir(path), domain.NewID()+".json")))
			case "symlink":
				require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(o.Root, "outside")))
			case "unknown-field":
				raw, err := os.ReadFile(path)
				require.NoError(t, err)
				raw = append([]byte(`{"unexpected":1,`), raw[1:]...)
				require.NoError(t, os.WriteFile(path, raw, 0600))
			case "extra-json":
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				require.NoError(t, err)
				_, err = f.WriteString("{}")
				require.NoError(t, err)
				require.NoError(t, f.Close())
			case "inside-archive":
				o.Destination = filepath.Join(o.Root, "recovered.db")
			case "cancelled":
				cancel()
			}
			_, err := Restore(ctx, o)
			require.Error(t, err)
			_, err = os.Stat(o.Destination)
			require.True(t, os.IsNotExist(err))
		})
	}
}
