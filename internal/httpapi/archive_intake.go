package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/DituLin/Atrium/internal/archive"
	"github.com/DituLin/Atrium/internal/auth"
	"github.com/DituLin/Atrium/internal/domain"
)

const archiveMetadataLimit = 16 << 10

func (a *API) archiveRoutes() {
	if a.deps.Config == nil || !a.deps.Config.Archives.Enabled || a.deps.Archives == nil {
		return
	}
	prefix := APIPrefix + "/integrations/archives"
	timeout := time.Duration(a.deps.Config.Archives.UploadTimeout)
	a.mux.HandleFunc("POST "+prefix+"/intakes", a.archiveBodyDeadline(timeout, a.deps.Config.Archives.MaxBytes+128*1024, a.requireIntegrationTimeout("archive.ingest", timeout, a.archiveIntake)))
	a.mux.HandleFunc("POST "+prefix+"/intakes/lookup", a.archiveBodyDeadline(5*time.Second, archiveMetadataLimit, a.requireIntegration("archive.ingest", a.archiveLookup)))
	a.mux.HandleFunc("GET "+prefix+"/tasks/{id}", a.requireIntegration("archive.read", a.archiveTask))
}

// SetReadDeadline reaches the actual network body reader through statusWriter.
// Closing a server body can drain it, so retain that deadline until Close returns.
// This also bounds rejected/unauthenticated request bodies and shutdown latency.
func (a *API) archiveBodyDeadline(timeout time.Duration, maxBytes int64, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controller := http.NewResponseController(w)
		if err := controller.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			WriteError(w, r, domain.Errorf(domain.CodeInternal, "archive transport cannot bound body reads"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		defer func() { _ = r.Body.Close(); _ = controller.SetReadDeadline(time.Time{}) }()
		next(w, r)
	}
}
func archiveRequestError(err error) error {
	if _, ok := domain.AsError(err); ok || errors.Is(err, domain.ErrNotFound) {
		return err
	}
	var size *http.MaxBytesError
	var timeout net.Error
	switch {
	case errors.Is(err, archive.ErrTooLarge) || errors.As(err, &size):
		return domain.Errorf(domain.CodeInvalidRequest, "archive byte limit exceeded").WithStatus(http.StatusRequestEntityTooLarge)
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout():
		return domain.Errorf(domain.CodeInvalidRequest, "archive upload timed out").WithStatus(http.StatusRequestTimeout)
	case errors.Is(err, archive.ErrEmpty), errors.Is(err, archive.ErrMismatch), errors.Is(err, io.ErrUnexpectedEOF):
		return domain.Errorf(domain.CodeInvalidRequest, "invalid archive content")
	default:
		return err
	}
}

// strictArchiveJSON rejects duplicate keys, unknown fields, null fields and
// omitted required fields before decoding into typed metadata.
func strictArchiveJSON(reader io.Reader, dst any, required []string, optional ...string) error {
	b, err := io.ReadAll(io.LimitReader(reader, archiveMetadataLimit+1))
	if err != nil {
		return err
	}
	if len(b) > archiveMetadataLimit {
		return domain.Errorf(domain.CodeInvalidRequest, "archive metadata too large")
	}
	b, err = domain.NormalizeArchiveObject(b)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		return domain.Errorf(domain.CodeInvalidRequest, "invalid archive metadata")
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, v := range fields {
		if !allowed[key] {
			return domain.Errorf(domain.CodeInvalidRequest, "unknown archive metadata field")
		}
		if bytes.Equal(v, []byte("null")) {
			return domain.Errorf(domain.CodeInvalidRequest, "null archive metadata field")
		}
		// These optional strings use empty internal values for omission, but an
		// explicitly supplied empty assertion is outside their wire schemas.
		if (key == "expected_sha256" || key == "visibility_intent") && bytes.Equal(v, []byte(`""`)) {
			return domain.Errorf(domain.CodeInvalidRequest, "empty archive assertion")
		}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return domain.Errorf(domain.CodeInvalidRequest, "missing archive metadata field")
		}
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.Errorf(domain.CodeInvalidRequest, "invalid archive metadata fields")
	}
	return nil
}

var archiveSourceFields = []string{"kind", "channel", "account", "conversation", "sender", "provider_message_id", "slot_index"}

func archivePart(part *multipart.Part, name string) error {
	if part.FormName() != name {
		return domain.Errorf(domain.CodeInvalidRequest, "expected archive %s part", name)
	}
	for key, values := range part.Header {
		if len(values) != 1 || key != "Content-Disposition" && key != "Content-Type" {
			return domain.Errorf(domain.CodeInvalidRequest, "invalid archive part headers")
		}
	}
	kind, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil || kind != "form-data" {
		return domain.Errorf(domain.CodeInvalidRequest, "invalid archive part disposition")
	}
	for key := range params {
		if key != "name" && key != "filename" {
			return domain.Errorf(domain.CodeInvalidRequest, "invalid archive part disposition")
		}
	}
	return nil
}

// multipart.Reader buffers beyond the closing delimiter and allows epilogues.
// Track the wire terminator as well, so extra bytes/parts after it cannot be
// hidden in its read-ahead buffer. This API permits no epilogue.
type archiveEnvelopeReader struct {
	reader       io.Reader
	marker, tail []byte
	total, end   int64
}

func (e *archiveEnvelopeReader) Read(p []byte) (int, error) {
	n, err := e.reader.Read(p)
	if n > 0 {
		joined := append(e.tail, p[:n]...)
		if e.end == 0 {
			if at := bytes.Index(joined, e.marker); at >= 0 {
				e.end = e.total - int64(len(e.tail)) + int64(at+len(e.marker))
			}
		}
		e.total += int64(n)
		keep := min(len(joined), len(e.marker))
		e.tail = append([]byte(nil), joined[len(joined)-keep:]...)
	}
	return n, err
}
func (e *archiveEnvelopeReader) validEnd() bool {
	return e.end == e.total || e.end == 0 && bytes.HasSuffix(e.tail, e.marker[:len(e.marker)-2])
}
func (a *API) archiveIntake(w http.ResponseWriter, r *http.Request) {
	fail := func(err error) { WriteError(w, r, archiveRequestError(err)) }
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || len(params["boundary"]) == 0 || len(params["boundary"]) > 70 {
		fail(domain.Errorf(domain.CodeInvalidRequest, "expected multipart/form-data"))
		return
	}
	envelope := &archiveEnvelopeReader{reader: r.Body, marker: []byte("\r\n--" + params["boundary"] + "--\r\n")}
	parts := multipart.NewReader(envelope, params["boundary"])
	metadataPart, err := parts.NextRawPart()
	if err != nil {
		fail(archiveMultipartError(err))
		return
	}
	if err = archivePart(metadataPart, "metadata"); err != nil {
		fail(err)
		return
	}
	var metadata domain.ArchiveIntakeMetadata
	required := append(append([]string{}, archiveSourceFields...), "original_name")
	if err = strictArchiveJSON(metadataPart, &metadata, required, "declared_mime", "expected_size", "expected_sha256", "visibility_intent"); err != nil {
		fail(err)
		return
	}
	if err = metadata.Validate(); err != nil {
		fail(err)
		return
	}
	content, err := parts.NextRawPart()
	if err != nil {
		fail(archiveMultipartError(err))
		return
	}
	if err = archivePart(content, "content"); err != nil {
		fail(err)
		return
	}
	household, err := a.deps.DB.LocalHousehold(r.Context())
	if err != nil {
		fail(err)
		return
	}
	receipt, err := a.deps.Archives.Receive(r.Context(), auth.FromContext(r.Context()).IntegrationActor(), household.ID, metadata, content, func() error {
		if _, err := parts.NextRawPart(); !errors.Is(err, io.EOF) {
			return archiveMultipartError(err)
		}
		if _, err := io.Copy(io.Discard, envelope); err != nil {
			return err
		}
		if !envelope.validEnd() {
			return domain.Errorf(domain.CodeInvalidRequest, "invalid archive multipart terminator")
		}
		return nil
	})
	if err != nil {
		fail(err)
		return
	}
	integrationReply(a, w, receipt)
}
func archiveMultipartError(err error) error {
	var timeout net.Error
	var size *http.MaxBytesError
	if errors.As(err, &timeout) && timeout.Timeout() || errors.As(err, &size) {
		return err
	}
	return domain.Errorf(domain.CodeInvalidRequest, "invalid archive multipart envelope")
}
func (a *API) archiveLookup(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		WriteError(w, r, domain.Errorf(domain.CodeInvalidRequest, "expected application/json"))
		return
	}
	var source domain.ArchiveSourceIdentity
	if err := strictArchiveJSON(r.Body, &source, archiveSourceFields); err != nil {
		WriteError(w, r, archiveRequestError(err))
		return
	}
	household, err := a.deps.DB.LocalHousehold(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	receipt, err := a.deps.Archives.Lookup(r.Context(), auth.FromContext(r.Context()).IntegrationActor(), household.ID, source)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	integrationReply(a, w, receipt)
}
func (a *API) archiveTask(w http.ResponseWriter, r *http.Request) {
	household, err := a.deps.DB.LocalHousehold(r.Context())
	if err != nil {
		WriteError(w, r, err)
		return
	}
	task, err := a.deps.Archives.Task(r.Context(), auth.FromContext(r.Context()).IntegrationActor(), household.ID, strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		WriteError(w, r, err)
		return
	}
	integrationReply(a, w, task)
}
