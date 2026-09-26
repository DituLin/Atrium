package brain

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oklog/ulid/v2"
)

// ErrToolBudget means a host turn has spent its eight actual MCP calls.
var ErrToolBudget = errors.New("turn has reached the eight-tool-call budget")

// ErrInvalidOutcome means a returned action result cannot be trusted as Core evidence.
var ErrInvalidOutcome = errors.New("action outcome is unconfirmed; query the original operation")

// ActionOutcomeError identifies the durable action whose outcome could not be
// confirmed. Cause remains available to host code but is omitted from Error.
type ActionOutcomeError struct {
	OperationID string
	CommandID   string
	Cause       error
}

func (e *ActionOutcomeError) Error() string {
	message := "action outcome is unconfirmed"
	if identifier.MatchString(e.OperationID) {
		message += "; operation_id=" + e.OperationID
	}
	if identifier.MatchString(e.CommandID) {
		message += "; command_id=" + e.CommandID
	}
	return message
}

// Unwrap supports errors.Is without disclosing provider or database details.
func (e *ActionOutcomeError) Unwrap() error { return e.Cause }

func actionError(record ActionRecord, cause error) error {
	if cause == nil {
		return nil
	}
	var existing *ActionOutcomeError
	if errors.As(cause, &existing) {
		return cause
	}
	return &ActionOutcomeError{OperationID: record.OperationID, CommandID: record.CommandID, Cause: cause}
}

// ToolCaller is implemented by the official MCP ClientSession.
type ToolCaller interface {
	CallTool(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

// Executor enforces host boundaries independently of any model runtime.
type Executor struct {
	client  ToolCaller
	logger  *slog.Logger
	ledger  *Ledger
	mu      sync.Mutex
	screens map[string]*screenLock
}

// NewExecutor binds a trusted MCP session to an independent durable ledger.
func NewExecutor(client ToolCaller, ledger *Ledger) *Executor {
	return NewExecutorWithLogger(client, ledger, nil)
}

// Turn owns a host-created identity, deadline and network-call budget.
type Turn struct {
	executor *Executor
	id       string
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	calls    int
}

// StartTurn gives a model turn at most thirty seconds, preserving earlier deadlines.
func (e *Executor) StartTurn(ctx context.Context) *Turn {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		cancel()
	}
	return &Turn{executor: e, id: id.String(), ctx: ctx, cancel: cancel}
}

// Close cancels local work; it never claims to cancel an accepted TV command.
func (t *Turn) Close() { t.cancel() }

type screenLock struct {
	slot chan struct{}
	refs int
}

func (e *Executor) lockScreen(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	lock := e.screens[id]
	if lock == nil {
		lock = &screenLock{slot: make(chan struct{}, 1)}
		e.screens[id] = lock
	}
	lock.refs++
	e.mu.Unlock()
	drop := func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		lock.refs--
		if lock.refs == 0 {
			delete(e.screens, id)
		}
	}
	select {
	case lock.slot <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-lock.slot
			drop()
			return nil, err
		}
		var once sync.Once
		return func() { once.Do(func() { <-lock.slot; drop() }) }, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}
func (t *Turn) reserve() (func(bool), error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	if t.calls >= 8 {
		return nil, ErrToolBudget
	}
	t.calls++
	return func(sent bool) {
		if !sent {
			t.mu.Lock()
			t.calls--
			t.mu.Unlock()
		}
	}, nil
}

// Call accepts model arguments but never accepts model-chosen operation or turn IDs.
// Repeating a call reference queries the original action, even after transport failure.
// Model runtimes must expose host schemas with operation_id and wait_ms removed,
// and must hide home_get_operation (host recovery only). Raw MCP input schemas
// cannot be forwarded to a model: their required operation_id is host-owned.
func (t *Turn) Call(callRef, name string, input map[string]any) (*mcp.CallToolResult, error) {
	if !identifier.MatchString(callRef) {
		return nil, errors.New("invalid call reference")
	}
	args, action, err := validateArguments(name, input)
	if err != nil {
		return nil, err
	}
	if err = t.ctx.Err(); err != nil {
		return nil, err
	}
	if action == nil {
		finish, err := t.reserve()
		if err != nil {
			return nil, err
		}
		sent := false
		defer func() { finish(sent) }()
		if err = t.ctx.Err(); err != nil {
			return nil, err
		}
		sent = true
		return t.executor.invoke(t.ctx, t.id, name, args)
	}
	release, err := t.executor.lockScreen(t.ctx, action.ScreenID)
	if err != nil {
		return nil, err
	}
	defer release()
	// Reserve before durable intent so a budget-exhausted call creates no action.
	finish, err := t.reserve()
	if err != nil {
		return nil, err
	}
	sent := false
	defer func() { finish(sent) }()
	record, send, err := t.executor.ledger.BeginAction(t.ctx, t.id, callRef, *action)
	if err != nil {
		return nil, err
	}
	if send {
		args["operation_id"] = record.OperationID
		args["wait_ms"] = 0
	} else {
		name = "home_get_operation"
		args = map[string]any{"operation_id": record.OperationID}
	}
	if err = t.ctx.Err(); err != nil {
		return nil, actionError(record, err)
	}
	sent = true
	result, err := t.executor.invoke(t.ctx, t.id, name, args)
	if err != nil {
		return nil, actionError(record, err)
	}
	if err = t.executor.observe(t.ctx, record, result); err != nil {
		return nil, actionError(record, err)
	}
	return result, nil
}

// Recover queries one bounded ledger page, never resending an action. Use
// RecoverPage's returned cursor to continue past unresolved entries.
func (e *Executor) Recover(ctx context.Context) ([]*mcp.CallToolResult, error) {
	results, _, err := e.RecoverPage(ctx, "")
	return results, err
}

// RecoverPage queries at most 100 pending records within a thirty-second deadline.
// The cursor remains usable when early records stay unresolved indefinitely.
func (e *Executor) RecoverPage(ctx context.Context, after string) ([]*mcp.CallToolResult, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	records, next, err := e.ledger.PendingPage(ctx, after)
	if err != nil {
		return nil, after, err
	}
	results := make([]*mcp.CallToolResult, 0, len(records))
	cursor := after
	for _, record := range records {
		release, err := e.lockScreen(ctx, record.Action.ScreenID)
		if err != nil {
			return results, cursor, actionError(record, err)
		}
		result, callErr := e.invoke(ctx, record.TurnID, "home_get_operation", map[string]any{"operation_id": record.OperationID})
		if callErr == nil {
			callErr = e.observe(ctx, record, result)
		}
		release()
		if callErr != nil {
			return results, cursor, actionError(record, callErr)
		}
		results = append(results, result)
		cursor = record.OperationID
	}
	return results, next, nil
}

func (e *Executor) observe(ctx context.Context, record ActionRecord, result *mcp.CallToolResult) (err error) {
	defer func() { err = actionError(record, err) }()
	evidence, err := decodeActionEvidence(record, result)
	if err != nil {
		return err
	}
	if evidence.Command == nil {
		return nil
	}
	record.CommandID = evidence.Command.ID
	return e.ledger.Observe(ctx, record.OperationID, evidence.Command.ID, evidence.Command.Status)
}

var modelArgumentFields = map[string][]string{"home_get_status": {}, "home_list_screens": {}, "home_get_screen": {"screen_id"}, "home_get_nas_status": {}, "home_list_photos": {"collection", "limit", "cursor"}, "home_get_photo": {"photo_id"}, "home_get_command": {"command_id"}, "home_get_operation": {}, "home_refresh_screen": {"screen_id"}, "home_show_photo": {"screen_id", "photo_id"}, "home_navigate_screen": {"screen_id", "route", "collection"}}

func validateArguments(name string, input map[string]any) (map[string]any, *Action, error) {

	fields, ok := modelArgumentFields[name]
	if !ok || name == "home_get_operation" {
		return nil, nil, errors.New("tool is not available to model calls")
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		found := false
		for _, field := range fields {
			if key == field {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, errors.New("unexpected tool argument")
		}
		out[key] = value
	}
	stringArg := func(key string, required bool) (string, error) {
		v, exists := out[key]
		if !exists && !required {
			return "", nil
		}
		s, ok := v.(string)
		if !ok {
			return "", errors.New("invalid string argument")
		}
		return s, nil
	}
	for _, key := range []string{"screen_id", "photo_id", "command_id"} {
		if v, exists := out[key]; exists {
			value, ok := v.(string)
			if !ok || !identifier.MatchString(value) {
				return nil, nil, errors.New("invalid resource ID")
			}
		}
	}
	switch name {
	case "home_get_screen", "home_get_photo", "home_get_command":
		if len(out) != 1 {
			return nil, nil, errors.New("missing resource ID")
		}
	}
	if name == "home_list_photos" {
		if v, exists := out["limit"]; exists {
			b, _ := json.Marshal(v)
			var n int
			if json.Unmarshal(b, &n) != nil || n < 1 || n > 50 {
				return nil, nil, errors.New("invalid page limit")
			}
			out["limit"] = n
		}
		if c, exists := out["cursor"]; exists {
			s, ok := c.(string)
			if !ok || len(s) > 2048 {
				return nil, nil, errors.New("invalid cursor")
			}
		}
	}
	if _, exists := out["collection"]; exists {
		c, err := stringArg("collection", true)
		if err != nil {
			return nil, nil, err
		}
		switch c {
		case "recent", "captured_today", "random", "all":
		default:
			return nil, nil, errors.New("invalid collection")
		}
	}
	switch name {
	case "home_refresh_screen", "home_show_photo", "home_navigate_screen":
		a := &Action{Tool: name}
		var err error
		a.ScreenID, err = stringArg("screen_id", true)
		if err != nil {
			return nil, nil, err
		}
		a.Route, err = stringArg("route", false)
		if err != nil {
			return nil, nil, err
		}
		a.Collection, err = stringArg("collection", false)
		if err != nil {
			return nil, nil, err
		}
		a.PhotoID, err = stringArg("photo_id", false)
		if err != nil {
			return nil, nil, err
		}
		if err = a.validate(); err != nil {
			return nil, nil, err
		}
		return out, a, nil
	}
	return out, nil, nil
}
