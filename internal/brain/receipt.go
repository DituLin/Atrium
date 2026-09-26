package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrReceipt means private execution evidence could not be persisted safely.
var ErrReceipt = errors.New("private execution evidence unavailable")
var invocationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Receipt is an append-only, bounded invocation evidence sink. A lease holds
// an advisory file lock from admission through fsync, including the tool call.
// This serializes receipt-enabled calls without changing ordinary host turns.
type Receipt struct {
	file       *os.File
	invocation string
	gate       chan struct{}
}

// OpenReceipt opens an existing operator-created private file, never a model path.
func OpenReceipt(path, invocation string) (*Receipt, error) {
	if !filepath.IsAbs(path) || !invocationPattern.MatchString(invocation) {
		return nil, ErrReceipt
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !privateOwned(parent, 0700, true) {
		return nil, ErrReceipt
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_APPEND|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrReceipt
	}
	file := os.NewFile(uintptr(fd), "private-receipt")
	info, err := file.Stat()
	if err != nil || !privateOwned(info, 0600, false) {
		_ = file.Close()
		return nil, ErrReceipt
	}
	return &Receipt{file: file, invocation: invocation, gate: make(chan struct{}, 1)}, nil
}
func privateOwned(info os.FileInfo, mode os.FileMode, directory bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == int64(os.Getuid()) && info.Mode().Perm() == mode && ((directory && info.IsDir()) || (!directory && info.Mode().IsRegular() && stat.Nlink == 1))
}

// Close releases the private descriptor after all runtime work has stopped.
func (r *Receipt) Close() error { return r.file.Close() }

type receiptError struct {
	Code        string `json:"code"`
	OperationID string `json:"operation_id,omitempty"`
	CommandID   string `json:"command_id,omitempty"`
}
type receiptToolResult struct {
	StructuredContent any  `json:"structuredContent"`
	IsError           bool `json:"isError,omitempty"`
}
type receiptRecord struct {
	Schema     int                `json:"schema_version"`
	Invocation string             `json:"invocation_id"`
	Run        string             `json:"run_id"`
	Tool       string             `json:"tool"`
	Arguments  map[string]any     `json:"arguments"`
	Result     *receiptToolResult `json:"tool_result"`
	Report     *ActionReport      `json:"report,omitempty"`
	Error      *receiptError      `json:"error,omitempty"`
}

func safeReceiptError(err error) *receiptError {
	if err == nil {
		return nil
	}
	out := &receiptError{Code: "request_failed"}
	switch {
	case errors.Is(err, ErrReceipt):
		out.Code = "receipt_unavailable"
	case errors.Is(err, ErrToolBudget):
		out.Code = "tool_budget"
	case errors.Is(err, ErrActionBudget):
		out.Code = "action_budget"
	case errors.Is(err, ErrRunClosed):
		out.Code = "run_closed"
	case errors.Is(err, ErrInvalidOutcome), errors.Is(err, ErrCommandConflict):
		out.Code = "outcome_unconfirmed"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		out.Code = "cancelled"
	}
	var action *ActionOutcomeError
	if errors.As(err, &action) {
		if integration.ValidTraceRef(action.OperationID) {
			out.OperationID = action.OperationID
		}
		if identifier.MatchString(action.CommandID) {
			out.CommandID = action.CommandID
		}
	}
	return out
}
func (r *Receipt) acquire(ctx context.Context) (func(), error) {
	select {
	case r.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ErrReceipt
	}
	releaseGate := func() { <-r.gate }
	for {
		if ctx.Err() != nil {
			releaseGate()
			return nil, ErrReceipt
		}
		err := syscall.Flock(int(r.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			releaseGate()
			return nil, ErrReceipt
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			releaseGate()
			return nil, ErrReceipt
		case <-timer.C:
		}
	}
	release := func() { _ = syscall.Flock(int(r.file.Fd()), syscall.LOCK_UN); releaseGate() }
	info, err := r.file.Stat()
	if err != nil || !privateOwned(info, 0600, false) || info.Size() > 512*1024 {
		release()
		return nil, ErrReceipt
	}
	raw, err := io.ReadAll(io.NewSectionReader(r.file, 0, 512*1024+1))
	if err != nil || len(raw) > 512*1024 {
		release()
		return nil, ErrReceipt
	}
	lines := bytes.Split(raw, []byte{'\n'})
	if len(raw) > 0 && len(lines[len(lines)-1]) != 0 {
		release()
		return nil, ErrReceipt
	}
	count := 0
	for _, line := range lines[:len(lines)-1] {
		var previous receiptRecord
		if len(line) == 0 || len(line) > 64*1024 || json.Unmarshal(line, &previous) != nil || previous.Schema != 1 || previous.Invocation != r.invocation {
			release()
			return nil, ErrReceipt
		}
		count++
	}
	if count >= 8 {
		release()
		return nil, ErrReceipt
	}
	return release, nil
}
func (r *Receipt) append(record receiptRecord) error {
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > 64*1024 {
		return ErrReceipt
	}
	raw = append(raw, '\n')
	info, statErr := r.file.Stat()
	if statErr != nil || !privateOwned(info, 0600, false) || info.Size()+int64(len(raw)) > 512*1024 {
		return ErrReceipt
	}
	n, err := r.file.Write(raw)
	if err != nil || n != len(raw) {
		return ErrReceipt
	}
	if r.file.Sync() != nil {
		return ErrReceipt
	}
	return nil
}

// receiptResult validates the structured contract and drops duplicate text and
// SDK metadata. Neither model prose nor arbitrary MCP content becomes evidence.
func receiptResult(tool string, args map[string]any, result *mcp.CallToolResult) (*receiptToolResult, error) {
	if result == nil || result.StructuredContent == nil {
		return nil, ErrInvalidOutcome
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil || len(raw) > 32*1024 {
		return nil, ErrInvalidOutcome
	}
	var envelope struct {
		Schema       string          `json:"schema_version"`
		Observed     time.Time       `json:"observed_at"`
		Availability string          `json:"availability"`
		Data         json.RawMessage `json:"data"`
		Error        json.RawMessage `json:"error,omitempty"`
	}
	if strictReceiptJSON(raw, &envelope) != nil || envelope.Schema != "1" || envelope.Observed.IsZero() || len(envelope.Data) == 0 {
		return nil, ErrInvalidOutcome
	}
	if len(envelope.Error) > 0 {
		var detail struct {
			Code        string `json:"code"`
			Message     string `json:"message"`
			OperationID string `json:"operation_id,omitempty"`
		}
		if !result.IsError || envelope.Availability != "unknown" || strictReceiptJSON(envelope.Error, &detail) != nil || detail.Code == "" {
			return nil, ErrInvalidOutcome
		}
		// Error envelopes are observations of failure/uncertainty, never success.
		safe := map[string]any{"schema_version": "1", "observed_at": envelope.Observed, "availability": "unknown", "data": nil, "error": map[string]any{"code": receiptBusinessCode(detail.Code), "message": "Tool outcome is not confirmed."}}
		return &receiptToolResult{IsError: true, StructuredContent: safe}, nil
	}
	if envelope.Availability != "available" || string(envelope.Data) == "null" {
		return nil, ErrInvalidOutcome
	}
	var data any
	switch tool {
	case "home_get_status":
		data = &integration.Home{}
	case "home_list_screens":
		data = &[]integration.Screen{}
	case "home_get_screen":
		data = &integration.Screen{}
	case "home_get_nas_status":
		data = &[]integration.Source{}
	case "home_list_photos":
		data = &integration.PhotoList{}
	case "home_get_photo":
		data = &integration.Photo{}
	default:
		data = &integration.Command{}
	}
	if strictReceiptJSON(envelope.Data, data) != nil || !receiptRequiredFields(envelope.Data, reflect.TypeOf(data)) {
		return nil, ErrInvalidOutcome
	}
	switch value := data.(type) {
	case *integration.Home:
		if value.Core != "reachable" || value.Day == "" || value.Timezone == "" {
			return nil, ErrInvalidOutcome
		}
	case *[]integration.Screen:
		if len(*value) > 128 {
			return nil, ErrInvalidOutcome
		}
	case *[]integration.Source:
		if len(*value) > 128 {
			return nil, ErrInvalidOutcome
		}
	case *integration.PhotoList:
		if len(value.Items) > 50 {
			return nil, ErrInvalidOutcome
		}
	case *integration.Screen:
		if value.ID != args["screen_id"] {
			return nil, ErrInvalidOutcome
		}
	case *integration.Photo:
		if value.ID != args["photo_id"] {
			return nil, ErrInvalidOutcome
		}
	case *integration.Command:
		if !identifier.MatchString(value.ID) || !identifier.MatchString(value.ScreenID) {
			return nil, ErrInvalidOutcome
		}
		if tool == "home_get_command" && value.ID != args["command_id"] {
			return nil, ErrInvalidOutcome
		}
		switch value.Status {
		case "accepted", "applied":
			if result.IsError {
				return nil, ErrInvalidOutcome
			}
		case "failed", "expired", "unknown":
			if !result.IsError {
				return nil, ErrInvalidOutcome
			}
		default:
			return nil, ErrInvalidOutcome
		}
	}
	if _, command := data.(*integration.Command); !command && result.IsError {
		return nil, ErrInvalidOutcome
	}
	projected := integration.Response[any]{SchemaVersion: "1", ObservedAt: envelope.Observed, Availability: "available", Data: data}
	return &receiptToolResult{IsError: result.IsError, StructuredContent: projected}, nil
}
func strictReceiptJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrInvalidOutcome
	}
	return nil
}
func receiptBusinessCode(code string) string {
	for _, allowed := range strings.Fields("permission_denied screen_offline photo_unavailable idempotency_conflict operation_expired not_found invalid_arguments busy response_too_large outcome_unknown tls_error core_unreachable invalid_response") {
		if code == allowed {
			return code
		}
	}
	return "tool_error"
}

// NewRuntimeWithReceipt enables private bounded evidence for one invocation.
func NewRuntimeWithReceipt(executor *Executor, receipt *Receipt) *Runtime {
	runtime := NewRuntime(executor)
	runtime.receipt = receipt
	return runtime
}

// Call records deterministic host evidence when an operator enabled a receipt.
func (r *Runtime) Call(id, callID, tool string, args map[string]any) (*mcp.CallToolResult, *ActionReport, error) {
	if r.receipt == nil {
		return r.call(id, callID, tool, args)
	}
	key, err := runtimeKey(id)
	if err != nil {
		return nil, nil, err
	}
	validated, action, err := validateArguments(tool, args)
	if err != nil {
		return nil, nil, err
	}
	r.mu.Lock()
	turn := r.turns[key]
	closed := r.closed
	r.mu.Unlock()
	if closed || turn == nil || turn.ctx.Err() != nil {
		return nil, nil, ErrRunClosed
	}
	release, err := r.receipt.acquire(turn.ctx)
	if err != nil {
		r.Close()
		return nil, nil, ErrReceipt
	}
	defer release()
	result, report, callErr := r.call(id, callID, tool, validated)
	// Read only the host ledger to recover IDs; never accept IDs from MCP errors.
	record := ActionRecord{}
	lookupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	if action != nil {
		record, _ = scanAction(r.executor.ledger.db.QueryRowContext(lookupCtx, `SELECT `+actionColumns+` FROM brain_actions WHERE turn_id=? AND call_id=?`, turn.id, callID))
	} else if tool == "home_get_command" {
		record, _ = scanAction(r.executor.ledger.db.QueryRowContext(lookupCtx, `SELECT `+actionColumns+` FROM brain_actions WHERE command_id=? LIMIT 1`, validated["command_id"]))
	}
	cancel()
	receiptAction := action
	if receiptAction == nil && record.OperationID != "" {
		receiptAction = &record.Action
	}
	var projected *receiptToolResult
	if callErr == nil {
		projected, err = receiptResult(tool, validated, result)
		if err != nil {
			callErr = actionError(record, err)
			result = nil
			if receiptAction != nil {
				value := DescribeAction(*receiptAction, nil, callErr)
				report = &value
			}
			r.Close()
		}
	}
	// Existing ActionOutcomeError carries IDs even if the ledger read failed.
	if callErr != nil && record.OperationID != "" {
		callErr = actionError(record, callErr)
	}
	if report != nil && integration.ValidTraceRef(record.OperationID) {
		copied := *report
		copied.OperationID = record.OperationID
		if copied.CommandID == "" {
			copied.CommandID = record.CommandID
		}
		report = &copied
	}
	row := receiptRecord{Schema: 1, Invocation: r.receipt.invocation, Run: id, Tool: tool, Arguments: validated, Result: projected, Report: report, Error: safeReceiptError(callErr)}
	if callErr == nil && projected != nil && projected.IsError {
		// Keep the trusted durable identity on business failures too. A transport
		// success containing outcome_unknown is still not execution confirmation.
		row.Error = &receiptError{Code: "tool_error"}
		if integration.ValidTraceRef(record.OperationID) {
			row.Error.OperationID = record.OperationID
		}
		if identifier.MatchString(record.CommandID) {
			row.Error.CommandID = record.CommandID
		}
	}
	if err = r.receipt.append(row); err != nil {
		r.Close()
		failure := actionError(record, ErrReceipt)
		var previous *ActionOutcomeError
		if record.OperationID == "" && errors.As(callErr, &previous) {
			failure = &ActionOutcomeError{OperationID: previous.OperationID, CommandID: previous.CommandID, Cause: ErrReceipt}
		}
		if receiptAction != nil {
			value := DescribeAction(*receiptAction, nil, failure)
			report = &value
		}
		return nil, report, failure
	}
	return result, report, callErr
}

// Go's decoder accepts absent scalar fields as zero values. Evidence must not
// turn an omitted online/count/status property into an asserted false or zero.
func receiptRequiredFields(raw []byte, shape reflect.Type) bool {
	if shape.Kind() == reflect.Pointer {
		if string(raw) == "null" {
			return true
		}
		return receiptRequiredFields(raw, shape.Elem())
	}
	if shape == reflect.TypeOf(time.Time{}) {
		return string(raw) != "null"
	}
	switch shape.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return false
		}
		for i := 0; i < shape.NumField(); i++ {
			field := shape.Field(i)
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			value, exists := fields[name]
			optional := false
			for _, part := range tag[1:] {
				if part == "omitempty" {
					optional = true
				}
			}
			if !exists {
				if !optional {
					return false
				}
				continue
			}
			if !receiptRequiredFields(value, field.Type) {
				return false
			}
		}
	case reflect.Slice:
		if string(raw) == "null" {
			return true
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return false
		}
		for _, item := range items {
			if !receiptRequiredFields(item, shape.Elem()) {
				return false
			}
		}
	default:
		if string(raw) == "null" {
			return false
		}
	}
	return true
}
