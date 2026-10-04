package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/app"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/stretchr/testify/require"
)

func archiveRuntime(t *testing.T, enabled bool) (*app.Runtime, string) {
	t.Helper()
	cfg := testConfig(t, func(c *config.Config) {
		c.Server.TLS.Mode = config.TLSOff
		c.Archives.Enabled = enabled
		c.Storage.MinFreeBytes = 0
	})
	rt, err := app.New(context.Background(), app.Options{Config: cfg, Logger: logging.Discard()})
	require.NoError(t, err)
	home, err := rt.DB().LocalHousehold(context.Background())
	require.NoError(t, err)
	_, token, err := auth.NewIntegrationService(rt.DB(), nil).Issue(context.Background(), "runtime intake", domain.IntegrationPolicy{Permissions: []string{"archive.ingest", "archive.read"}, HouseholdIDs: []string{home.ID}}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, rt.Start(context.Background()))
	return rt, token
}

const runtimeMetadata = `{"kind":"text","channel":"cli","account":"synthetic","conversation":"test","sender":"test","provider_message_id":"one","slot_index":0,"original_name":"note.txt"}`

func TestRuntimeArchiveFeatureLifecycleAndRoundTrip(t *testing.T) {
	off, _ := archiveRuntime(t, false)
	require.NoDirExists(t, off.Layout().Archives())
	require.NoError(t, off.Shutdown())
	rt, token := archiveRuntime(t, true)
	defer rt.Shutdown()
	require.DirExists(t, rt.Layout().Archives())
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	p, err := writer.CreateFormField("metadata")
	require.NoError(t, err)
	_, err = io.WriteString(p, runtimeMetadata)
	require.NoError(t, err)
	p, err = writer.CreateFormFile("content", "note.txt")
	require.NoError(t, err)
	_, err = io.WriteString(p, "synthetic original\r\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request := func(method, path, ct string, body io.Reader) map[string]any {
		req, e := http.NewRequest(method, "http://"+rt.Addr()+path, body)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", ct)
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		raw, e := io.ReadAll(res.Body)
		require.NoError(t, e)
		require.Equal(t, 200, res.StatusCode, string(raw))
		var envelope map[string]any
		require.NoError(t, json.Unmarshal(raw, &envelope))
		return envelope["data"].(map[string]any)
	}
	received := request("POST", "/api/v1/integrations/archives/intakes", writer.FormDataContentType(), &body)
	lookup := request("POST", "/api/v1/integrations/archives/intakes/lookup", "application/json", strings.NewReader(strings.TrimSuffix(runtimeMetadata, `,"original_name":"note.txt"}`)+"}"))
	require.Equal(t, received, lookup)
	task := request("GET", "/api/v1/integrations/archives/tasks/"+received["task_id"].(string), "", nil)
	require.Equal(t, "queued", task["status"])
	require.Equal(t, "pending", task["nas_status"])
}
func TestRuntimeArchiveInitializationFailureReleasesDataLock(t *testing.T) {
	cfg := testConfig(t, func(c *config.Config) { c.Server.TLS.Mode = config.TLSOff; c.Archives.Enabled = true })
	layout := app.NewLayout(cfg.Storage.DataDir)
	require.NoError(t, layout.Ensure())
	require.NoError(t, os.Symlink(t.TempDir(), layout.Archives()))
	_, err := app.New(context.Background(), app.Options{Config: cfg, Logger: logging.Discard()})
	require.Error(t, err)
	require.NoError(t, os.Remove(layout.Archives()))
	rt, err := app.New(context.Background(), app.Options{Config: cfg, Logger: logging.Discard()})
	require.NoError(t, err)
	require.NoError(t, rt.Shutdown())
}
func TestRuntimeShutdownClosesBlockedArchiveUpload(t *testing.T) {
	rt, token := archiveRuntime(t, true)
	conn, err := net.Dial("tcp", rt.Addr())
	require.NoError(t, err)
	defer conn.Close()
	prefix := "--boundary\r\nContent-Disposition: form-data; name=\"metadata\"\r\n\r\n" + runtimeMetadata + "\r\n--boundary\r\nContent-Disposition: form-data; name=\"content\"; filename=\"x\"\r\n\r\nbytes"
	_, err = fmt.Fprintf(conn, "POST /api/v1/integrations/archives/intakes HTTP/1.1\r\nHost: test\r\nAuthorization: Bearer %s\r\nContent-Type: multipart/form-data; boundary=boundary\r\nContent-Length: 10000\r\n\r\n%s", token, prefix)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		names, _ := filepath.Glob(filepath.Join(rt.Layout().Archives(), ".staging-*"))
		return len(names) > 0
	}, 2*time.Second, time.Millisecond)
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- rt.Shutdown() }()
	select {
	case err := <-done:
		require.Error(t, err, "graceful HTTP shutdown exceeded its budget")
	case <-time.After(app.ShutdownTimeout + 3*time.Second):
		t.Fatal("shutdown hung behind blocked archive reader")
	}
	require.Less(t, time.Since(start), app.ShutdownTimeout+3*time.Second)
	staging, err := filepath.Glob(filepath.Join(rt.Layout().Archives(), ".staging-*"))
	require.NoError(t, err)
	require.Empty(t, staging)
}
func TestRuntimeArchiveConsumerProjectsLegacyOutbox(t *testing.T) {
	rt, _ := archiveRuntime(t, true)
	defer rt.Shutdown()
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, rt.DB().FamilyRecords().PutMember(ctx, domain.FamilyMember{ID: "synthetic", Name: "Synthetic"}, now))
	_, _, e := rt.DB().FamilyRecords().Record(ctx, "test", "runtime-archive", domain.FamilyRecordInput{MemberID: "synthetic", Category: "growth", Title: "height", OccurredAt: now, DisplayOnTV: true}, now)
	require.NoError(t, e)
	require.Eventually(t, func() bool {
		var n int
		return rt.DB().SQL().QueryRow(`SELECT count(*) FROM archive_legacy_outbox WHERE status='completed'`).Scan(&n) == nil && n == 1
	}, 3*time.Second, 20*time.Millisecond)
}
