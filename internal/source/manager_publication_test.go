package source

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type observedFS struct {
	*FakeFS
	observed chan struct{}
}

func (f *observedFS) Statfs(ctx context.Context) (VolumeStats, error) {
	volume, err := f.FakeFS.Statfs(ctx)
	f.observed <- struct{}{}
	return volume, err
}

func TestSourcePublicationCannotCommitAheadOfRuntimeState(t *testing.T) {
	for _, operation := range []string{"probe", "rebind"} {
		t.Run(operation, func(t *testing.T) {
			db := testutil.NewDB(t)
			fs := &observedFS{FakeFS: NewFakeFS(), observed: make(chan struct{}, 2)}
			require.NoError(t, db.Sources().Upsert(t.Context(), "family", "Family", "/fake/root", time.Now()))
			manager, err := NewManager(ManagerOptions{
				Config: &config.Config{Sources: []config.Source{{ID: "family", Root: "/fake/root", Identity: config.Identity{RequireMount: true}}}},
				DB:     db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				NewFS: func(config.Source) (FS, error) { return fs, nil },
			})
			require.NoError(t, err)
			entry := manager.Get("family")
			// Hold runtime readers/writers at the publication boundary. Another
			// observation must not see a committed new binding before this source can
			// publish that binding and health together.
			entry.mu.Lock()
			result := make(chan Probe, 1)
			go func() {
				if operation == "rebind" {
					p, _ := manager.Rebind(t.Context(), "family")
					result <- p
				} else {
					result <- manager.Probe(t.Context(), entry)
				}
			}()
			select {
			case <-fs.observed: // Slow I/O must remain outside the publication lock.
			case <-time.After(time.Second):
				entry.mu.Unlock()
				t.Fatal("filesystem check blocked on runtime publication")
			}
			assert.Never(t, func() bool {
				row, readErr := db.Sources().Get(t.Context(), "family")
				return readErr != nil || row.LastCheckAt != nil || row.IdentityBound != nil
			}, 100*time.Millisecond, time.Millisecond, "durable and runtime publication must share one serialized boundary")
			entry.mu.Unlock()
			select {
			case p := <-result:
				require.True(t, p.OK())
			case <-time.After(time.Second):
				t.Fatal("publication did not finish")
			}
			// A later check of another mount must leave both views blocked. An earlier
			// rebind publication cannot resume afterward and turn the scanner online.
			fs.SetVolume(VolumeStats{FSType: "smbfs", MountFrom: "//other/photos", IsMountPoint: true})
			require.Equal(t, DetailIdentityMismatch, manager.Probe(t.Context(), entry).Detail)
			require.False(t, entry.Online())
			require.True(t, entry.IdentityMismatch())
			row, err := db.Sources().Get(t.Context(), "family")
			require.NoError(t, err)
			require.Equal(t, domain.HealthUnknown, row.Health)
		})
	}
}

func TestSourceRecoveryCannotPublishBeforeRuntimeState(t *testing.T) {
	db := testutil.NewDB(t)
	root := t.TempDir()
	fs, err := NewOSFS(OSFSOptions{Root: root})
	require.NoError(t, err)
	fs.degraded.Store(true)
	require.NoError(t, db.Sources().Upsert(t.Context(), "local", "Local test", root, time.Now()))
	manager, err := NewManager(ManagerOptions{
		Config: &config.Config{Sources: []config.Source{{ID: "local", Root: root, Identity: config.Identity{AllowLocal: true}}}},
		DB:     db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), NewFS: func(config.Source) (FS, error) { return fs, nil },
	})
	require.NoError(t, err)
	entry := manager.Get("local")
	entry.mu.Lock()
	lease, err := db.Sources().BeginObservation(t.Context(), "local", root)
	require.NoError(t, err)
	done := make(chan bool, 1)
	go func() {
		committed, _, _ := manager.publishObservation(t.Context(), entry, lease, store.SourceObservation{Health: domain.HealthOnline, Success: true, At: time.Now()}, false, false)
		done <- committed
	}()
	assert.Never(t, func() bool { return !fs.Degraded() }, 100*time.Millisecond, time.Millisecond, "I/O recovery belongs to runtime publication")
	entry.mu.Unlock()
	select {
	case committed := <-done:
		require.True(t, committed)
	case <-time.After(time.Second):
		t.Fatal("publication did not finish")
	}
	require.False(t, fs.Degraded())
	require.True(t, entry.Online())
}
