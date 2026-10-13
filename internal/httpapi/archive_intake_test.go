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
	return newArchiveHTTPWithMax(t, enabled, 1024, permissions...)
}

func newArchiveHTTPWithMax(t *testing.T, enabled bool, maxBytes int64, permissions ...string) *archiveHTTPHarness {
	t.Helper()
	// Envelope and receipt tests include real SQLite and blob persistence work.
	// Keep their deadline separate from tests that deliberately stall the body.
	return newArchiveHTTPWithUploadTimeout(t, enabled, maxBytes, 10*time.Second, permissions...)
}

func newArchiveHTTPWithUploadTimeout(t *testing.T, enabled bool, maxBytes int64, uploadTimeout time.Duration, permissions ...string) *archiveHTTPHarness {
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
	cfg.Archives.MaxBytes = maxBytes
	cfg.Archives.UploadTimeout = config.Duration(uploadTimeout)
	blobs, err := archive.NewBlobStore(filepath.Join(t.TempDir(), "originals"), maxBytes)
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
	h := newArchiveHTTPWithUploadTimeout(t, true, 1024, 150*time.Millisecond, "archive.ingest")
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

func TestArchiveHTTPOptionalAssertionPresence(t *testing.T) {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("bytes")))
	for _, tc := range []struct {
		name, fields string
		status       int
	}{
		{"omitted", "", http.StatusOK},
		{"hash_omitted_with_inherit", `,"visibility_intent":"inherit"`, http.StatusOK},
		{"intent_omitted_with_hash", `,"expected_sha256":"` + hash + `"`, http.StatusOK},
		{"empty_declared_mime_allowed", `,"declared_mime":""`, http.StatusOK},
		{"empty_hash", `,"expected_sha256":""`, http.StatusBadRequest},
		{"empty_intent", `,"visibility_intent":""`, http.StatusBadRequest},
		{"mixed_case_hash", `,"expected_sha256":"","Expected_SHA256":"` + hash + `"`, http.StatusBadRequest},
		{"mixed_case_intent", `,"visibility_intent":"","Visibility_Intent":"inherit"`, http.StatusBadRequest},
		{"duplicate_hash", `,"expected_sha256":"","expected_sha256":"` + hash + `"`, http.StatusBadRequest},
		{"duplicate_intent", `,"visibility_intent":"","visibility_intent":"inherit"`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newArchiveHTTP(t, true, "archive.ingest")
			metadata := strings.TrimSuffix(validIntakeMetadata, "}") + tc.fields + "}"
			body, contentType := archiveMultipart(t, metadata, "bytes", "")
			status, _, raw := h.request(t, "POST", intakePath, contentType, body, h.token)
			require.Equal(t, tc.status, status, raw)
			expectedRows := 0
			if tc.status == http.StatusOK {
				expectedRows = 1
			}
			for _, table := range []string{"archive_sources", "archive_tasks"} {
				var count int
				require.NoError(t, h.db.SQL().QueryRow(`SELECT count(*) FROM `+table).Scan(&count))
				require.Equal(t, expectedRows, count, table)
			}
		})
	}
}

func TestArchiveHTTPActualMultipartOverheadBound(t *testing.T) {
	for _, overhead := range []int{128*1024 - 1, 128 * 1024, 128*1024 + 1, 512 * 1024} {
		t.Run(fmt.Sprint(overhead), func(t *testing.T) {
			h := newArchiveHTTPWithMax(t, true, 32<<20, "archive.ingest")
			body, ct := archiveMultipart(t, validIntakeMetadata, "bytes", "")
			header := "Content-Type: application/json; pad="
			padding := overhead - (len(body) - len("bytes")) - len(header) - 2
			require.Positive(t, padding)
			header += strings.Repeat("x", padding) + "\r\n"
			position := bytes.Index(body, []byte("\r\n\r\n")) + 2
			body = append(append(append([]byte{}, body[:position]...), []byte(header)...), body[position:]...)
			require.Equal(t, overhead, len(body)-len("bytes"))
			status, _, raw := h.request(t, "POST", intakePath, ct, body, h.token)
			expectedStatus, expectedRows := http.StatusOK, 1
			if overhead > 128*1024 {
				expectedStatus, expectedRows = http.StatusRequestEntityTooLarge, 0
			}
			require.Equal(t, expectedStatus, status, raw)
			for _, table := range []string{"archive_sources", "archive_tasks"} {
				var count int
				require.NoError(t, h.db.SQL().QueryRow(`SELECT count(*) FROM `+table).Scan(&count))
				require.Equal(t, expectedRows, count, table)
			}
		})
	}
}

func TestArchiveHTTPFirstWhitespaceTerminatorRejectsEpilogue(t *testing.T) {
	for _, space := range []string{" ", "\t", " \t "} {
		for _, epilogue := range []string{"none", "text", "later_delimiter", "extra_part"} {
			t.Run(fmt.Sprintf("%q/%s", space, epilogue), func(t *testing.T) {
				h := newArchiveHTTP(t, true, "archive.ingest")
				body, ct := archiveMultipart(t, validIntakeMetadata, "bytes", "")
				_, boundary, ok := strings.Cut(ct, "boundary=")
				require.True(t, ok)
				closing := []byte("\r\n--" + boundary + "--\r\n")
				require.True(t, bytes.HasSuffix(body, closing))
				body = append(bytes.TrimSuffix(body, closing), []byte("\r\n--"+boundary+"--"+space+"\r\n")...)
				switch epilogue {
				case "text":
					body = append(body, []byte("hidden bytes")...)
				case "later_delimiter":
					body = append(body, closing...)
				case "extra_part":
					body = append(body, []byte("--"+boundary+"\r\nContent-Disposition: form-data; name=\"extra\"\r\n\r\nhidden bytes")...)
					body = append(body, closing...)
				}
				status, _, raw := h.request(t, "POST", intakePath, ct, body, h.token)
				expectedStatus, expectedRows := http.StatusBadRequest, 0
				if epilogue == "none" {
					expectedStatus, expectedRows = http.StatusOK, 1
				}
				require.Equal(t, expectedStatus, status, raw)
				for _, table := range []string{"archive_sources", "archive_tasks"} {
					var count int
					require.NoError(t, h.db.SQL().QueryRow(`SELECT count(*) FROM `+table).Scan(&count))
					require.Equal(t, expectedRows, count, table)
				}
			})
		}
	}
}

func TestArchiveHTTPMarkerLikeContentIsPreserved(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.ingest")
	body, ct := archiveMultipart(t, validIntakeMetadata, "placeholder", "")
	_, boundary, ok := strings.Cut(ct, "boundary=")
	require.True(t, ok)
	// Bare LF is ordinary content in CRLF multipart mode. Embedded prefixes and
	// a boundary followed by a non-delimiter character must also remain bytes.
	content := "prefix--" + boundary + "-- \r\n\n--" + boundary + "--\t\r\n\r\n--" + boundary + "X\r\nlast"
	body = bytes.Replace(body, []byte("placeholder"), []byte(content), 1)
	status, envelope, raw := h.request(t, "POST", intakePath, ct, body, h.token)
	require.Equal(t, http.StatusOK, status, raw)
	receipt := envelope["data"].(map[string]any)
	require.EqualValues(t, len(content), receipt["size_bytes"])
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(content))), receipt["sha256"])
}
func TestArchiveClarificationRouteRejectsUntypedAnswer(t *testing.T) {
	h := newArchiveHTTP(t, true, "archive.read", "archive.organize")
	status, _, _ := h.request(t, "POST", "/api/v1/integrations/archives/tasks/missing/answer", "application/json", []byte(`{"action":"anything","lease_token":"stolen"}`), h.token)
	require.Equal(t, 400, status)
}

func TestArchiveRevisionRouteStrictAuthorizationAndShape(t *testing.T) {
	for _, tc := range []struct {
		name        string
		permissions []string
		body        string
		want        int
	}{
		{"unknown", []string{"archive.read", "archive.organize"}, `{"document_revision":1,"kind":"private","reason":"x","lease_token":"no"}`, 400},
		{"missing_reason", []string{"archive.read", "archive.organize"}, `{"document_revision":1,"kind":"private"}`, 400},
		{"ingest_only", []string{"archive.ingest"}, `{"document_revision":1,"kind":"private","reason":"x"}`, 403},
		{"missing_document", []string{"archive.read", "archive.organize"}, `{"document_revision":1,"kind":"private","reason":"x"}`, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newArchiveHTTP(t, true, tc.permissions...)
			status, _, raw := h.request(t, "POST", "/api/v1/integrations/archives/documents/missing/revisions", "application/json", []byte(tc.body), h.token)
			require.Equal(t, tc.want, status, raw)
		})
	}
}
