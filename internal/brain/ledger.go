// Package brain provides host-side policy and durable action state. It does
// not read Core's database or manage model credentials.
package brain

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite" // Independent Brain database, not the Core store.
)

// Stable host errors never include credentials or household content.
var (
	ErrActionConflict      = errors.New("logical action already has different parameters")
	ErrActionBudget        = errors.New("turn has reached the two-action budget")
	ErrScopeMismatch       = errors.New("ledger belongs to another household or principal")
	ErrCommandConflict     = errors.New("observation belongs to a different command")
	ErrObservationConflict = errors.New("terminal observation cannot change")
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// Action is a bounded screen intent. Operation IDs are never model arguments.
type Action struct {
	Tool       string `json:"tool"`
	ScreenID   string `json:"screen_id"`
	Route      string `json:"route,omitempty"`
	Collection string `json:"collection,omitempty"`
	PhotoID    string `json:"photo_id,omitempty"`
}

func (a Action) validate() error {
	if !identifier.MatchString(a.ScreenID) {
		return errors.New("invalid screen ID")
	}
	switch a.Tool {
	case "home_refresh_screen":
		if a.Route == "" && a.Collection == "" && a.PhotoID == "" {
			return nil
		}
	case "home_show_photo":
		if a.Route == "" && a.Collection == "" && identifier.MatchString(a.PhotoID) {
			return nil
		}
	case "home_navigate_screen":
		if a.PhotoID != "" {
			break
		}
		if a.Route == "dashboard" && a.Collection == "" {
			return nil
		}
		if a.Route == "photos" {
			switch a.Collection {
			case "recent", "captured_today", "random", "all":
				return nil
			}
		}
	}
	return errors.New("invalid screen action")
}

// ActionRecord retains only IDs, bounded parameters and an observed outcome.
type ActionRecord struct {
	OperationID string
	TurnID      string
	CallID      string
	Action      Action
	CommandID   string
	Status      string
	CreatedAt   time.Time
}

// Ledger is isolated from Core. scope binds it to a fixed origin/principal pair.
type Ledger struct {
	db  *sql.DB
	now func() time.Time
}

// OpenLedger opens a private host ledger. Its parent must already exist with
// mode 0700. It refuses symlinks, unsafe modes, foreign databases and scope reuse.
func OpenLedger(path, scope string, now func() time.Time) (*Ledger, error) {
	if scope == "" || len(scope) > 512 {
		return nil, errors.New("ledger scope is required")
	}
	if now == nil {
		now = time.Now
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("invalid ledger path")
	}
	parent, err := os.Lstat(filepath.Dir(absolute))
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		return nil, errors.New("ledger requires a private 0700 parent directory")
	}
	f, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600) // #nosec G304 -- explicit private host state file.
	if err == nil {
		_ = f.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return nil, errors.New("cannot create ledger")
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errors.New("ledger must be a regular non-symlink 0600 file")
	}
	uri := url.URL{Scheme: "file", Path: absolute}
	db, err := sql.Open("sqlite", uri.String()+"?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var activeTx *sql.Tx
	fail := func(err error) (*Ledger, error) {
		if activeTx != nil {
			_ = activeTx.Rollback()
		}
		_ = db.Close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	activeTx = tx
	defer func() { _ = tx.Rollback() }()
	var tables, meta int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(name='brain_meta'),0) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables, &meta); err != nil {
		return fail(err)
	}
	if tables > 0 && meta == 0 {
		return fail(errors.New("refusing a database not owned by Brain"))
	}
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS brain_meta (singleton INTEGER PRIMARY KEY CHECK(singleton=1),scope TEXT NOT NULL,version INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS brain_actions (operation_id TEXT PRIMARY KEY,turn_id TEXT NOT NULL,call_id TEXT NOT NULL,action_json TEXT NOT NULL,command_id TEXT NOT NULL DEFAULT '',status TEXT NOT NULL DEFAULT 'unconfirmed',created_at INTEGER NOT NULL,UNIQUE(turn_id,call_id));
 CREATE TABLE IF NOT EXISTS brain_runtime_runs (run_key TEXT PRIMARY KEY,turn_id TEXT UNIQUE);`)
	if err != nil {
		return fail(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO brain_meta(singleton,scope,version) VALUES(1,?,1)`, scope)
	if err != nil {
		return fail(err)
	}
	var got string
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT scope,version FROM brain_meta WHERE singleton=1`).Scan(&got, &version); err != nil {
		return fail(err)
	}
	if got != scope {
		return fail(ErrScopeMismatch)
	}
	if version != 1 {
		return fail(errors.New("unsupported Brain ledger version"))
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	if _, err = db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		return fail(err)
	}
	return &Ledger{db: db, now: now}, nil
}

// Close releases the ledger connection.
func (l *Ledger) Close() error { return l.db.Close() }

const actionColumns = `operation_id,turn_id,call_id,action_json,command_id,status,created_at`

func scanAction(row interface{ Scan(...any) error }) (ActionRecord, error) {
	var r ActionRecord
	var raw string
	var at int64
	if err := row.Scan(&r.OperationID, &r.TurnID, &r.CallID, &raw, &r.CommandID, &r.Status, &at); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(raw), &r.Action); err != nil {
		return r, errors.New("invalid ledger action")
	}
	r.CreatedAt = time.UnixMilli(at).UTC()
	return r, nil
}

// BeginAction commits the operation before permitting its first network send.
// A false send result ALWAYS means query-only recovery, even if the prior
// process crashed before reaching Core. It never authorizes an automatic replay.
func (l *Ledger) BeginAction(ctx context.Context, turnID, callID string, action Action) (record ActionRecord, send bool, err error) {
	if !identifier.MatchString(turnID) || !identifier.MatchString(callID) {
		return record, false, errors.New("invalid host turn or call ID")
	}
	if err = action.validate(); err != nil {
		return record, false, err
	}
	raw, _ := json.Marshal(action)
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return record, false, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err = scanAction(tx.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM brain_actions WHERE turn_id=? AND call_id=?`, turnID, callID))
	if err == nil {
		if record.Action != action {
			return record, false, ErrActionConflict
		}
		return record, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return record, false, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM brain_actions WHERE turn_id=?`, turnID).Scan(&count); err != nil {
		return record, false, err
	}
	if count >= 2 {
		return record, false, ErrActionBudget
	}
	now := l.now().UTC()
	id, err := ulid.New(ulid.Timestamp(now), rand.Reader)
	if err != nil {
		return record, false, err
	}
	record = ActionRecord{OperationID: id.String(), TurnID: turnID, CallID: callID, Action: action, Status: "unconfirmed", CreatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO brain_actions(operation_id,turn_id,call_id,action_json,created_at) VALUES(?,?,?,?,?)`, record.OperationID, turnID, callID, string(raw), now.UnixMilli())
	if err != nil {
		return record, false, err
	}
	if err = tx.Commit(); err != nil {
		return record, false, err
	}
	return record, true, nil
}

// Observe stores only a validated Core command outcome, never model prose.
func (l *Ledger) Observe(ctx context.Context, operationID, commandID, status string) error {
	if !identifier.MatchString(commandID) {
		return errors.New("invalid command ID")
	}
	switch status {
	case "accepted", "applied", "failed", "expired", "unknown":
	default:
		return errors.New("invalid command status")
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := scanAction(tx.QueryRowContext(ctx, `SELECT `+actionColumns+` FROM brain_actions WHERE operation_id=?`, operationID))
	if err != nil {
		return err
	}
	if record.CommandID != "" && record.CommandID != commandID {
		return ErrCommandConflict
	}
	if terminal(record.Status) && record.Status != status {
		return ErrObservationConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE brain_actions SET command_id=?,status=? WHERE operation_id=?`, commandID, status, operationID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func terminal(status string) bool {
	return status == "applied" || status == "failed" || status == "expired" || status == "unknown"
}

// Pending returns the first recovery page. Use PendingPage to enumerate all
// actions; the absence of a Core operation does not remove it from this queue.
func (l *Ledger) Pending(ctx context.Context) ([]ActionRecord, error) {
	records, _, err := l.PendingPage(ctx, "")
	return records, err
}

// PendingPage enumerates uncertain actions without starving later records.
// Pass next as after for the next page; an empty next marks the end of this pass.
// The cursor is local host state, never a model-supplied operation identifier.
func (l *Ledger) PendingPage(ctx context.Context, after string) ([]ActionRecord, string, error) {
	if after != "" {
		if _, err := ulid.ParseStrict(after); err != nil {
			return nil, "", errors.New("invalid recovery cursor")
		}
	}
	rows, err := l.db.QueryContext(ctx, `SELECT `+actionColumns+` FROM brain_actions WHERE status IN ('unconfirmed','accepted') AND operation_id>? ORDER BY operation_id LIMIT 101`, after)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rows.Close() }()
	out := []ActionRecord{}
	for rows.Next() {
		r, err := scanAction(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > 100 {
		out = out[:100]
		next = out[99].OperationID
	}
	return out, next, nil
}
