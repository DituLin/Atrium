package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/config"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/httpapi"
	"github.com/DituLin/Atrium/internal/logging"
	"github.com/DituLin/Atrium/internal/store"
	"github.com/DituLin/Atrium/internal/store/testutil"
	"github.com/stretchr/testify/require"
)

func TestArchiveScreenHTTPCurrentPublicReadOnly(t *testing.T) {
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	actor := archiveActor(t, d)
	rec, f, x := publicationFixture(t, d, r, "http-screen", domain.ArchiveInputFile)
	f = saveBundle(t, r, f, publicationBundle(t, "70"))
	_, err := r.PublishExtraction(ctx, f, x.ID)
	require.NoError(t, err)
	_, err = d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, err)
	token, err := auth.NewScreenToken()
	require.NoError(t, err)
	require.NoError(t, d.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: auth.HashToken(token)}))
	cfg := config.Defaults()
	cfg.Archives.Enabled = true
	api := httpapi.New(httpapi.Deps{Config: cfg, DB: d, Archives: archive.NewIntake(nil, d.ArchiveTasks()), Auth: auth.NewAuthenticator(d, auth.NewOriginPolicy([]string{"https://core"}), nil), Logger: logging.Discard()})
	call := func(method, path string, cookie bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "https://core/api/v1/archives/"+path, nil)
		if cookie {
			req.AddCookie(&http.Cookie{Name: auth.ScreenCookieName, Value: token})
			req.Header.Set("Referer", "https://core/")
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		api.Handler().ServeHTTP(out, req)
		return out
	}
	for _, cookie := range []bool{false, true} {
		out := call("GET", "documents/"+rec.DocumentID, cookie)
		require.Equal(t, 200, out.Code)
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
		require.NotContains(t, out.Body.String(), "task_status")
		require.NotContains(t, out.Body.String(), "provider_message")
		require.Equal(t, 200, call("HEAD", "documents/"+rec.DocumentID, cookie).Code)
		require.Equal(t, 400, call("GET", "documents/"+rec.DocumentID+"?household_id=other", cookie).Code)
		require.Contains(t, []int{404, 405}, call("POST", "documents/"+rec.DocumentID, cookie).Code)
	}
	for _, path := range []string{"versions/01/pages/1", "versions/1/pages/0", "versions/1/pages/101", "versions/1/pages/1?path=/tmp/private", "versions/1/pages/1?hash=" + strings.Repeat("a", 64)} {
		require.Equal(t, 400, call("GET", "documents/"+rec.DocumentID+"/"+path, false).Code)
	}
	detail, err := r.ArchiveDocumentForScreen(ctx, "tv", auth.HashToken(token), "home-1", rec.DocumentID)
	require.NoError(t, err)
	_, err = r.ReviseArchive(ctx, actor, "home-1", rec.DocumentID, detail.Item.DocumentRevision, store.ArchiveRevision{Kind: "withdraw", Reason: "test"})
	require.NoError(t, err)
	require.Equal(t, 404, call("GET", "documents/"+rec.DocumentID, false).Code)
	require.NoError(t, d.Screens().Revoke(ctx, "tv", archiveNow))
	require.Equal(t, 410, call("GET", "documents/"+rec.DocumentID, false).Code)
}

func TestArchivePreviewRechecksWithdrawalAfterRenderer(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native process boundary")
	}
	d := testutil.NewDB(t)
	ctx := context.Background()
	r := d.ArchiveTasksWithClock(&archiveClock{now: archiveNow})
	actor := archiveActor(t, d)
	_, e := d.SQL().Exec(`UPDATE local_household SET household_id='home-1'`)
	require.NoError(t, e)
	blobs, e := archive.NewBlobStore(filepath.Join(t.TempDir(), "originals"), 32<<20)
	require.NoError(t, e)
	defer blobs.Close()
	var pixels bytes.Buffer
	require.NoError(t, png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	ref, e := blobs.Put(ctx, "home-1", bytes.NewReader(pixels.Bytes()), archive.PutOptions{})
	require.NoError(t, e)
	rec, e := r.AcceptIntake(ctx, actor, "home-1", 1, domain.ArchiveIntakeMetadata{ArchiveSourceIdentity: domain.ArchiveSourceIdentity{Kind: domain.ArchiveInputFile, Channel: "test", Account: "test", Conversation: "test", Sender: "test", ProviderMessageID: "renderer-race"}, OriginalName: "synthetic.png"}, ref)
	require.NoError(t, e)
	token, e := auth.NewScreenToken()
	require.NoError(t, e)
	require.NoError(t, d.Screens().Create(ctx, &domain.Screen{ID: "tv", Name: "TV", TokenHash: auth.HashToken(token)}))
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	output := filepath.Join(dir, "output.json")
	helper := filepath.Join(dir, "helper")
	body, e := json.Marshal(map[string]any{"protocol": 1, "page": 1, "total_pages": 1, "png": pixels.Bytes()})
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(output, body, 0600))
	script := fmt.Sprintf("#!/bin/sh\ntouch %q\nwhile [ ! -f %q ]; do sleep 0.01; done\ncat %q\n", ready, release, output)
	require.NoError(t, os.WriteFile(helper, []byte(script), 0700))
	cfg := config.Defaults()
	cfg.Archives.Enabled = true
	api := httpapi.New(httpapi.Deps{Config: cfg, DB: d, Archives: archive.NewIntake(blobs, d.ArchiveTasks()), ArchiveBlobs: blobs, ArchivePreview: archive.NativeParser{HelperPath: helper}, Auth: auth.NewAuthenticator(d, auth.NewOriginPolicy(nil), nil), Logger: logging.Discard()})
	req := httptest.NewRequest("GET", "https://core/api/v1/archives/documents/"+rec.DocumentID+"/versions/1/pages/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	out := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); api.Handler().ServeHTTP(out, req) }()
	require.Eventually(t, func() bool { _, e := os.Stat(ready); return e == nil }, 2*time.Second, 10*time.Millisecond)
	_, e = r.ReviseArchive(ctx, actor, "home-1", rec.DocumentID, 1, store.ArchiveRevision{Kind: "withdraw", Reason: "withdraw while renderer runs"})
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(release, nil, 0600))
	<-done
	require.Equal(t, 404, out.Code)
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	require.NotEqual(t, "image/png", out.Header().Get("Content-Type"))
}
