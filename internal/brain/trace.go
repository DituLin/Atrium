package brain

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oklog/ulid/v2"
)

// NewExecutorWithLogger uses the host's log sink. Nil selects JSON stderr; no
// prompts, arguments, response bodies, credentials or underlying errors are logged.
func NewExecutorWithLogger(client ToolCaller, ledger *Ledger, logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &Executor{client: client, ledger: ledger, screens: map[string]*screenLock{}, logger: logger}
}
func (e *Executor) invoke(ctx context.Context, turnRef, name string, args map[string]any) (result *mcp.CallToolResult, err error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return nil, errors.New("cannot create call correlation ID")
	}
	callRef := id.String()
	meta := mcp.Meta{integration.CallRefMeta: callRef}
	attrs := []any{"event", "brain.tool", "tool", name, "call_ref", callRef}
	if integration.ValidTraceRef(turnRef) {
		meta[integration.TurnRefMeta] = turnRef
		attrs = append(attrs, "turn_ref", turnRef)
	}
	if op, ok := args["operation_id"].(string); ok && integration.ValidTraceRef(op) {
		attrs = append(attrs, "operation_id", op)
	}
	started := time.Now()
	defer func() {
		code := ""
		if err != nil {
			code = "transport_error"
		} else if result != nil && result.IsError {
			code = "tool_error"
		}
		attrs = append(attrs, "duration_ms", time.Since(started).Milliseconds(), "error_code", code)
		e.logger.InfoContext(ctx, "Brain tool completed", attrs...)
	}()
	return e.client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args, Meta: meta})
}
