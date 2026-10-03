package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	"github.com/stretchr/testify/require"
)

const intakePath = "/api/v1/integrations/archives/intakes"
const validIntakeMetadata = `{"kind":"text","channel":"cli","account":"synthetic","conversation":"test","sender":"test","provider_message_id":"one","slot_index":0,"original_name":"note.txt"}`

type archiveHTTPHarness struct {
	db     *store.DB
	server *httptest.Server
	token  string
	home   string
	cfg    *config.Config
}

func newArchiveHTTP(t *testing.T, enabled bool, permissions ...string) *archiveHTTPHarness {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Migrate(ctx)
	require.NoError(t, err)
	home, err := db.LocalHousehold(ctx)
	require.NoError(t, err)
	cfg := config.Defaults()
	cfg.Archives.Enabled = enabled
	cfg.Archives.MaxBytes = 1024
	cfg.Archives.UploadTimeout = config.Duration(150 * time.Millisecond)
	blobs, err := archive.NewBlobStore(filepath.Join(t.TempDir(), "originals"), 1024)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, blobs.Close()) })
	_, token, err := auth.NewIntegrationService(db, nil).Issue(ctx, "test", domain.IntegrationPolicy{Permissions: permissions, HouseholdIDs: []string{home.ID}}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	api := httpapi.New(httpapi.Deps{Config: cfg, DB: db, Auth: auth.NewAuthenticator(db, auth.NewOriginPolicy(nil), nil), Logger: logging.Discard(), Archives: archive.NewIntake(blobs, db.ArchiveTasks())})
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	return &archiveHTTPHarness{db, server, token, home.ID, cfg}
}
func archiveMultipart(t *testing.T, metadata, content, extra string) ([]byte, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	p, err := w.CreateFormField("metadata")
	require.NoError(t, err)
	_, err = io.WriteString(p, metadata)
	require.NoError(t, err)
	p, err = w.CreateFormFile("content", "original")
	require.NoError(t, err)
	_, err = io.WriteString(p, content)
	require.NoError(t, err)
	if extra != "" {
		p, err = w.CreateFormField(extra)
		require.NoError(t, err)
		_, err = io.WriteString(p, "{}")
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return b.Bytes(), w.FormDataContentType()
}
func (h *archiveHTTPHarness) request(t *testing.T, method, path, contentType string, body []byte, token string) (int, map[string]any, string) {
	t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := h.server.Client().Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	var data map[string]any
	_ = json.Unmarshal(raw, &data)
	return res.StatusCode, data, string(raw)
}
func TestArchiveHTTPEnabledAuthExactUploadLookupAndTask(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.ingest", "archive.read")
	body, ct := archiveMultipart(t, validIntakeMetadata, " 原文\r\n", "")
	status, _, _ := h.request(t, "POST", intakePath, ct, body, "")
	require.Equal(t, 401, status)
	status, result, _ := h.request(t, "POST", intakePath, ct, body, h.token)
	require.Equal(t, 200, status)
	data := result["data"].(map[string]any)
	require.EqualValues(t, len(" 原文\r\n"), data["size_bytes"])
	source := `{"kind":"text","channel":"cli","account":"synthetic","conversation":"test","sender":"test","provider_message_id":"one","slot_index":0}`
	status, lookup, _ := h.request(t, "POST", intakePath+"/lookup", "application/json", []byte(source), h.token)
	require.Equal(t, 200, status)
	require.Equal(t, data, lookup["data"])
	status, task, raw := h.request(t, "GET", "/api/v1/integrations/archives/tasks/"+data["task_id"].(string), "", nil, h.token)
	require.Equal(t, 200, status)
	require.Equal(t, "queued", task["data"].(map[string]any)["status"])
	require.NotContains(t, raw, "input_json")
	require.NotContains(t, raw, "lease")
	_, err := h.db.SQL().Exec(`UPDATE archive_tasks SET stage='/private/path',state_json='{"server_path":"/server"}',result_json='{"secret":"/secret","parse_status":"/bad"}',clarification_json='{"question":"/prompt"}'`)
	require.NoError(t, err)
	_, _, raw = h.request(t, "GET", "/api/v1/integrations/archives/tasks/"+data["task_id"].(string), "", nil, h.token)
	for _, secret := range []string{"/private", "/server", "/secret", "/bad", "/prompt"} {
		require.NotContains(t, raw, secret)
	}
}
func TestArchiveHTTPDisabledAndScoped(t *testing.T) {
	h := newArchiveHTTP(t, false, "archive.ingest")
	body, ct := archiveMultipart(t, validIntakeMetadata, "bytes", "")
	status, _, _ := h.request(t, "POST", intakePath, ct, body, h.token)
	require.Equal(t, 404, status)
	h = newArchiveHTTP(t, true, "archive.ingest")
	status, result, _ := h.request(t, "POST", intakePath, ct, body, h.token)
	require.Equal(t, 200, status)
	id := result["data"].(map[string]any)["task_id"].(string)
	status, _, _ = h.request(t, "GET", "/api/v1/integrations/archives/tasks/"+id, "", nil, h.token)
	require.Equal(t, 403, status)
	_, err := h.db.SQL().Exec(`INSERT INTO households(id) VALUES('unknown')`)
	require.NoError(t, err)
	_, other, err := auth.NewIntegrationService(h.db, nil).Issue(context.Background(), "other household", domain.IntegrationPolicy{Permissions: []string{"archive.ingest"}, HouseholdIDs: []string{"unknown"}}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	status, _, _ = h.request(t, "POST", intakePath, ct, body, other)
	require.Equal(t, 404, status)
}
func TestArchiveHTTPStrictEnvelopeNeverCommitsInvalidRequest(t *testing.T) {
	for _, bad := range []string{"unknown", "case-variant", "duplicate", "missing-slot", "null-slot", "extra-part", "duplicate-part", "truncated", "empty", "oversize", "wrong-size", "wrong-hash", "bad-intent", "metadata-second", "epilogue"} {
		t.Run(bad, func(t *testing.T) {
			h := newArchiveHTTP(t, true, "archive.ingest")
			meta := validIntakeMetadata
			content := "bytes"
			extra := ""
			switch bad {
			case "unknown":
				meta = strings.TrimSuffix(meta, "}") + `,"server_path":"/tmp/input"}`
			case "case-variant":
				meta = strings.TrimSuffix(meta, "}") + `,"Kind":"file"}`
			case "duplicate":
				meta = strings.TrimSuffix(meta, "}") + `,"kind":"file"}`
			case "missing-slot":
				meta = strings.Replace(meta, `,"slot_index":0`, "", 1)
			case "null-slot":
				meta = strings.Replace(meta, `"slot_index":0`, `"slot_index":null`, 1)
			case "extra-part":
				extra = "more"
			case "duplicate-part":
				extra = "metadata"
			case "empty":
				content = ""
			case "oversize":
				content = strings.Repeat("x", 1025)
			case "wrong-size":
				meta = strings.TrimSuffix(meta, "}") + `,"expected_size":8}`
			case "wrong-hash":
				meta = strings.TrimSuffix(meta, "}") + `,"expected_sha256":"` + strings.Repeat("0", 64) + `"}`
			case "bad-intent":
				meta = strings.TrimSuffix(meta, "}") + `,"visibility_intent":"public"}`
			}
			body, ct := archiveMultipart(t, meta, content, extra)
			if bad == "truncated" {
				body = body[:len(body)-8]
			}
			if bad == "metadata-second" {
				body = bytes.Replace(body, []byte(`name="metadata"`), []byte(`name="content"`), 1)
			}
			if bad == "epilogue" {
				body = append(body, []byte("unexpected")...)
			}
			status, _, raw := h.request(t, "POST", intakePath, ct, body, h.token)
			require.True(t, status == 400 || status == 413, "status=%d response=%s", status, raw)
			var n int
			require.NoError(t, h.db.SQL().QueryRow(`SELECT count(*) FROM archive_sources`).Scan(&n))
			require.Zero(t, n)
		})
	}
}
func TestArchiveHTTPBlockedSocketReadActuallyTimesOut(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.ingest")
	conn, err := net.Dial("tcp", strings.TrimPrefix(h.server.URL, "http://"))
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
	prefix := "--boundary\r\nContent-Disposition: form-data; name=\"metadata\"\r\n\r\n" + validIntakeMetadata + "\r\n--boundary\r\nContent-Disposition: form-data; name=\"content\"; filename=\"x\"\r\n\r\nbytes"
	start := time.Now()
	_, err = fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: test\r\nAuthorization: Bearer %s\r\nContent-Type: multipart/form-data; boundary=boundary\r\nContent-Length: 10000\r\n\r\n%s", intakePath, h.token, prefix)
	require.NoError(t, err)
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, 408, response.StatusCode)
	require.Less(t, time.Since(start), time.Second)
	var n int
	require.NoError(t, h.db.SQL().QueryRow(`SELECT count(*) FROM archive_sources`).Scan(&n))
	require.Zero(t, n)
}

func TestArchiveHTTPCurlStyleMetadataFilePartAndDeclaredLimit(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.ingest")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("metadata", "metadata.json")
	require.NoError(t, err)
	_, err = io.WriteString(part, validIntakeMetadata)
	require.NoError(t, err)
	part, err = writer.CreateFormFile("content", "note.txt")
	require.NoError(t, err)
	_, err = io.WriteString(part, "bytes")
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	status, _, raw := h.request(t, "POST", intakePath, writer.FormDataContentType(), body.Bytes(), h.token)
	require.Equal(t, 200, status, raw)
	metadata := strings.TrimSuffix(validIntakeMetadata, "}") + `,"expected_size":2048}`
	payload, ct := archiveMultipart(t, metadata, "bytes", "")
	status, _, raw = h.request(t, "POST", intakePath, ct, payload, h.token)
	require.Equal(t, 413, status, raw)
}

func TestArchiveHTTPImageOriginalBytesAndSniffedMIME(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.ingest")
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9ZlqAAAAAASUVORK5CYII=")
	require.NoError(t, err)
	meta := strings.Replace(validIntakeMetadata, `"kind":"text"`, `"kind":"file"`, 1)
	meta = strings.Replace(meta, `"note.txt"`, `"tiny.png"`, 1)
	meta = strings.TrimSuffix(meta, "}") + `,"declared_mime":"application/octet-stream"}`
	body, ct := archiveMultipart(t, meta, string(image), "")
	status, envelope, raw := h.request(t, "POST", intakePath, ct, body, h.token)
	require.Equal(t, 200, status, raw)
	receipt := envelope["data"].(map[string]any)
	require.EqualValues(t, len(image), receipt["size_bytes"])
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(image)), receipt["sha256"])
	var actualMIME string
	require.NoError(t, h.db.SQL().QueryRow(`SELECT mime FROM archive_blobs WHERE household_id=? AND sha256=?`, h.home, receipt["sha256"]).Scan(&actualMIME))
	require.Equal(t, "image/png", actualMIME)
}

func TestArchiveHTTPLostReceiptRecoveredWithoutResendingOriginal(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.ingest")
	body, ct := archiveMultipart(t, validIntakeMetadata, "lost reply bytes", "")
	req, err := http.NewRequest("POST", h.server.URL+intakePath, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+h.token)
	reply, err := h.server.Client().Do(req)
	require.NoError(t, err)
	require.NoError(t, reply.Body.Close()) // Caller discards the receipt bytes.
	lookup := strings.TrimSuffix(validIntakeMetadata, `,"original_name":"note.txt"}`) + "}"
	status, envelope, raw := h.request(t, "POST", intakePath+"/lookup", "application/json", []byte(lookup), h.token)
	require.Equal(t, 200, status, raw)
	var receiptID, taskID string
	require.NoError(t, h.db.SQL().QueryRow(`SELECT receipt_id,task_id FROM archive_sources`).Scan(&receiptID, &taskID))
	recovered := envelope["data"].(map[string]any)
	require.Equal(t, receiptID, recovered["receipt_id"])
	require.Equal(t, taskID, recovered["task_id"])
	require.Len(t, recovered, 6)
}
