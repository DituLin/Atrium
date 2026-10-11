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
	"github.com/DituLin/Atrium/internal/store"
)

const (
	archiveMetadataLimit = 16 << 10
	archiveOverheadLimit = 128 << 10
)

func (a *API) archiveRoutes() {
	if a.deps.Config == nil || !a.deps.Config.Archives.Enabled || a.deps.Archives == nil {
		return
	}
	a.archiveSearchRoutes()
	prefix := APIPrefix + "/integrations/archives"
	timeout := time.Duration(a.deps.Config.Archives.UploadTimeout)
	a.mux.HandleFunc("POST "+prefix+"/intakes", a.archiveBodyDeadline(timeout, a.deps.Config.Archives.MaxBytes+archiveOverheadLimit, a.requireIntegrationTimeout("archive.ingest", timeout, a.archiveIntake)))
	a.mux.HandleFunc("POST "+prefix+"/intakes/lookup", a.archiveBodyDeadline(5*time.Second, archiveMetadataLimit, a.requireIntegration("archive.ingest", a.archiveLookup)))
	a.mux.HandleFunc("POST "+prefix+"/documents/{id}/revisions", a.archiveBodyDeadline(5*time.Second, archiveMetadataLimit, a.requireIntegration("archive.organize", a.archiveRevision)))
	a.mux.HandleFunc("GET "+prefix+"/tasks/{id}", a.requireIntegration("archive.read", a.archiveTask))
	a.mux.HandleFunc("GET "+prefix+"/tasks/{id}/question", a.requireIntegration("archive.read", a.archiveQuestion))
	a.mux.HandleFunc("POST "+prefix+"/tasks/{id}/answer", a.archiveBodyDeadline(5*time.Second, archiveMetadataLimit, a.requireIntegration("archive.organize", a.archiveAnswer)))
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

// multipart.Reader buffers past closing delimiters and permits epilogues. This
// constant-space scanner records the first final delimiter, matching Go's
// isFinalBoundary: --boundary-- followed by space/tab and the selected newline,
// or EOF. Only the first actual opening delimiter selects CRLF versus LF mode;
// arbitrary preamble/content lines never select it. No body lines are buffered.
type archiveEnvelopeReader struct {
	reader             io.Reader
	boundary           []byte // --boundary, without newline or final --
	total, end         int64
	position           int
	state              archiveBoundaryState
	final, eof, bareLF bool
	newline, previous  byte // newline: zero before first delimiter, CR or LF after it
}

type archiveBoundaryState uint8

const (
	archiveBoundaryPrefix archiveBoundaryState = iota
	archiveBoundarySuffix
	archiveBoundaryDash
	archiveBoundarySpace
	archiveBoundaryCR
	archiveBoundaryOther
)

func (e *archiveEnvelopeReader) Read(p []byte) (int, error) {
	n, err := e.reader.Read(p)
	for i, b := range p[:n] {
		if b == '\n' {
			lineEnd := byte('\n')
			if e.state == archiveBoundaryCR {
				lineEnd = '\r'
			}
			complete := e.state == archiveBoundarySuffix || e.state == archiveBoundarySpace || e.state == archiveBoundaryCR
			eligible := e.newline != '\r' || !e.bareLF
			if complete && eligible {
				if e.final {
					selected := e.newline
					if selected == 0 {
						selected = '\r'
					} // Go starts in CRLF mode before any part.
					if e.end == 0 && lineEnd == selected {
						e.end = e.total + int64(i) + 1
					}
				} else if e.newline == 0 {
					e.newline = lineEnd
				}
			}
			e.position, e.state, e.final = 0, archiveBoundaryPrefix, false
			e.bareLF = e.previous != '\r'
		} else {
			switch e.state {
			case archiveBoundaryPrefix:
				if b != e.boundary[e.position] {
					e.state = archiveBoundaryOther
				} else {
					e.position++
					if e.position == len(e.boundary) {
						e.state = archiveBoundarySuffix
					}
				}
			case archiveBoundarySuffix:
				switch b {
				case '-':
					e.state = archiveBoundaryDash
				case ' ', '\t':
					e.state = archiveBoundarySpace
				case '\r':
					e.state = archiveBoundaryCR
				default:
					e.state = archiveBoundaryOther
				}
			case archiveBoundaryDash:
				if b == '-' {
					e.final = true
					e.state = archiveBoundarySpace
				} else {
					e.state = archiveBoundaryOther
				}
			case archiveBoundarySpace:
				switch b {
				case ' ', '\t':
				case '\r':
					e.state = archiveBoundaryCR
				default:
					e.state = archiveBoundaryOther
				}
			case archiveBoundaryCR:
				e.state = archiveBoundaryOther
			}
		}
		e.previous = b
	}
	e.total += int64(n)
	e.eof = err == io.EOF
	return n, err
}
func (e *archiveEnvelopeReader) validEnd() bool {
	if e.end != 0 {
		return e.end == e.total
	}
	return e.eof && e.final && e.state == archiveBoundarySpace && (e.newline != '\r' || !e.bareLF)
}

// Count actual content bytes, not the declared size or configured maximum.
// Receive only calls envelope validation after a successful complete Put.
type archiveContentReader struct {
	io.Reader
	total int64
}

func (r *archiveContentReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.total += int64(n)
	return n, err
}
func (a *API) archiveIntake(w http.ResponseWriter, r *http.Request) {
	fail := func(err error) { WriteError(w, r, archiveRequestError(err)) }
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || len(params["boundary"]) == 0 || len(params["boundary"]) > 70 {
		fail(domain.Errorf(domain.CodeInvalidRequest, "expected multipart/form-data"))
		return
	}
	envelope := &archiveEnvelopeReader{reader: r.Body, boundary: []byte("--" + params["boundary"])}
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
	original := &archiveContentReader{Reader: content}
	receipt, err := a.deps.Archives.Receive(r.Context(), auth.FromContext(r.Context()).IntegrationActor(), household.ID, metadata, original, func() error {
		if _, err := parts.NextRawPart(); !errors.Is(err, io.EOF) {
			return archiveMultipartError(err)
		}
		if _, err := io.Copy(io.Discard, envelope); err != nil {
			return err
		}
		if !envelope.validEnd() {
			return domain.Errorf(domain.CodeInvalidRequest, "invalid archive multipart terminator")
		}
		if envelope.total-original.total > archiveOverheadLimit {
			return domain.Errorf(domain.CodeInvalidRequest, "archive multipart overhead limit exceeded").WithStatus(http.StatusRequestEntityTooLarge)
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

func (a *API) archiveQuestion(w http.ResponseWriter, r *http.Request) {
	home, e := a.deps.DB.LocalHousehold(r.Context())
	if e != nil {
		WriteError(w, r, e)
		return
	}
	q, e := a.deps.DB.ArchiveTasks().ArchiveQuestion(r.Context(), auth.FromContext(r.Context()).IntegrationActor(), home.ID, r.PathValue("id"))
	if e != nil {
		WriteError(w, r, e)
		return
	}
	integrationReply(a, w, q)
}
func (a *API) archiveAnswer(w http.ResponseWriter, r *http.Request) {
	var answer store.ArchiveAnswer
	if e := strictArchiveJSON(r.Body, &answer, []string{"question_id", "question_version", "waiting_task_version", "extraction_id", "document_revision", "resolution_key", "action"}, "target", "name"); e != nil {
		WriteError(w, r, e)
		return
	}
	home, e := a.deps.DB.LocalHousehold(r.Context())
	if e != nil {
		WriteError(w, r, e)
		return
	}
	task, e := a.deps.DB.ArchiveTasks().AnswerArchiveQuestion(r.Context(), auth.FromContext(r.Context()).IntegrationActor(), home.ID, r.PathValue("id"), answer)
	if e != nil {
		WriteError(w, r, e)
		return
	}
	integrationReply(a, w, domain.ProjectArchiveTask(task))
}

func (a *API) archiveRevision(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DocumentRevision int64  `json:"document_revision"`
		Kind             string `json:"kind"`
		ObservationID    string `json:"observation_id"`
		Value            string `json:"value"`
		EntityID         string `json:"entity_id"`
		Reason           string `json:"reason"`
	}
	if e := strictArchiveJSON(r.Body, &input, []string{"document_revision", "kind", "reason"}, "observation_id", "value", "entity_id"); e != nil {
		WriteError(w, r, e)
		return
	}
	home, e := a.deps.DB.LocalHousehold(r.Context())
	if e != nil {
		WriteError(w, r, e)
		return
	}
	revision, e := a.deps.DB.ArchiveTasks().ReviseArchive(r.Context(), auth.FromContext(r.Context()).IntegrationActor(), home.ID, r.PathValue("id"), input.DocumentRevision, store.ArchiveRevision{Kind: input.Kind, ObservationID: input.ObservationID, Value: input.Value, EntityID: input.EntityID, Reason: input.Reason})
	if e != nil {
		WriteError(w, r, e)
		return
	}
	integrationReply(a, w, map[string]int64{"document_revision": revision})
}
