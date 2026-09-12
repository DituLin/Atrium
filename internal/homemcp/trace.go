package homemcp

import (
	"context"
	"regexp"
	"time"

	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oklog/ulid/v2"
)

type callTrace struct{ turn, call string }
type traceContextKey struct{}

var requestRefPattern = regexp.MustCompile(`^req_[0-9a-f]{16}$`)

func withCallTrace(ctx context.Context, meta mcp.Meta) context.Context {
	trace := callTrace{}
	if v, ok := meta[integration.TurnRefMeta].(string); ok && integration.ValidTraceRef(v) {
		trace.turn = v
	}
	if v, ok := meta[integration.CallRefMeta].(string); ok && integration.ValidTraceRef(v) {
		trace.call = v
	}
	if trace.call == "" {
		trace.call = ulid.Make().String()
	}
	return context.WithValue(ctx, traceContextKey{}, trace)
}
func traceFrom(ctx context.Context) callTrace {
	v, _ := ctx.Value(traceContextKey{}).(callTrace)
	return v
}
func (a *adapter) logRequest(ctx context.Context, status int, requestID string, started time.Time) {
	if a.logger == nil {
		return
	}
	trace := traceFrom(ctx)
	fields := []any{"turn_ref", trace.turn, "call_ref", trace.call, "http_status", status, "duration_ms", time.Since(started).Milliseconds()}
	if requestRefPattern.MatchString(requestID) {
		fields = append(fields, "request_id", requestID)
	}
	a.logger.InfoContext(ctx, "core_request", fields...)
}
func (a *adapter) logTool(ctx context.Context, name string, in args, out *mcp.CallToolResult, started time.Time) {
	if a.logger == nil {
		return
	}
	trace := traceFrom(ctx)
	fields := []any{"tool", name, "turn_ref", trace.turn, "call_ref", trace.call, "duration_ms", time.Since(started).Milliseconds()}
	if integration.ValidTraceRef(in.OperationID) {
		fields = append(fields, "operation_id", in.OperationID)
	}
	commandID := in.CommandID
	code := ""
	if out == nil {
		code = "tool_error"
	} else if obj, ok := out.StructuredContent.(map[string]any); ok {
		if data, ok := obj["data"].(map[string]any); ok {
			if name == "home_get_command" || name == "home_get_operation" || name == "home_navigate_screen" || name == "home_show_photo" || name == "home_refresh_screen" {
				if id, ok := data["id"].(string); ok {
					commandID = id
				}
			}
			if out.IsError {
				code = safeTraceError(data["error_code"])
			}
		}
		if detail, ok := obj["error"].(map[string]any); ok {
			code = safeTraceError(detail["code"])
		}
		if out.IsError && code == "" {
			code = "tool_error"
		}
	}
	if integration.ValidTraceRef(commandID) {
		fields = append(fields, "command_id", commandID)
	}
	if code != "" {
		fields = append(fields, "error_code", code)
	}
	a.logger.InfoContext(ctx, "mcp_tool", fields...)
}
func safeTraceError(value any) string {
	code, _ := value.(string)
	switch code {
	case "delivery_failed", "ack_timeout", "server_restart", "session_replaced", "render_failed", "invalid_route", "superseded", "preview_unavailable", "preview_processing", "source_offline", "screen_revoked", "invalid_command", "permission_denied", "screen_offline", "photo_unavailable", "idempotency_conflict", "operation_expired", "not_found", "invalid_arguments", "busy", "response_too_large", "outcome_unknown", "tls_error", "core_unreachable", "invalid_response", "expired", "unknown", "command_expired", "screen_disconnected":
		return code
	default:
		return "tool_error"
	}
}
