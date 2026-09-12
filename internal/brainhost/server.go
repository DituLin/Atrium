// Package brainhost adapts trusted local runtime events to the Brain executor.
// JSONL is private process IPC, not a network or model-facing API.
package brainhost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/DituLin/Atrium/internal/brain"
	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type request struct {
	ID        string         `json:"id"`
	Method    string         `json:"method"`
	RunID     string         `json:"run_id,omitempty"`
	CallID    string         `json:"call_id,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}
type protocolError struct {
	Code        string `json:"code"`
	OperationID string `json:"operation_id,omitempty"`
	CommandID   string `json:"command_id,omitempty"`
}
type response struct {
	ID     string         `json:"id"`
	Result any            `json:"result,omitempty"`
	Error  *protocolError `json:"error,omitempty"`
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// Serve owns input, output and runtime. Closing either pipe must unblock its I/O. EOF, cancellation, malformed input, or output
// failure closes all active turns. Calls run concurrently so cancel is readable.
func Serve(parent context.Context, input io.ReadCloser, output io.WriteCloser, runtime *brain.Runtime, tools []*mcp.Tool) (retErr error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = input.Close()
			_ = output.Close()
		case <-stopped:
		}
	}()
	defer close(stopped)
	defer func() { _ = input.Close(); _ = output.Close() }()
	var workers sync.WaitGroup
	queue := make(chan response, 32)
	writerDone := make(chan error, 1)
	go func() {
		encoder := json.NewEncoder(output)
		for value := range queue {
			if err := encoder.Encode(value); err != nil {
				cancel()
				writerDone <- errors.New("runtime output failed")
				return
			}
		}
		writerDone <- nil
	}()
	defer func() {
		if runtime != nil {
			runtime.Close()
		}
		workers.Wait()
		close(queue)
		var err error
		select {
		case err = <-writerDone:
		case <-time.After(time.Second):
			cancel()
			_ = output.Close()
			err = <-writerDone
		}
		if retErr == nil {
			retErr = err
		}
	}()
	send := func(value response) {
		if ctx.Err() != nil {
			return
		}
		select {
		case queue <- value:
		default:
			cancel()
		}
	}
	slots := make(chan struct{}, 4)
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		var req request
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil || !safeID.MatchString(req.ID) {
			return errors.New("invalid runtime request")
		}
		if decoder.Decode(new(any)) != io.EOF {
			return errors.New("invalid runtime request")
		}
		switch req.Method {
		case "tools":
			send(response{ID: req.ID, Result: tools})
		case "start":
			if runtime == nil {
				return errors.New("runtime unavailable")
			}
			err := runtime.Start(ctx, req.RunID)
			send(response{ID: req.ID, Result: map[string]bool{"started": err == nil}, Error: safeError(err)})
		case "end", "cancel":
			if runtime == nil {
				return errors.New("runtime unavailable")
			}
			err := runtime.End(req.RunID)
			send(response{ID: req.ID, Result: map[string]bool{"closed": err == nil}, Error: safeError(err)})
		case "call":
			if runtime == nil {
				return errors.New("runtime unavailable")
			}
			select {
			case slots <- struct{}{}:
			default:
				send(response{ID: req.ID, Error: &protocolError{Code: "busy"}})
				continue
			}
			workers.Add(1)
			go func(req request) {
				defer workers.Done()
				defer func() { <-slots }()
				result, report, err := runtime.Call(req.RunID, req.CallID, req.Tool, req.Arguments)
				send(response{ID: req.ID, Result: struct {
					ToolResult *mcp.CallToolResult `json:"tool_result"`
					Report     *brain.ActionReport `json:"report,omitempty"`
				}{result, report}, Error: safeError(err)})
			}(req)
		default:
			send(response{ID: req.ID, Error: &protocolError{Code: "invalid_request"}})
		}
	}
	if scanner.Err() != nil && ctx.Err() == nil {
		return errors.New("runtime input failed")
	}
	return nil
}

func safeError(err error) *protocolError {
	if err == nil {
		return nil
	}
	out := &protocolError{Code: "request_failed"}
	switch {
	case errors.Is(err, brain.ErrRunClosed):
		out.Code = "run_closed"
	case errors.Is(err, brain.ErrToolBudget):
		out.Code = "tool_budget"
	case errors.Is(err, brain.ErrActionBudget):
		out.Code = "action_budget"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		out.Code = "cancelled"
	case errors.Is(err, brain.ErrActionConflict):
		out.Code = "action_conflict"
	case errors.Is(err, brain.ErrInvalidOutcome), errors.Is(err, brain.ErrCommandConflict):
		out.Code = "outcome_unconfirmed"
	}
	var actionErr *brain.ActionOutcomeError
	if errors.As(err, &actionErr) {
		if integration.ValidTraceRef(actionErr.OperationID) {
			out.OperationID = actionErr.OperationID
		}
		if safeID.MatchString(actionErr.CommandID) {
			out.CommandID = actionErr.CommandID
		}
	}
	return out
}

// WriteRecovery preserves partial trusted observations, the continuation cursor
// and safe recovery IDs even when a later query fails. Failure remains nonzero.
func WriteRecovery(output io.Writer, results []*mcp.CallToolResult, next string, recoveryErr error) error {
	if err := json.NewEncoder(output).Encode(struct {
		Results []*mcp.CallToolResult `json:"results"`
		Next    string                `json:"next_cursor"`
		Error   *protocolError        `json:"error,omitempty"`
	}{results, next, safeError(recoveryErr)}); err != nil {
		return errors.New("recovery output failed")
	}
	if recoveryErr != nil {
		return errors.New("recovery incomplete; retain original operation IDs")
	}
	return nil
}
