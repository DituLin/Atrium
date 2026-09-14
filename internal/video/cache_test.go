package video

import (
	"os"
	"testing"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/media"
	"github.com/stretchr/testify/require"
)

func TestCoverCacheBudgetAndOrphanCleanup(t *testing.T) {
	c := NewCoverCache(t.TempDir(), 10, 0, media.FakeDiskStats{FreeBytes: 1000})
	first, second := domain.NewID(), domain.NewID()
	require.NoError(t, c.Write(first, []byte("123456")))
	require.ErrorIs(t, c.Write(second, []byte("123456")), ErrCacheFull)
	require.NoError(t, os.WriteFile(c.files.Abs(".tmp-crash"), []byte("old"), 0600))
	require.NoError(t, c.Sweep(func() (map[string]bool, error) { return map[string]bool{first: true}, nil }))
	used, err := c.Bytes()
	require.NoError(t, err)
	require.EqualValues(t, 6, used)
	_, err = c.Read(first)
	require.NoError(t, err)
	require.NoError(t, c.Sweep(func() (map[string]bool, error) { return map[string]bool{}, nil }))
	_, err = c.Read(first)
	require.Error(t, err)
	require.NoError(t, c.Write(second, []byte("123456")))
	require.Error(t, c.Write("../escape", []byte("x")))
	low := NewCoverCache(t.TempDir(), 100, 100, media.FakeDiskStats{FreeBytes: 101})
	require.ErrorIs(t, low.Write(first, []byte("123")), ErrLowDisk)
}

func TestCoverCacheWaitsForExistingPhotoUsage(t *testing.T) {
	c := NewCoverCache(t.TempDir(), 10, 0, media.FakeDiskStats{FreeBytes: 1000})
	photoBytes := int64(98)
	c.SetSharedBudget(100, func() (int64, error) { return photoBytes, nil })
	require.ErrorIs(t, c.Write(domain.NewID(), []byte("123")), ErrCacheFull)
	photoBytes = 90
	require.NoError(t, c.Write(domain.NewID(), []byte("123")))
}

func TestCoverCacheShrinksExistingCoversToBudget(t *testing.T) {
	root := t.TempDir()
	old := NewCoverCache(root, 100, 0, media.FakeDiskStats{FreeBytes: 1000})
	first, second := domain.NewID(), domain.NewID()
	require.NoError(t, old.Write(first, []byte("123456")))
	require.NoError(t, old.Write(second, []byte("123456")))
	smaller := NewCoverCache(root, 10, 0, media.FakeDiskStats{FreeBytes: 1000})
	require.NoError(t, smaller.TrimToBudget())
	used, err := smaller.Bytes()
	require.NoError(t, err)
	require.LessOrEqual(t, used, int64(10))
}
