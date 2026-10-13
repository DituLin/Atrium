package archiveworker

import (
	"github.com/DituLin/Atrium/internal/archive"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredProviderSDKLinkTracksTrustedRuntime(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "openclaw")
	provider := filepath.Join(root, "provider")
	require.NoError(t, os.MkdirAll(filepath.Join(runtime, "dist"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(provider, "node_modules"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(runtime, "package.json"), []byte(`{"name":"openclaw","version":"test"}`), 0600))
	entry := filepath.Join(runtime, "openclaw.mjs")
	require.NoError(t, os.WriteFile(entry, []byte("entry"), 0600))
	sdk := filepath.Join(runtime, "dist", "sdk.js")
	require.NoError(t, os.WriteFile(sdk, []byte("sdk-v1"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(provider, "index.js"), []byte("provider"), 0600))
	require.NoError(t, os.Symlink(runtime, filepath.Join(provider, "node_modules", "openclaw")))
	w := Worker{Adapter: archive.OpenClawUnderstandingAdapter{OpenClawPath: entry, ProviderPath: provider}}
	first, e := w.ConfigurationFingerprint()
	require.NoError(t, e, "installed provider uses an explicit peer SDK link to configured runtime")
	require.NoError(t, os.WriteFile(sdk, []byte("sdk-v2"), 0600))
	second, e := w.ConfigurationFingerprint()
	require.NoError(t, e)
	require.NotEqual(t, first, second, "SDK bytes participate, not only link name")
	require.NoError(t, os.Remove(filepath.Join(provider, "node_modules", "openclaw")))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(provider, "node_modules", "openclaw")))
	_, e = w.ConfigurationFingerprint()
	require.Error(t, e, "different SDK runtime is not trusted")
}
func TestConfiguredRuntimeRejectsEscapingDependencyLink(t *testing.T) {
	runtime := t.TempDir()
	entry := filepath.Join(runtime, "openclaw.mjs")
	require.NoError(t, os.WriteFile(entry, []byte("entry"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(runtime, "package.json"), []byte(`{"name":"openclaw"}`), 0600))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(runtime, "escape")))
	w := Worker{Adapter: archive.OpenClawUnderstandingAdapter{OpenClawPath: entry}}
	_, e := w.ConfigurationFingerprint()
	require.Error(t, e)
}

// The deployed plugin must carry its actual implementation and inference
// resources inside the directory whose bytes ConfigurationFingerprint binds.
func TestPackagedWorkerOwnsInferenceResources(t *testing.T) {
	root := filepath.Join("..", "..", "adapters", "openclaw", "archive-worker-plugin")
	index, e := os.ReadFile(filepath.Join(root, "index.js"))
	require.NoError(t, e)
	require.Contains(t, string(index), `from "./worker.mjs"`)
	worker, e := os.ReadFile(filepath.Join(root, "worker.mjs"))
	require.NoError(t, e)
	require.Contains(t, string(worker), `from "./resources-v2.mjs"`)
	_, e = os.Stat(filepath.Join(root, "resources-v2.mjs"))
	require.NoError(t, e)
}
