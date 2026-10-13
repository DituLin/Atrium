package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/DituLin/Atrium/internal/app"
	"github.com/DituLin/Atrium/internal/cli"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func maintenanceConfig(t *testing.T, dataDir string) string {
	t.Helper()
	t.Setenv(config.EnvDataDir, "")
	path := filepath.Join(dataDir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("home:\n  timezone: UTC\n"), 0600))
	return path
}

func maintenanceDatabase(t *testing.T) (dataDir string, version int) {
	t.Helper()
	db := testutil.NewDB(t)
	version, err := db.SchemaVersion(context.Background())
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return filepath.Dir(db.Path()), version
}

func TestMaintenanceSuccessfulResults(t *testing.T) {
	for _, command := range []string{"schema", "backfill"} {
		t.Run(command, func(t *testing.T) {
			dataDir, version := maintenanceDatabase(t)
			configPath := maintenanceConfig(t, dataDir)
			out, err := run(t, "maintenance", command, "--config", configPath, "--data-dir", dataDir)
			require.NoError(t, err)
			if command == "schema" {
				var result struct {
					Compatible bool `json:"compatible"`
					Schema     int  `json:"schema"`
					Latest     int  `json:"latest"`
				}
				require.NoError(t, json.Unmarshal([]byte(out), &result))
				require.True(t, result.Compatible)
				require.Equal(t, version, result.Schema)
				require.Equal(t, version, result.Latest)
			} else {
				var result store.ArchiveBackfillStats
				require.NoError(t, json.Unmarshal([]byte(out), &result))
				require.Equal(t, store.ArchiveBackfillStats{}, result)
			}
		})
	}
}

type maintenanceFailingWriter struct{ err error }

func (w maintenanceFailingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestMaintenancePreservesOutputErrorAfterClosingDatabase(t *testing.T) {
	for _, command := range []string{"schema", "backfill"} {
		t.Run(command, func(t *testing.T) {
			dataDir, _ := maintenanceDatabase(t)
			configPath := maintenanceConfig(t, dataDir)
			outputErr := errors.New("synthetic output failure")
			root := cli.NewRoot()
			root.SetOut(maintenanceFailingWriter{err: outputErr})
			root.SetErr(io.Discard)
			root.SetArgs([]string{"maintenance", command, "--config", configPath, "--data-dir", dataDir})
			err := root.ExecuteContext(context.Background())
			require.ErrorIs(t, err, outputErr)
		})
	}
}

func TestMaintenanceRequiresExistingDatabase(t *testing.T) {
	for _, command := range []string{"schema", "backfill"} {
		t.Run(command, func(t *testing.T) {
			dataDir := t.TempDir()
			configPath := maintenanceConfig(t, dataDir)
			_, err := run(t, "maintenance", command, "--config", configPath, "--data-dir", dataDir)
			require.EqualError(t, err, "maintenance: existing database required")
			require.NoFileExists(t, app.NewLayout(dataDir).DB())
		})
	}
}
