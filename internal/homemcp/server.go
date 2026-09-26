package homemcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/DituLin/Atrium/internal/integration"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxOutput = 32 * 1024

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

type adapter struct {
	logger        *slog.Logger
	client        *http.Client
	origin, token string
	slots         chan struct{}
	mu            sync.Mutex
	busy          map[string]bool
}
type args struct {
	ScreenID    string `json:"screen_id"`
	PhotoID     string `json:"photo_id"`
	CommandID   string `json:"command_id"`
	OperationID string `json:"operation_id"`
	Route       string `json:"route"`
	Collection  string `json:"collection"`
	Cursor      string `json:"cursor"`
	Limit       int    `json:"limit"`
	WaitMS      *int   `json:"wait_ms"`
}
type toolDef struct {
	name     string
	required []string
	optional []string
	mutation bool
}

var definitions = []toolDef{
	{"home_get_status", nil, nil, false}, {"home_list_screens", nil, nil, false}, {"home_get_screen", []string{"screen_id"}, nil, false}, {"home_get_nas_status", nil, nil, false}, {"home_list_photos", nil, []string{"collection", "limit", "cursor"}, false}, {"home_get_photo", []string{"photo_id"}, nil, false},
	{"home_navigate_screen", []string{"screen_id", "route", "operation_id"}, []string{"collection", "wait_ms"}, true}, {"home_show_photo", []string{"screen_id", "photo_id", "operation_id"}, []string{"wait_ms"}, true}, {"home_refresh_screen", []string{"screen_id", "operation_id"}, []string{"wait_ms"}, true}, {"home_get_command", []string{"command_id"}, nil, false}, {"home_get_operation", []string{"operation_id"}, nil, false},
}

func field(name string) map[string]any {
	switch name {
	case "route":
		return map[string]any{"type": "string", "enum": []string{"dashboard", "photos"}}
	case "collection":
		return map[string]any{"type": "string", "enum": []string{"recent", "captured_today", "random", "all"}}
	case "operation_id":
		return map[string]any{"type": "string", "pattern": "^[0-7][0-9A-HJKMNP-TV-Z]{25}$", "minLength": 26, "maxLength": 26}
	case "wait_ms":
		return map[string]any{"type": "integer", "minimum": 0, "maximum": 5000, "default": 3000}
	case "limit":
		return map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "default": 20}
	case "cursor":
		return map[string]any{"type": "string", "maxLength": 2048}
	default:
		return map[string]any{"type": "string", "pattern": safeID.String(), "maxLength": 128, "minLength": 1}
	}
}

// NewServer builds the eleven-tool MCP server without opening a listener.
func NewServer(c Config) (*mcp.Server, error) {
	return NewServerWithLogger(c, slog.New(slog.NewJSONHandler(os.Stderr, nil)))
}

// NewServerWithLogger builds the server with structured correlation logging.
// A nil logger uses the same stderr JSON logger as NewServer.
func NewServerWithLogger(c Config, logger *slog.Logger) (*mcp.Server, error) {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	client, token, e := c.client()
	if e != nil {
		return nil, e
	}
	a := &adapter{logger: logger, client: client, token: token, origin: strings.TrimRight(c.CoreURL, "/"), slots: make(chan struct{}, 4), busy: map[string]bool{}}
	s := mcp.NewServer(&mcp.Implementation{Name: "atrium-home-mcp", Version: "1.0.0"}, nil)
	no := false
	for _, d := range definitions {
		props := map[string]any{}
		for _, k := range append(append([]string{}, d.required...), d.optional...) {
			props[k] = field(k)
		}
		req := d.required
		if req == nil {
			req = []string{}
		}
		schema := map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}
		if d.name == "home_navigate_screen" {
			schema["allOf"] = []any{map[string]any{"if": map[string]any{"properties": map[string]any{"route": map[string]any{"const": "photos"}}, "required": []string{"route"}}, "then": map[string]any{"required": []string{"collection"}}}}
		}
		b, _ := json.Marshal(schema)
		var raw jsonschema.Schema
		if e = json.Unmarshal(b, &raw); e != nil {
			return nil, e
		}
		resolved, e := raw.Resolve(nil)
		if e != nil {
			return nil, e
		}
		s.AddTool(&mcp.Tool{Name: d.name, Description: description(d), InputSchema: schema, OutputSchema: outputSchema(), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: !d.mutation, DestructiveHint: &no, OpenWorldHint: &no, IdempotentHint: true}}, func(ctx context.Context, r *mcp.CallToolRequest) (out *mcp.CallToolResult, err error) {
			ctx = withCallTrace(ctx, r.Params.Meta)
			started := time.Now()
			var in args
			defer func() { a.logTool(ctx, d.name, in, out, started) }()
			var obj any
			if json.Unmarshal(r.Params.Arguments, &obj) != nil || resolved.Validate(obj) != nil {
				return failure("invalid_arguments", "Arguments do not match the tool schema.", ""), nil
			}
			if json.Unmarshal(r.Params.Arguments, &in) != nil {
				return failure("invalid_arguments", "Invalid arguments.", ""), nil
			}
			return a.call(ctx, d, in), nil
		})
	}
	return s, nil
}
func description(d toolDef) string {
	if d.mutation {
		return "Control an authorized screen. Reuse the same operation_id for the same action. accepted means pending; only applied confirms completion. Cancellation does not cancel an accepted action."
	}
	return "Read current authorized household metadata. Availability and observation time describe freshness; no filesystem paths or media URLs are returned."
}
func outputSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"schema_version", "observed_at", "availability", "data"}, "additionalProperties": false, "properties": map[string]any{"schema_version": map[string]any{"type": "string", "const": "1"}, "observed_at": map[string]any{"type": "string", "format": "date-time"}, "availability": map[string]any{"type": "string"}, "data": map[string]any{"type": []string{"object", "array", "null"}}, "error": map[string]any{"type": "object", "required": []string{"code", "message"}, "additionalProperties": false, "properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}, "operation_id": map[string]any{"type": "string"}}}}}
}
func result(v any, bad bool) *mcp.CallToolResult {
	b, e := json.Marshal(v)
	if e != nil || len(b) > maxOutput {
		return failure("response_too_large", "Response exceeds the bounded output limit; request a smaller page.", "")
	}
	var obj any
	_ = json.Unmarshal(b, &obj)
	b, _ = json.Marshal(obj)
	r := &mcp.CallToolResult{StructuredContent: obj, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, IsError: bad}
	full, err := json.Marshal(r)
	// Reserve space for SDK-added server metadata and resultType.
	if err != nil || len(full) > maxOutput-512 {
		return failure("response_too_large", "Response exceeds the bounded output limit; request a smaller page.", "")
	}
	return r
}
func failure(code, message, op string) *mcp.CallToolResult {
	errObj := map[string]any{"code": code, "message": message}
	if op != "" {
		errObj["operation_id"] = op
	}
	return result(map[string]any{"schema_version": "1", "observed_at": time.Now().UTC(), "availability": "unknown", "data": nil, "error": errObj}, true)
}
func (a *adapter) call(ctx context.Context, d toolDef, in args) *mcp.CallToolResult {
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		return failure("busy", "Too many concurrent tool calls; retry later.", in.OperationID)
	}
	if d.mutation {
		a.mu.Lock()
		busy := a.busy[in.ScreenID]
		if !busy {
			a.busy[in.ScreenID] = true
		}
		a.mu.Unlock()
		if busy {
			return failure("busy", "A screen action is already in progress; query its operation before retrying.", in.OperationID)
		}
		defer func() { a.mu.Lock(); delete(a.busy, in.ScreenID); a.mu.Unlock() }()
		return a.control(ctx, d, in)
	}
	path := ""
	switch d.name {
	case "home_get_status":
		path = "home"
	case "home_list_screens":
		path = "screens"
	case "home_get_screen":
		path = "screens/" + in.ScreenID
	case "home_get_nas_status":
		path = "nas/status"
	case "home_get_photo":
		path = "photos/" + in.PhotoID
	case "home_get_command":
		path = "commands/" + in.CommandID
	case "home_get_operation":
		path = "operations/" + in.OperationID
	case "home_list_photos":
		if in.Limit == 0 {
			in.Limit = 20
		}
		if in.Collection == "" {
			in.Collection = "recent"
		}
		q := url.Values{"collection": {in.Collection}, "limit": {jsonNumber(in.Limit)}}
		if in.Cursor != "" {
			q.Set("cursor", in.Cursor)
		}
		path = "photos?" + q.Encode()
	}
	b, status, e := a.request(ctx, "GET", path, nil)
	if e != nil {
		return transportFailure(e, "")
	}
	return decode(d.name, b, status)
}
func jsonNumber(i int) string { b, _ := json.Marshal(i); return string(b) }
func (a *adapter) request(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	started := time.Now()
	status := 0
	requestID := ""
	defer func() { a.logRequest(ctx, status, requestID, started) }()
	var rd io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return nil, 0, e
		}
		rd = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, a.origin+"/api/v1/integrations/"+path, rd)
	if e != nil {
		return nil, 0, e
	}
	trace := traceFrom(ctx)
	if trace.turn != "" {
		req.Header.Set(integration.TurnRefHeader, trace.turn)
	}
	if trace.call != "" {
		req.Header.Set(integration.CallRefHeader, trace.call)
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := a.client.Do(req)
	if e != nil {
		return nil, 0, e
	}
	status = resp.StatusCode
	requestID = resp.Header.Get("X-Request-Id")
	defer func() { _ = resp.Body.Close() }()
	b, e := io.ReadAll(io.LimitReader(resp.Body, maxOutput+1))
	if len(b) > maxOutput {
		return nil, resp.StatusCode, errLarge
	}
	return b, resp.StatusCode, e
}

var errLarge = errors.New("large response")

func transportFailure(e error, op string) *mcp.CallToolResult {
	if op != "" {
		return failure("outcome_unknown", "Action outcome is unconfirmed. Query this operation_id; do not create a replacement action.", op)
	}
	if errors.Is(e, errLarge) {
		return failure("response_too_large", "Request a smaller result page.", "")
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(e, &certErr) {
		return failure("tls_error", "Household CA or Core certificate verification failed.", "")
	}
	return failure("core_unreachable", "Core could not be reached securely; check its connection and household CA.", "")
}
func decode(name string, b []byte, status int) *mcp.CallToolResult {
	switch name {
	case "home_get_status":
		return typed[integration.Home](b, status)
	case "home_list_screens":
		return typed[[]integration.Screen](b, status)
	case "home_get_screen":
		return typed[integration.Screen](b, status)
	case "home_get_nas_status":
		return typed[[]integration.Source](b, status)
	case "home_list_photos":
		return typed[integration.PhotoList](b, status)
	case "home_get_photo":
		return typed[integration.Photo](b, status)
	default:
		return typed[integration.Command](b, status)
	}
}
func typed[T any](b []byte, status int) *mcp.CallToolResult {
	var v integration.Response[T]
	if json.Unmarshal(b, &v) != nil || v.SchemaVersion != "1" || v.ObservedAt.IsZero() || v.Availability == "" {
		return backendError(b, status)
	}
	if status < 200 || status >= 300 {
		if c, ok := any(v.Data).(integration.Command); !ok || c.Status != "failed" {
			return backendError(b, status)
		}
	}
	bad := false
	if c, ok := any(v.Data).(integration.Command); ok {
		switch c.Status {
		case "accepted", "applied":
		case "failed", "expired", "unknown":
			bad = true
		default:
			return failure("invalid_response", "Core returned an invalid command state.", "")
		}
	}
	return result(v, bad)
}
func backendError(b []byte, status int) *mcp.CallToolResult {
	var v struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(b, &v)
	messages := map[string]string{"permission_denied": "This service identity is not permitted to access the requested resource.", "screen_offline": "The screen session is offline.", "photo_unavailable": "The photo is not available.", "idempotency_conflict": "The operation_id is already bound to different action parameters.", "operation_expired": "The operation is outside the supported retry window.", "not_found": "The authorized resource was not found.", "invalid_arguments": "Core rejected the action arguments."}
	if msg, ok := messages[v.Error.Code]; ok {
		return failure(v.Error.Code, msg, "")
	}
	if status == 401 || status == 403 {
		return failure("permission_denied", "Core rejected the service identity or its permissions.", "")
	}
	return failure("invalid_response", "Core returned an unexpected response.", "")
}
func (a *adapter) control(ctx context.Context, d toolDef, in args) *mcp.CallToolResult {
	kind := strings.TrimSuffix(strings.TrimPrefix(d.name, "home_"), "_screen")
	if d.name == "home_show_photo" {
		kind = "show"
	}
	payload := integration.CommandPayload{Route: in.Route, Collection: in.Collection, PhotoID: in.PhotoID}
	if in.Route == "photos" && in.Collection == "" {
		return failure("invalid_arguments", "A photos action requires an explicit collection.", in.OperationID)
	}
	if in.Route == "dashboard" && in.Collection != "" {
		return failure("invalid_arguments", "A dashboard action does not accept a collection.", in.OperationID)
	}
	b, status, e := a.request(ctx, "POST", "screens/"+in.ScreenID+"/commands", map[string]any{"operation_id": in.OperationID, "kind": kind, "payload": payload})
	if e != nil {
		return transportFailure(e, in.OperationID)
	}
	r := decode("command", b, status)
	if obj, ok := r.StructuredContent.(map[string]any); ok && obj["error"] == nil {
		data, valid := obj["data"].(map[string]any)
		id, validID := data["id"].(string)
		if !valid || !validID || !safeID.MatchString(id) {
			return transportFailure(errors.New("invalid command identifier"), in.OperationID)
		}
	}
	if r.IsError {
		if obj, ok := r.StructuredContent.(map[string]any); ok {
			if detail, ok := obj["error"].(map[string]any); ok {
				if detail["code"] == "invalid_response" {
					return transportFailure(errors.New("unconfirmed response"), in.OperationID)
				}
				detail["operation_id"] = in.OperationID
				return result(obj, true)
			}
		}
		return r
	}
	var envelope integration.Response[integration.Command]
	if json.Unmarshal(b, &envelope) != nil {
		return failure("outcome_unknown", "Query the operation to confirm its result.", in.OperationID)
	}
	wait := 3000
	if in.WaitMS != nil {
		wait = *in.WaitMS
	}
	if !safeID.MatchString(envelope.Data.ID) {
		return failure("outcome_unknown", "Core did not return a valid command identifier; query the operation.", in.OperationID)
	}
	commandID := envelope.Data.ID
	if envelope.Data.Status != "accepted" || wait == 0 {
		return r
	}
	pollCtx, cancel := context.WithTimeout(ctx, time.Duration(wait)*time.Millisecond)
	defer cancel()
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-pollCtx.Done():
			return r
		case <-timer.C:
			b, status, e = a.request(pollCtx, "GET", "commands/"+commandID, nil)
			if e != nil {
				if pollCtx.Err() != nil {
					return r
				}
				return unknownCommand(in.OperationID, commandID)
			}
			next := decode("command", b, status)
			if next.IsError {
				// Only a decoded command result is evidence of a terminal state.
				// Error envelopes, including malformed command-shaped responses,
				// cannot confirm the action's outcome.
				obj, ok := next.StructuredContent.(map[string]any)
				if ok && obj["error"] == nil {
					data, valid := obj["data"].(map[string]any)
					if valid && data["id"] == commandID && (data["status"] == "failed" || data["status"] == "expired" || data["status"] == "unknown") {
						return next
					}
				}
				return unknownCommand(in.OperationID, commandID)
			}
			if json.Unmarshal(b, &envelope) != nil || envelope.Data.ID != commandID {
				return unknownCommand(in.OperationID, commandID)
			}
			r = next
			if envelope.Data.Status != "accepted" {
				return r
			}
		}
	}
}

func unknownCommand(operationID, commandID string) *mcp.CallToolResult {
	r := failure("outcome_unknown", "The accepted action could not be confirmed. Query the original operation or command; do not create a replacement.", operationID)
	obj, ok := r.StructuredContent.(map[string]any)
	if !ok {
		return r
	}
	obj["data"] = map[string]any{"id": commandID, "status": "unknown"}
	return result(obj, true)
}
