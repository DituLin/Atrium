package logging_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/logging"
)

func readLog(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, logging.LogFileName))
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw[:len(raw)-1], &out))
	return out
}

func TestFileHandlerRedactsSensitiveKeysAtInfo(t *testing.T) {
	dir := t.TempDir()
	log, err := logging.New(logging.Options{Level: "info", Dir: dir, RetainDays: 7, MaxTotalMB: 200})
	require.NoError(t, err)

	log.Info("request",
		"token", "atr_scr_secretvalue",
		"authorization", "Bearer atr_adm_secret",
		"cookie", "atrium_screen=secret",
		"root_path", "/Volumes/photos/family",
		"rel_path", "2026/holiday/img.jpg",
		"mount_from", "//user@nas/photos",
		"screen_id", "living_room_tv",
	)
	require.NoError(t, log.Close())

	entry := readLog(t, dir)
	for _, key := range []string{"token", "authorization", "cookie", "root_path", "rel_path", "mount_from"} {
		require.Equal(t, logging.Placeholder, entry[key], "key %q must be redacted", key)
	}
	require.Equal(t, "living_room_tv", entry["screen_id"], "non-sensitive fields survive")
	require.Equal(t, "request", entry["msg"])
}

func TestDebugLevelKeepsRelPathButNotTokens(t *testing.T) {
	dir := t.TempDir()
	log, err := logging.New(logging.Options{Level: "debug", Dir: dir, RetainDays: 7, MaxTotalMB: 200})
	require.NoError(t, err)
	log.Debug("scan", "rel_path", "2026/a.jpg", "token", "atr_scr_x", "root_path", "/Volumes/photos")
	require.NoError(t, log.Close())

	entry := readLog(t, dir)
	require.Equal(t, "2026/a.jpg", entry["rel_path"], "debug may include rel_path")
	require.Equal(t, logging.Placeholder, entry["token"])
	require.Equal(t, logging.Placeholder, entry["root_path"])
}

func TestLevelFilteringAndFileCreation(t *testing.T) {
	dir := t.TempDir()
	log, err := logging.New(logging.Options{Level: "warn", Dir: dir, RetainDays: 7, MaxTotalMB: 200})
	require.NoError(t, err)
	log.Info("suppressed")
	log.Warn("kept")
	require.NoError(t, log.Close())

	entry := readLog(t, dir)
	require.Equal(t, "kept", entry["msg"])
}

func TestParseLevelAndIsSensitive(t *testing.T) {
	require.Equal(t, "DEBUG", logging.ParseLevel("debug").String())
	require.Equal(t, "INFO", logging.ParseLevel("").String())
	require.Equal(t, "WARN", logging.ParseLevel("warn").String())
	require.Equal(t, "ERROR", logging.ParseLevel("error").String())
	require.True(t, logging.IsSensitive("Authorization"))
	require.False(t, logging.IsSensitive("screen_id"))
	require.Equal(t, logging.Placeholder, logging.RedactValue("token", "secret"))
	require.Equal(t, "value", logging.RedactValue("other", "value"))
}
