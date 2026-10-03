package app_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DituLin/Atrium/internal/app"
	"github.com/stretchr/testify/require"
)

func TestLayoutArchivesIsSeparateFromCachesAndEnsure(t *testing.T) {
	l := app.NewLayout(filepath.Join(t.TempDir(), "data"))
	require.Equal(t, filepath.Join(l.Root, "archives"), l.Archives())
	require.NoError(t, l.Ensure())
	require.NoDirExists(t, l.Archives(), "only the blob initializer creates the archive root")
	target := t.TempDir()
	require.NoError(t, os.Chmod(target, 0o750))
	require.NoError(t, os.Symlink(target, l.Archives()))
	require.NoError(t, l.Ensure())
	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o750), info.Mode().Perm(), "Ensure must not follow or chmod archive symlinks")
}
