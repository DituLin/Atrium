package brain

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrRunClosed rejects missing, ended, expired, or previously claimed host runs.
var ErrRunClosed = errors.New("runtime run is not active")

// Runtime binds trusted external run identities to host-created bounded turns.
// External identifiers must come from runtime hooks, never model arguments.
// A process restart cannot start a previously claimed run again; pending actions
// remain available through Executor.RecoverPage without resubmission.
type Runtime struct {
	executor *Executor
	mu       sync.Mutex
	turns    map[string]*Turn
	closed   bool
	receipt  *Receipt
}

// NewRuntime creates a local runtime bound to one scoped executor.
func NewRuntime(executor *Executor) *Runtime {
	return &Runtime{executor: executor, turns: make(map[string]*Turn)}
}

func runtimeKey(id string) (string, error) {
	if !identifier.MatchString(id) {
		return "", ErrRunClosed
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:]), nil
}

// Start durably claims an external run before making its bounded turn available.
func (r *Runtime) Start(ctx context.Context, id string) error {
	key, err := runtimeKey(id)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRunClosed
	}
	if turn := r.turns[key]; turn != nil {
		if turn.ctx.Err() != nil {
			return ErrRunClosed
		}
		return nil
	}
	for old, turn := range r.turns {
		if turn.ctx.Err() != nil {
			turn.Close()
			delete(r.turns, old)
		}
	}
	if len(r.turns) >= 128 {
		return errors.New("runtime active run limit reached")
	}
	turn := r.executor.StartTurn(ctx)
	// The durable claim is committed before a model tool can enter this turn.
	result, err := r.executor.ledger.db.ExecContext(turn.ctx, `INSERT OR IGNORE INTO brain_runtime_runs (run_key,turn_id) VALUES (?,?)`, key, turn.id)
	if err != nil {
		turn.Close()
		return errors.New("cannot claim runtime run")
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		turn.Close()
		return ErrRunClosed
	}
	r.turns[key] = turn
	return nil
}

// Call shares the original turn deadline, tool budget and action ledger.
func (r *Runtime) call(id, callID, tool string, args map[string]any) (*mcp.CallToolResult, *ActionReport, error) {
	key, err := runtimeKey(id)
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
	_, action, err := validateArguments(tool, args)
	if err != nil {
		return nil, nil, err
	}
	result, err := turn.Call(callID, tool, args)
	if action == nil && tool == "home_get_command" && err == nil {
		commandID, _ := args["command_id"].(string)
		record, lookupErr := scanAction(r.executor.ledger.db.QueryRowContext(turn.ctx, `SELECT `+actionColumns+` FROM brain_actions WHERE command_id=? LIMIT 1`, commandID))
		if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
			return nil, nil, errors.New("cannot read action receipt")
		}
		if lookupErr == nil {
			err = r.executor.observe(turn.ctx, record, result)
			report := DescribeAction(record.Action, result, err)
			if err != nil {
				return nil, &report, err
			}
			return result, &report, nil
		}
	}
	if action == nil {
		return result, nil, err
	}

	report := DescribeAction(*action, result, err)
	return result, &report, err
}

// End also persists cancellation that arrives before start. A failed durable
// cancellation closes this Runtime, rather than allowing subsequent actions.
func (r *Runtime) End(id string) error {
	key, err := runtimeKey(id)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if turn := r.turns[key]; turn != nil {
		turn.Close()
		delete(r.turns, key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = r.executor.ledger.db.ExecContext(ctx, `INSERT OR IGNORE INTO brain_runtime_runs (run_key,turn_id) VALUES (?,NULL)`, key)
	if err != nil {
		r.closed = true
		for _, turn := range r.turns {
			turn.Close()
		}
		return errors.New("cannot persist runtime cancellation")
	}
	return nil
}

// Close cancels all active turns and rejects future starts.
func (r *Runtime) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	for key, turn := range r.turns {
		turn.Close()
		delete(r.turns, key)
	}
}
