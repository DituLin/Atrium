package source_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestManagerProbeRecoversAfterNormalIOCapacityReturns(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "marker"), []byte("identity"), 0o600))
	fsys, err := source.NewOSFS(source.OSFSOptions{Root: root, IOTimeout: 25 * time.Millisecond, MaxInflight: 1})
	require.NoError(t, err)
	db := testutil.NewDB(t)
	cfg := &config.Config{Sources: []config.Source{{ID: "recovery", Name: "Recovery", Root: root, Identity: config.Identity{AllowLocal: true, MarkerFile: "marker"}}}}
	require.NoError(t, db.Sources().Upsert(ctx, "recovery", "Recovery", root, time.Now()))
	manager, err := source.NewManager(source.ManagerOptions{Config: cfg, DB: db, Logger: quietLogger(), NewFS: func(config.Source) (source.FS, error) { return fsys, nil }})
	require.NoError(t, err)
	require.True(t, manager.Probe(ctx, manager.Get("recovery")).OK())
	rel := blockingFIFO(t, root)
	_, err = fsys.Open(ctx, rel)
	require.ErrorIs(t, err, source.ErrStuck)
	_, err = fsys.Stat(ctx, "")
	require.ErrorIs(t, err, source.ErrDegraded)
	writer, err := os.OpenFile(filepath.Join(root, rel), os.O_WRONLY|syscall.O_NONBLOCK, 0)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.Eventually(t, func() bool { return fsys.Stats().Inflight == 0 }, time.Second, time.Millisecond)
	require.True(t, fsys.Degraded())
	// The real periodic Manager path must recover, including marker validation.
	probe := manager.Probe(ctx, manager.Get("recovery"))
	require.Equal(t, domain.HealthOnline, probe.Health)
	require.False(t, fsys.Degraded())
	_, err = fsys.ReadDir(ctx, "")
	require.NoError(t, err)
	// A missing marker must still fail closed; reserved capacity is not a bypass.
	require.NoError(t, os.Remove(filepath.Join(root, "marker")))
	require.Equal(t, source.DetailMarkerMissing, manager.Probe(ctx, manager.Get("recovery")).Detail)
}

func TestSequentialIdentityProbesReleaseCapacityBeforeReturning(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "marker"), []byte("identity"), 0o600))
	fsys, err := source.NewOSFS(source.OSFSOptions{Root: root, IOTimeout: time.Second, MaxInflight: 1})
	require.NoError(t, err)
	for i := 0; i < 10000; i++ {
		probe := source.CheckIdentity(context.Background(), fsys, source.IdentityConfig{AllowLocal: true, MarkerFile: "marker"})
		require.Truef(t, probe.OK(), "probe %d rejected successful sequential I/O: %s", i, probe.Detail)
		require.Zero(t, fsys.Stats().Inflight, "completed probe must release its capacity before returning")
	}
}
