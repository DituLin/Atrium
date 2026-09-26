package indexer_test

import (
	"context"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/stretchr/testify/require"
	"io/fs"
	"testing"
)

type lockProbeFS struct {
	source.FS
	probe func()
}

func (f lockProbeFS) ReadDir(ctx context.Context, rel string) ([]fs.DirEntry, error) {
	f.probe()
	entries, err := f.FS.ReadDir(ctx, rel)
	out := make([]fs.DirEntry, len(entries))
	for i, e := range entries {
		out[i] = lockProbeEntry{e, f.probe}
	}
	return out, err
}
func (f lockProbeFS) Stat(ctx context.Context, rel string) (fs.FileInfo, error) {
	f.probe()
	return f.FS.Stat(ctx, rel)
}

type lockProbeEntry struct {
	fs.DirEntry
	probe func()
}

func (e lockProbeEntry) Info() (fs.FileInfo, error) { e.probe(); return e.DirEntry.Info() }

// Network operations may block for seconds. They must never execute while an
// index transaction owns SQLite's writer lock: health/commands/job completion
// need to remain writable even with a slow SMB share.
func TestScanDoesNotHoldWriteLockDuringSourceIO(t *testing.T) {
	for _, checks := range []int{1, 2} {
		t.Run(string(rune('0'+checks)), func(t *testing.T) {
			fx := newFixture(t, func(c *config.Source) { c.Scan.StabilityChecks = checks })
			fx.add("a.jpg", 10, mod)
			fx.add("b.jpg", 10, mod)
			fx.add("sub/c.jpg", 10, mod)
			ctx := context.Background()
			conn, err := fx.db.SQL().Conn(ctx)
			require.NoError(t, err)
			defer conn.Close()
			_, err = conn.ExecContext(ctx, "PRAGMA busy_timeout=20")
			require.NoError(t, err)
			probes := 0
			fx.manager.Get(srcID).FS = lockProbeFS{fx.fs, func() {
				probes++
				_, err := conn.ExecContext(ctx, "UPDATE data_sources SET name=name WHERE id=?", srcID)
				require.NoError(t, err, "source IO must not hold SQLite writer lock")
			}}
			fx.scan(t, domain.ScanScheduled)
			require.Greater(t, probes, 3)
		})
	}
}
