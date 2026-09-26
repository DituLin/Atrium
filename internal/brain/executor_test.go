package brain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeCaller struct {
	mu    sync.Mutex
	names []string
	reply *mcp.CallToolResult
	err   error
}

func (f *fakeCaller) CallTool(ctx context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names = append(f.names, p.Name)
	return f.reply, f.err
}
func executorLedger(t *testing.T) (*Ledger, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "brain.db")
	l, err := OpenLedger(path, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, path
}
func commandReply(status string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: status != "accepted" && status != "applied", StructuredContent: map[string]any{"schema_version": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "availability": "available", "data": map[string]any{"id": "cmd1", "screen_id": "tv", "status": status, "kind": "refresh", "payload": map[string]any{}}}}
}
func TestExecutorDuplicateAndRecovery(t *testing.T) {
	l, path := executorLedger(t)
	f := &fakeCaller{reply: commandReply("accepted")}
	e := NewExecutor(f, l)
	turn := e.StartTurn(context.Background())
	defer turn.Close()
	for range 2 {
		if _, err := turn.Call("call1", "home_refresh_screen", map[string]any{"screen_id": "tv"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.names) != 2 || f.names[0] != "home_refresh_screen" || f.names[1] != "home_get_operation" {
		t.Fatal(f.names)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLedger(path, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	f.reply = commandReply("applied")
	if _, err = NewExecutor(f, reopened).Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.names[2] != "home_get_operation" {
		t.Fatal(f.names)
	}
	pending, err := reopened.Pending(context.Background())
	if err != nil || len(pending) != 0 {
		t.Fatal(pending, err)
	}
}
func TestExecutorBoundaries(t *testing.T) {
	l, _ := executorLedger(t)
	f := &fakeCaller{}
	e := NewExecutor(f, l)
	turn := e.StartTurn(context.Background())
	defer turn.Close()
	for _, tc := range []struct {
		name string
		args map[string]any
	}{{"shell", nil}, {"home_refresh_screen", map[string]any{"screen_id": "tv", "operation_id": "fake"}}, {"home_get_status", map[string]any{"path": "/tmp"}}, {"home_get_photo", map[string]any{"photo_id": "../x"}}, {"home_list_photos", map[string]any{"limit": 51}}, {"home_navigate_screen", map[string]any{"screen_id": "tv", "route": "photos"}}} {
		if _, err := turn.Call("bad", tc.name, tc.args); err == nil {
			t.Fatal(tc)
		}
	}
	if len(f.names) != 0 {
		t.Fatal(f.names)
	}
	for range 8 {
		if _, err := turn.Call("read", "home_get_status", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := turn.Call("read", "home_get_status", nil); !errors.Is(err, ErrToolBudget) {
		t.Fatal(err)
	}
	canceled := e.StartTurn(context.Background())
	canceled.Close()
	if _, err := canceled.Call("read", "home_get_status", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(f.names) != 8 {
		t.Fatal(f.names)
	}
}
func TestExecutorObservations(t *testing.T) {
	for _, status := range []string{"accepted", "applied", "failed", "expired", "unknown"} {
		t.Run(status, func(t *testing.T) {
			l, _ := executorLedger(t)
			f := &fakeCaller{reply: commandReply(status)}
			turn := NewExecutor(f, l).StartTurn(context.Background())
			defer turn.Close()
			if _, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); err != nil {
				t.Fatal(err)
			}
			var got string
			if err := l.db.QueryRow("SELECT status FROM brain_actions").Scan(&got); err != nil || got != status {
				t.Fatal(got, err)
			}
		})
	}
}
func TestExecutorSyntheticUnknownRemainsPending(t *testing.T) {
	l, _ := executorLedger(t)
	r := commandReply("unknown")
	r.StructuredContent.(map[string]any)["error"] = map[string]any{"code": "outcome_unknown"}
	r.StructuredContent.(map[string]any)["availability"] = "unknown"
	f := &fakeCaller{reply: r}
	turn := NewExecutor(f, l).StartTurn(context.Background())
	defer turn.Close()
	if _, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); err != nil {
		t.Fatal(err)
	}
	pending, err := l.Pending(context.Background())
	if err != nil || len(pending) != 1 || pending[0].CommandID != "" {
		t.Fatal(pending, err)
	}
	f.reply = commandReply("applied")
	if _, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); err != nil {
		t.Fatal(err)
	}
}

type blockingCaller struct {
	entered chan string
	release chan struct{}
}

func (f *blockingCaller) CallTool(ctx context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	select {
	case f.entered <- p.Name:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-f.release:
		return commandReply("accepted"), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func TestExecutorSameScreenWaitCanCancel(t *testing.T) {
	l, _ := executorLedger(t)
	f := &blockingCaller{entered: make(chan string, 2), release: make(chan struct{})}
	e := NewExecutor(f, l)
	a := e.StartTurn(context.Background())
	defer a.Close()
	done := make(chan error, 1)
	go func() { _, err := a.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); done <- err }()
	<-f.entered
	b := e.StartTurn(context.Background())
	other := make(chan error, 1)
	go func() {
		_, err := b.Call("two", "home_refresh_screen", map[string]any{"screen_id": "tv"})
		other <- err
	}()
	b.Close()
	if err := <-other; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case name := <-f.entered:
		t.Fatalf("unexpected concurrent call %s", name)
	default:
	}
	close(f.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var n int
	if err := l.db.QueryRow("SELECT COUNT(*) FROM brain_actions").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
func TestExecutorTransportFailureNeverResends(t *testing.T) {
	l, _ := executorLedger(t)
	f := &fakeCaller{err: errors.New("transport unavailable")}
	turn := NewExecutor(f, l).StartTurn(context.Background())
	defer turn.Close()
	if _, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); err == nil {
		t.Fatal("transport error hidden")
	}
	f.err = nil
	f.reply = commandReply("accepted")
	if _, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); err != nil {
		t.Fatal(err)
	}
	if f.names[1] != "home_get_operation" {
		t.Fatal(f.names)
	}
}
func TestExecutorRejectsUnverifiableObservations(t *testing.T) {
	for _, change := range []func(map[string]any){func(m map[string]any) { m["schema_version"] = "2" }, func(m map[string]any) { delete(m, "observed_at") }, func(m map[string]any) { m["availability"] = "unknown" }, func(m map[string]any) { m["data"].(map[string]any)["screen_id"] = "different" }, func(m map[string]any) { m["data"].(map[string]any)["status"] = "success" }, func(m map[string]any) { m["data"].(map[string]any)["kind"] = "show" }} {
		l, _ := executorLedger(t)
		r := commandReply("applied")
		change(r.StructuredContent.(map[string]any))
		f := &fakeCaller{reply: r}
		turn := NewExecutor(f, l).StartTurn(context.Background())
		if result, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); !errors.Is(err, ErrInvalidOutcome) || result != nil {
			t.Fatalf("unverified outcome exposed: %v %v", result, err)
		}
		turn.Close()
		pending, err := l.Pending(context.Background())
		if err != nil || len(pending) != 1 || pending[0].CommandID != "" {
			t.Fatal(pending, err)
		}
	}
}
func TestExecutorActionBudget(t *testing.T) {
	l, _ := executorLedger(t)
	f := &fakeCaller{reply: commandReply("accepted")}
	turn := NewExecutor(f, l).StartTurn(context.Background())
	defer turn.Close()
	for _, ref := range []string{"one", "two"} {
		if _, err := turn.Call(ref, "home_refresh_screen", map[string]any{"screen_id": "tv"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := turn.Call("three", "home_refresh_screen", map[string]any{"screen_id": "tv"}); !errors.Is(err, ErrActionBudget) {
		t.Fatal(err)
	}
	if len(f.names) != 2 {
		t.Fatal(f.names)
	}
}

func TestExecutorTurnDeadlineAndCommandBinding(t *testing.T) {
	l, _ := executorLedger(t)
	f := &fakeCaller{reply: commandReply("accepted")}
	e := NewExecutor(f, l)
	started := time.Now()
	turn := e.StartTurn(context.Background())
	defer turn.Close()
	deadline, ok := turn.ctx.Deadline()
	if !ok || deadline.Before(started.Add(29*time.Second)) || deadline.After(time.Now().Add(30*time.Second)) {
		t.Fatal(deadline)
	}
	if _, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); err != nil {
		t.Fatal(err)
	}
	f.reply = commandReply("applied")
	f.reply.StructuredContent.(map[string]any)["data"].(map[string]any)["id"] = "different"
	if _, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); !errors.Is(err, ErrCommandConflict) {
		t.Fatal(err)
	}
	pending, err := l.Pending(context.Background())
	if err != nil || len(pending) != 1 || pending[0].CommandID != "cmd1" || pending[0].Status != "accepted" {
		t.Fatal(pending, err)
	}
}

var _ ToolCaller = (*mcp.ClientSession)(nil)

func TestExecutorReservationRefund(t *testing.T) {
	l, _ := executorLedger(t)
	f := &fakeCaller{reply: commandReply("accepted")}
	turn := NewExecutor(f, l).StartTurn(context.Background())
	defer turn.Close()
	if _, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := turn.Call("one", "home_navigate_screen", map[string]any{"screen_id": "tv", "route": "dashboard"}); !errors.Is(err, ErrActionConflict) {
			t.Fatal(err)
		}
	}
	for range 7 {
		if _, err := turn.Call("read", "home_get_status", nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.names) != 8 {
		t.Fatal(f.names)
	}
	if _, err := turn.Call("read", "home_get_status", nil); !errors.Is(err, ErrToolBudget) {
		t.Fatal(err)
	}
}

type recoveryCaller struct {
	last  string
	count int
}

func (f *recoveryCaller) CallTool(_ context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	f.count++
	if p.Name != "home_get_operation" {
		return nil, errors.New("recovery attempted mutation")
	}
	if p.Arguments.(map[string]any)["operation_id"] == f.last {
		return commandReply("applied"), nil
	}
	return &mcp.CallToolResult{IsError: true, StructuredContent: map[string]any{"schema_version": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "availability": "unknown", "data": nil, "error": map[string]any{"code": "not_found"}}}, nil
}
func TestExecutorRecoveryContinuesPastUnresolvedPage(t *testing.T) {
	l, _ := executorLedger(t)
	for i := 0; i < 101; i++ {
		_, _, err := l.BeginAction(context.Background(), fmt.Sprintf("turn%d", i), "call", Action{Tool: "home_refresh_screen", ScreenID: "tv"})
		if err != nil {
			t.Fatal(err)
		}
	}
	var last string
	if err := l.db.QueryRow("SELECT MAX(operation_id) FROM brain_actions").Scan(&last); err != nil {
		t.Fatal(err)
	}
	f := &recoveryCaller{last: last}
	e := NewExecutor(f, l)
	first, next, err := e.RecoverPage(context.Background(), "")
	if err != nil || len(first) != 100 || next == "" {
		t.Fatal(len(first), next, err)
	}
	second, next, err := e.RecoverPage(context.Background(), next)
	if err != nil || len(second) != 1 || next != "" || f.count != 101 {
		t.Fatal(len(second), next, f.count, err)
	}
	var status string
	if err := l.db.QueryRow("SELECT status FROM brain_actions WHERE operation_id=?", last).Scan(&status); err != nil || status != "applied" {
		t.Fatal(status, err)
	}
}
func TestExecutorRecoveryDoesNotExposeInvalidApplied(t *testing.T) {
	l, _ := executorLedger(t)
	_, _, err := l.BeginAction(context.Background(), "turn", "call", Action{Tool: "home_refresh_screen", ScreenID: "tv"})
	if err != nil {
		t.Fatal(err)
	}
	r := commandReply("applied")
	r.StructuredContent.(map[string]any)["schema_version"] = "2"
	results, _, err := NewExecutor(&fakeCaller{reply: r}, l).RecoverPage(context.Background(), "")
	if !errors.Is(err, ErrInvalidOutcome) || len(results) != 0 {
		t.Fatal(results, err)
	}
}

func TestExecutorOutcomeErrorCarriesDurableIdentity(t *testing.T) {
	l, _ := executorLedger(t)
	cause := errors.New("private transport detail must not appear")
	f := &fakeCaller{err: cause}
	turn := NewExecutor(f, l).StartTurn(context.Background())
	defer turn.Close()
	_, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	var outcome *ActionOutcomeError
	if !errors.As(err, &outcome) || !errors.Is(err, cause) || outcome.OperationID == "" || outcome.CommandID != "" || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	pending, _ := l.Pending(context.Background())
	if len(pending) != 1 || pending[0].OperationID != outcome.OperationID {
		t.Fatal(pending, outcome)
	}
	f.err = nil
	f.reply = commandReply("accepted")
	if _, err = turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"}); err != nil {
		t.Fatal(err)
	}
	f.err = context.Canceled
	_, err = turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	if !errors.As(err, &outcome) || !errors.Is(err, context.Canceled) || outcome.CommandID != "cmd1" || outcome.OperationID != pending[0].OperationID {
		t.Fatal(err, outcome)
	}
}

type callbackCaller func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)

func (f callbackCaller) CallTool(ctx context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return f(ctx, p)
}
func TestExecutorOutcomeErrorRetainsValidatedIDOnPersistenceFailure(t *testing.T) {
	l, _ := executorLedger(t)
	f := callbackCaller(func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		_ = l.Close()
		return commandReply("applied"), nil
	})
	turn := NewExecutor(f, l).StartTurn(context.Background())
	defer turn.Close()
	result, err := turn.Call("one", "home_refresh_screen", map[string]any{"screen_id": "tv"})
	var outcome *ActionOutcomeError
	if result != nil || !errors.As(err, &outcome) || outcome.CommandID != "cmd1" || outcome.OperationID == "" {
		t.Fatal(result, err)
	}
}
func TestExecutorRecoveryErrorIdentifiesCurrentOperation(t *testing.T) {
	l, _ := executorLedger(t)
	for _, ref := range []string{"one", "two"} {
		_, _, err := l.BeginAction(context.Background(), "turn", ref, Action{Tool: "home_refresh_screen", ScreenID: "tv"})
		if err != nil {
			t.Fatal(err)
		}
	}
	records, _, err := l.PendingPage(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	cause := errors.New("disconnected")
	f := callbackCaller(func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		calls++
		if calls == 1 {
			return commandReply("accepted"), nil
		}
		return nil, cause
	})
	results, _, err := NewExecutor(f, l).RecoverPage(context.Background(), "")
	var outcome *ActionOutcomeError
	if len(results) != 1 || !errors.As(err, &outcome) || !errors.Is(err, cause) || outcome.OperationID != records[1].OperationID {
		t.Fatal(results, err, outcome)
	}
}

func TestExecutorScreenLocksAreReleased(t *testing.T) {
	e := NewExecutor(nil, nil)
	for i := 0; i < 200; i++ {
		release, err := e.lockScreen(context.Background(), fmt.Sprintf("tv%d", i))
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	e.mu.Lock()
	n := len(e.screens)
	e.mu.Unlock()
	if n != 0 {
		t.Fatalf("retained %d screen locks", n)
	}
}
func TestExecutorScreenLockWaitersShareLifetime(t *testing.T) {
	e := NewExecutor(nil, nil)
	release, err := e.lockScreen(context.Background(), "tv")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() { _, err := e.lockScreen(ctx, "tv"); canceled <- err }()
	waiter := make(chan func(), 1)
	go func() {
		unlock, err := e.lockScreen(context.Background(), "tv")
		if err != nil {
			panic(err)
		}
		waiter <- unlock
	}()
	deadline := time.After(time.Second)
	for {
		e.mu.Lock()
		refs := e.screens["tv"].refs
		e.mu.Unlock()
		if refs == 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("waiters did not register")
		default:
			runtime.Gosched()
		}
	}
	cancel()
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	second := <-waiter
	third := make(chan func(), 1)
	go func() {
		unlock, err := e.lockScreen(context.Background(), "tv")
		if err != nil {
			panic(err)
		}
		third <- unlock
	}()
	select {
	case <-third:
		t.Fatal("new caller bypassed existing waiter lock")
	case <-time.After(20 * time.Millisecond):
	}
	second()
	last := <-third
	last()
	e.mu.Lock()
	n := len(e.screens)
	e.mu.Unlock()
	if n != 0 {
		t.Fatalf("retained %d screen locks", n)
	}
}
