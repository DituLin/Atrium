package source_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atritum/internal/source"
)

func newTree(t *testing.T) (string, *source.OSFS) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "album", "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "album", "a.jpg"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "album", "sub", "b.jpg"), []byte("bb"), 0o644))
	fsys, err := source.NewOSFS(source.OSFSOptions{Root: root, IOTimeout: 2 * time.Second, MaxInflight: 4})
	require.NoError(t, err)
	return root, fsys
}

func TestCleanRelRejectsTraversalAndAbsolutePaths(t *testing.T) {
	for _, bad := range []string{
		"..", "../etc/passwd", "album/../../etc/passwd", "/etc/passwd",
		"album/../..", "a\x00b", "/", "\\..\\etc",
	} {
		_, err := source.CleanRel(bad)
		assert.ErrorIs(t, err, source.ErrUnsafePath, "expected %q to be rejected", bad)
	}
	for in, want := range map[string]string{
		"":            "",
		".":           "",
		"album":       "album",
		"album/a.jpg": "album/a.jpg",
		"./album/./x": "album/x",
	} {
		got, err := source.CleanRel(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}

func TestOSFSRejectsTraversalOnEveryOperation(t *testing.T) {
	_, fsys := newTree(t)
	ctx := context.Background()

	_, err := fsys.Stat(ctx, "../../etc/passwd")
	assert.ErrorIs(t, err, source.ErrUnsafePath)
	_, err = fsys.ReadDir(ctx, "album/../..")
	assert.ErrorIs(t, err, source.ErrUnsafePath)
	_, err = fsys.Open(ctx, "/etc/passwd")
	assert.ErrorIs(t, err, source.ErrUnsafePath)
}

func TestOSFSNeverFollowsSymlinks(t *testing.T) {
	root, fsys := newTree(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "album", "link.jpg")))

	ctx := context.Background()
	_, err := fsys.Stat(ctx, "album/link.jpg")
	assert.ErrorIs(t, err, source.ErrSymlink)
	_, err = fsys.Open(ctx, "album/link.jpg")
	assert.Error(t, err)
	assert.Positive(t, fsys.Stats().SkippedSymlinks)

	entries, err := fsys.ReadDir(ctx, "album")
	require.NoError(t, err)
	var sawLink bool
	for _, e := range entries {
		if e.Name() == "link.jpg" {
			sawLink = true
			assert.NotZero(t, e.Type()&os.ModeSymlink, "walker must be able to see the link to skip it")
		}
	}
	assert.True(t, sawLink)
}

func TestOSFSReadsAndStats(t *testing.T) {
	_, fsys := newTree(t)
	ctx := context.Background()

	info, err := fsys.Stat(ctx, "album/a.jpg")
	require.NoError(t, err)
	assert.EqualValues(t, 1, info.Size())

	f, err := fsys.Open(ctx, "album/sub/b.jpg")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	buf := make([]byte, 2)
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "bb", string(buf[:n]))

	vol, err := fsys.Statfs(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, vol.TotalBytes, int64(0))
}

// blockingFIFO creates a named pipe. Opening it for reading blocks in the
// kernel until a writer appears, which reproduces a hung network syscall
// faithfully enough to exercise the deadline and the inflight cap.
func blockingFIFO(t *testing.T, root string) string {
	t.Helper()
	p := filepath.Join(root, "stuck.fifo")
	require.NoError(t, syscall.Mkfifo(p, 0o600))
	t.Cleanup(func() {
		// Unblock any parked helper goroutine so the test binary can exit.
		if w, err := os.OpenFile(p, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
	})
	return "stuck.fifo"
}

func TestOSFSDeadlineReturnsErrStuckAndAccounts(t *testing.T) {
	root := t.TempDir()
	fsys, err := source.NewOSFS(source.OSFSOptions{Root: root, IOTimeout: 50 * time.Millisecond, MaxInflight: 4})
	require.NoError(t, err)
	rel := blockingFIFO(t, root)

	start := time.Now()
	_, err = fsys.Open(context.Background(), rel)
	assert.ErrorIs(t, err, source.ErrStuck)
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.EqualValues(t, 1, fsys.Stats().StuckOps)
	assert.EqualValues(t, 1, fsys.Stats().ErrorOps)
}

func TestFakeFSHangTripsStuck(t *testing.T) {
	fake := source.NewFakeFS()
	fake.Hang = true
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := fake.Stat(ctx, "")
	assert.ErrorIs(t, err, source.ErrStuck)
}

func TestFakeFSInjectsPermissionAndDisappearance(t *testing.T) {
	fake := source.NewFakeFS()
	fake.AddFile("album/a.jpg", []byte("x"), time.Unix(100, 0))
	fake.Denied["album"] = true
	ctx := context.Background()

	_, err := fake.Stat(ctx, "album/a.jpg")
	assert.ErrorIs(t, err, os.ErrPermission)

	fake.Denied = map[string]bool{}
	info, err := fake.Stat(ctx, "album/a.jpg")
	require.NoError(t, err)
	assert.EqualValues(t, 1, info.Size())

	fake.Missing = true
	_, err = fake.ReadDir(ctx, "")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestOSFSInflightCapTripsDegraded(t *testing.T) {
	root := t.TempDir()
	fsys, err := source.NewOSFS(source.OSFSOptions{Root: root, IOTimeout: 5 * time.Second, MaxInflight: 2})
	require.NoError(t, err)
	rel := blockingFIFO(t, root)

	// Both slots are consumed by helpers parked in open(2); the callers give up
	// at their own deadline but the helpers keep the slots, which is the leak
	// the cap exists to bound.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, _ = fsys.Open(ctx, rel)
		}()
	}
	wg.Wait()

	require.Eventually(t, func() bool { return fsys.Stats().Inflight == 2 }, 2*time.Second, 10*time.Millisecond)
	_, err = fsys.Stat(context.Background(), "")
	assert.ErrorIs(t, err, source.ErrDegraded)
	assert.True(t, fsys.Degraded())

	// A probe still gets through on its reserved slot, and success clears it.
	_, err = fsys.ProbeStat(context.Background())
	require.NoError(t, err)
	fsys.ClearDegraded()
	assert.False(t, fsys.Degraded())
}
