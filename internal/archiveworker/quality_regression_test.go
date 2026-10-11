package archiveworker

import (
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerQualityRegressionFingerprintTrustedExecutableSymlink(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "node-real")
	link := filepath.Join(dir, "node")
	require.NoError(t, os.WriteFile(actual, []byte("executable-v1"), 0700))
	require.NoError(t, os.Symlink(actual, link))
	w := Worker{Adapter: archive.OpenClawUnderstandingAdapter{NodePath: link}}
	_, e := w.ConfigurationFingerprint()
	require.NoError(t, e, "trusted configured executable paths commonly are package-manager symlinks")
}
func TestWorkerQualityRegressionFingerprintActualHomebrewNode(t *testing.T) {
	if _, e := os.Lstat("/opt/homebrew/bin/node"); e != nil {
		t.Skip(e)
	}
	w := Worker{Adapter: archive.OpenClawUnderstandingAdapter{NodePath: "/opt/homebrew/bin/node"}}
	_, e := w.ConfigurationFingerprint()
	require.NoError(t, e)
}

func TestWorkerFingerprintSymlinkTargetAndTreeBoundaries(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "node-real")
	link := filepath.Join(dir, "node")
	require.NoError(t, os.WriteFile(target, []byte("one"), 0700))
	require.NoError(t, os.Symlink(target, link))
	w := Worker{Adapter: archive.OpenClawUnderstandingAdapter{NodePath: link}}
	first, e := w.ConfigurationFingerprint()
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(target, []byte("two"), 0700))
	second, e := w.ConfigurationFingerprint()
	require.NoError(t, e)
	require.NotEqual(t, first, second)
	tree := filepath.Join(dir, "plugin")
	require.NoError(t, os.Mkdir(tree, 0700))
	require.NoError(t, os.Symlink(target, filepath.Join(tree, "escape")))
	w = Worker{Adapter: archive.OpenClawUnderstandingAdapter{PluginPath: tree}}
	_, e = w.ConfigurationFingerprint()
	require.Error(t, e)
	broken := filepath.Join(dir, "broken")
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), broken))
	w = Worker{Adapter: archive.OpenClawUnderstandingAdapter{NodePath: broken}}
	_, e = w.ConfigurationFingerprint()
	require.Error(t, e)
	huge := filepath.Join(dir, "huge")
	f, e := os.Create(huge)
	require.NoError(t, e)
	require.NoError(t, f.Truncate((128<<20)+1))
	require.NoError(t, f.Close())
	w = Worker{Adapter: archive.OpenClawUnderstandingAdapter{NodePath: huge}}
	_, e = w.ConfigurationFingerprint()
	require.Error(t, e)
}
