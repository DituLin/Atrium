package source_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/source"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// managerFixture wires a manager around a single fake source.
func managerFixture(t *testing.T, ident config.Identity) (*store.DB, *source.Manager, *source.FakeFS, *events.Bus) {
	t.Helper()
	db := testutil.NewDB(t)
	fake := source.NewFakeFS()
	bus := events.NewBus()
	cfg := &config.Config{Sources: []config.Source{{
		ID: "family_photos", Name: "Family photos", Root: "/mnt/photos",
		IncludeExtensions: []string{"jpg", "JPEG"},
		Identity:          ident,
	}}}
	require.NoError(t, db.Sources().Upsert(context.Background(), "family_photos", "Family photos", "/mnt/photos", time.Now()))
	m, err := source.NewManager(source.ManagerOptions{
		Config: cfg, DB: db, Logger: quietLogger(), Bus: bus,
		NewFS: func(config.Source) (source.FS, error) { return fake, nil },
	})
	require.NoError(t, err)
	return db, m, fake, bus
}

func TestProbeBindsIdentityOnFirstSuccess(t *testing.T) {
	db, m, _, bus := managerFixture(t, config.Identity{RequireMount: true})
	ctx := context.Background()
	sub := bus.Subscribe("test", 8)
	defer sub.Close()

	p := m.Probe(ctx, m.Get("family_photos"))
	require.Equal(t, domain.HealthOnline, p.Health)

	row, err := db.Sources().Get(ctx, "family_photos")
	require.NoError(t, err)
	require.NotNil(t, row.IdentityBound)
	assert.Equal(t, "smbfs", row.IdentityBound.FSType)
	assert.NotEmpty(t, row.IdentityBound.MountFromHash)
	assert.True(t, row.IdentityBound.IsNetworkMount)
	assert.NotNil(t, row.LastSuccessAt)
	assert.NotNil(t, row.ShareTotalBytes)

	ev := <-sub.C()
	assert.Contains(t, ev.Topics, domain.TopicNAS)
}

func TestProbeRejectsLocalDirectoryWhenMountRequired(t *testing.T) {
	db, m, fake, _ := managerFixture(t, config.Identity{RequireMount: true})
	fake.SetVolume(source.VolumeStats{FSType: "apfs", MountFrom: "/dev/disk1s1", IsMountPoint: false})

	p := m.Probe(context.Background(), m.Get("family_photos"))
	assert.Equal(t, domain.HealthUnknown, p.Health)
	assert.Equal(t, source.DetailNotAMount, p.Detail)

	row, err := db.Sources().Get(context.Background(), "family_photos")
	require.NoError(t, err)
	assert.Nil(t, row.IdentityBound)
	assert.Nil(t, row.LastSuccessAt)
}

func TestAllowLocalBindsLocalDirectory(t *testing.T) {
	_, m, fake, _ := managerFixture(t, config.Identity{RequireMount: true, AllowLocal: true})
	fake.SetVolume(source.VolumeStats{FSType: "apfs", MountFrom: "/dev/disk1s1"})

	p := m.Probe(context.Background(), m.Get("family_photos"))
	require.Equal(t, domain.HealthOnline, p.Health)
	assert.False(t, p.Identity.IsNetworkMount)
	assert.True(t, m.Get("family_photos").Online())
}

func TestIdentityMismatchBlocksSourceUntilRebind(t *testing.T) {
	db, m, fake, _ := managerFixture(t, config.Identity{RequireMount: true})
	ctx := context.Background()
	entry := m.Get("family_photos")
	require.Equal(t, domain.HealthOnline, m.Probe(ctx, entry).Health)

	// The share was replaced by a different one under the same path.
	fake.SetVolume(source.VolumeStats{
		FSType: "smbfs", MountFrom: "//guest@other/photos", IsMountPoint: true,
	})
	p := m.Probe(ctx, entry)
	assert.Equal(t, domain.HealthUnknown, p.Health)
	assert.Equal(t, source.DetailIdentityMismatch, p.Detail)
	assert.True(t, entry.IdentityMismatch())
	assert.False(t, entry.Online())

	rebound, err := m.Rebind(ctx, "family_photos")
	require.NoError(t, err)
	assert.Equal(t, domain.HealthOnline, rebound.Health)
	assert.False(t, entry.IdentityMismatch())

	row, err := db.Sources().Get(ctx, "family_photos")
	require.NoError(t, err)
	assert.Equal(t, source.HashMountFrom("//guest@other/photos"), row.IdentityBound.MountFromHash)
}

func TestHealthTransitionsOfflineDegradedOnline(t *testing.T) {
	_, m, fake, _ := managerFixture(t, config.Identity{RequireMount: true})
	ctx := context.Background()
	entry := m.Get("family_photos")
	require.Equal(t, domain.HealthOnline, m.Probe(ctx, entry).Health)

	fake.Missing = true
	p := m.Probe(ctx, entry)
	assert.Equal(t, domain.HealthOffline, p.Health)
	assert.Equal(t, source.DetailRootMissing, p.Detail)

	fake.Missing = false
	fake.Denied[""] = true
	p = m.Probe(ctx, entry)
	assert.Equal(t, domain.HealthDegraded, p.Health)
	assert.Equal(t, source.DetailPermissionDenied, p.Detail)

	fake.Denied = map[string]bool{}
	assert.Equal(t, domain.HealthOnline, m.Probe(ctx, entry).Health)
}

func TestMarkerFileMustExist(t *testing.T) {
	_, m, fake, _ := managerFixture(t, config.Identity{RequireMount: true, MarkerFile: ".atrium-source"})
	ctx := context.Background()
	entry := m.Get("family_photos")

	p := m.Probe(ctx, entry)
	assert.Equal(t, domain.HealthUnknown, p.Health)
	assert.Equal(t, source.DetailMarkerMissing, p.Detail)

	fake.AddFile(".atrium-source", nil, time.Unix(0, 0))
	p = m.Probe(ctx, entry)
	require.Equal(t, domain.HealthOnline, p.Health)
	assert.True(t, p.Identity.Marker)
}

func TestLoadRestoresBoundIdentity(t *testing.T) {
	db, m, _, _ := managerFixture(t, config.Identity{RequireMount: true, AllowLocal: true})
	ctx := context.Background()
	m.Probe(ctx, m.Get("family_photos"))

	m2, err := source.NewManager(source.ManagerOptions{
		Config: &config.Config{Sources: []config.Source{{ID: "family_photos", Root: "/mnt/photos"}}},
		DB:     db, Logger: quietLogger(),
		NewFS: func(config.Source) (source.FS, error) { return source.NewFakeFS(), nil },
	})
	require.NoError(t, err)
	require.NoError(t, m2.Load(ctx))
	assert.NotNil(t, m2.Get("family_photos").Bound())
}

func TestRunProbesUntilContextCancelled(t *testing.T) {
	_, m, _, _ := managerFixture(t, config.Identity{RequireMount: true, AllowLocal: true})
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	m2 := m
	done := make(chan struct{})
	go func() { m2.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop with the context")
	}
	assert.True(t, m2.Get("family_photos").Online())
}

func TestShareStatsHiddenForLocalFilesystems(t *testing.T) {
	db, m, fake, _ := managerFixture(t, config.Identity{AllowLocal: true})
	fake.SetVolume(source.VolumeStats{FSType: "apfs", TotalBytes: 100, FreeBytes: 50})
	m.Probe(context.Background(), m.Get("family_photos"))
	row, err := db.Sources().Get(context.Background(), "family_photos")
	require.NoError(t, err)
	assert.Nil(t, row.ShareTotalBytes, "capacity is only meaningful for a network share (FR-12)")
}

// TestSubdirectoryOfNetworkShareIsAccepted covers the real deployment shape:
// the authorized root is `<mount>/Photos`, which shares its parent's device
// number and must still bind under require_mount.
func TestSubdirectoryOfNetworkShareIsAccepted(t *testing.T) {
	_, m, fake, _ := managerFixture(t, config.Identity{RequireMount: true})
	fake.SetVolume(source.VolumeStats{
		FSType: "smbfs", MountFrom: "//guest@nas/photos", IsMountPoint: false,
	})

	p := m.Probe(context.Background(), m.Get("family_photos"))
	require.Equal(t, domain.HealthOnline, p.Health)
	require.NotNil(t, p.Identity)
	assert.True(t, p.Identity.IsNetworkMount)
	assert.True(t, m.Get("family_photos").Online())
}

// TestLocalDirectoryOnAPFSStillRejected keeps the protection that matters: a
// share that failed to mount leaves an ordinary local directory behind.
func TestLocalDirectoryOnAPFSStillRejected(t *testing.T) {
	_, m, fake, _ := managerFixture(t, config.Identity{RequireMount: true})
	fake.SetVolume(source.VolumeStats{
		FSType: "apfs", MountFrom: "/dev/disk1s1", IsMountPoint: true,
	})

	p := m.Probe(context.Background(), m.Get("family_photos"))
	assert.Equal(t, domain.HealthUnknown, p.Health)
	assert.Equal(t, source.DetailNotAMount, p.Detail)
}

type gatedProbeFS struct {
	*source.FakeFS
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *gatedProbeFS) Statfs(ctx context.Context) (source.VolumeStats, error) {
	f.once.Do(func() { close(f.started) })
	select {
	case <-f.release:
	case <-ctx.Done():
		return source.VolumeStats{}, ctx.Err()
	}
	return f.FakeFS.Statfs(ctx)
}

func TestDelayedProbeCannotPublishAcrossAuthorizationChange(t *testing.T) {
	for _, operation := range []string{"probe", "rebind"} {
		for _, change := range []string{"revoke", "restore", "reconcile_revoked", "replace_root"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				db := testutil.NewDB(t)
				now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
				require.NoError(t, db.Sources().Upsert(t.Context(), "family", "Family", "/root/old", now))
				fs := &gatedProbeFS{FakeFS: source.NewFakeFS(), started: make(chan struct{}), release: make(chan struct{})}
				cfg := &config.Config{Sources: []config.Source{{ID: "family", Root: "/root/old", Identity: config.Identity{RequireMount: true}}}}
				m, err := source.NewManager(source.ManagerOptions{Config: cfg, DB: db, Logger: quietLogger(), Now: func() time.Time { return now }, NewFS: func(config.Source) (source.FS, error) { return fs, nil }})
				require.NoError(t, err)
				result := make(chan source.Probe, 1)
				go func() {
					if operation == "rebind" {
						p, _ := m.Rebind(t.Context(), "family")
						result <- p
					} else {
						result <- m.Probe(t.Context(), m.Get("family"))
					}
				}()
				select {
				case <-fs.started:
				case <-time.After(time.Second):
					t.Fatal("probe did not start")
				}
				if change != "replace_root" {
					require.NoError(t, db.Sources().Revoke(t.Context(), "family", "revoked", now))
				}
				switch change {
				case "restore":
					require.NoError(t, db.Sources().Restore(t.Context(), "family", now))
				case "reconcile_revoked":
					require.NoError(t, db.Sources().Upsert(t.Context(), "family", "Family", "/root/old", now))
				case "replace_root":
					require.NoError(t, db.Sources().Upsert(t.Context(), "family", "Family", "/root/new", now))
				}
				close(fs.release)
				select {
				case <-result:
				case <-time.After(time.Second):
					t.Fatal("probe did not finish")
				}
				row, err := db.Sources().Get(t.Context(), "family")
				require.NoError(t, err)
				require.Nil(t, row.LastCheckAt, "old in-flight observation must not be published")
				require.Nil(t, row.LastSuccessAt)
				require.Nil(t, row.ShareStatsAt)
				require.Nil(t, row.IdentityBound, "old in-flight identity must not be bound")
				require.False(t, m.Get("family").Online())
				if change == "restore" || change == "reconcile_revoked" {
					require.Equal(t, domain.HealthOnline, m.Probe(t.Context(), m.Get("family")).Health)
					row, err = db.Sources().Get(t.Context(), "family")
					require.NoError(t, err)
					require.NotNil(t, row.LastCheckAt, "a new check under restored authorization can publish")
				}
			})
		}
	}
}
