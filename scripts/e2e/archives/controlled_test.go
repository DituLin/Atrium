package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/stretchr/testify/require"
)

func TestFormulaRejectsTraversalBeforeOpeningFixture(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/archive/f01-missing-cache.xlsx")
	require.NoError(t, err)
	digest := sha256.Sum256(data)
	for _, tc := range []struct {
		name      string
		file      string
		wantError bool
	}{
		{"basename", "inside.xlsx", false},
		{"existing parent path", "../outside.xlsx", true},
		{"missing parent path", "../missing.xlsx", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fixtures := filepath.Join(dir, "fixtures")
			require.NoError(t, os.Mkdir(fixtures, 0700))
			require.NoError(t, os.WriteFile(filepath.Join(fixtures, "inside.xlsx"), data, 0600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.xlsx"), data, 0600))
			manifest, err := json.Marshal(struct {
				Synthetic bool      `json:"synthetic_only"`
				Fixtures  []fixture `json:"fixtures"`
			}{true, []fixture{{ID: "F01-missing", File: tc.file, SHA256: hex.EncodeToString(digest[:])}}})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(fixtures, "manifest.json"), manifest, 0600))
			blobRoot := filepath.Join(dir, "originals")
			blobs, err := archive.NewBlobStore(blobRoot, 0)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, blobs.Close()) })
			priorArgs := os.Args
			t.Cleanup(func() { os.Args = priorArgs })
			os.Args = []string{"archives", "operations", dir, "formula", fixtures}

			_, err = formula(context.Background(), blobs, state{Home: "synthetic-home"}, dir)
			if !tc.wantError {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var pathErr *os.PathError
			require.False(t, errors.As(err, &pathErr), "invalid fixture name must be rejected before opening it")
			require.ErrorContains(t, err, "invalid fixture filename")
			entries, err := os.ReadDir(blobRoot)
			require.NoError(t, err)
			require.Empty(t, entries, "rejected fixture must not be stored")
		})
	}
}
