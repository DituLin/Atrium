package brain

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestLedgerConcurrentCallsAndRestartNeverResubmit(t *testing.T) {
	ctx := context.Background()
	path := privateLedgerPath(t)
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	first, err := OpenLedger(path, "household/principal-1", func() time.Time { return now })
	require.NoError(t, err)
	second, err := OpenLedger(path, "household/principal-1", func() time.Time { return now })
	require.NoError(t, err)
	action := Action{Tool: "home_navigate_screen", ScreenID: "tv", Route: "dashboard"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ids []string
	sent := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ledger := first
			if i%2 == 1 {
				ledger = second
			}
			record, send, err := ledger.BeginAction(ctx, "turn-1", "call-1", action)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Error(err)
				return
			}
			ids = append(ids, record.OperationID)
			if send {
				sent++
			}
		}(i)
	}
	wg.Wait()
	require.Len(t, ids, 20)
	require.Equal(t, 1, sent)
	for _, id := range ids {
		require.Equal(t, ids[0], id)
	}
	id, err := ulid.ParseStrict(ids[0])
	require.NoError(t, err)
	require.Equal(t, ulid.Timestamp(now), id.Time())
	require.NoError(t, first.Close())
	require.NoError(t, second.Close())
	reopened, err := OpenLedger(path, "household/principal-1", func() time.Time { return now.Add(time.Hour) })
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()
	record, send, err := reopened.BeginAction(ctx, "turn-1", "call-1", action)
	require.NoError(t, err)
	require.False(t, send)
	require.Equal(t, ids[0], record.OperationID)
	pending, err := reopened.Pending(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "unconfirmed", pending[0].Status)
	require.NoError(t, reopened.Observe(ctx, record.OperationID, "command-1", "applied"))
	pending, err = reopened.Pending(ctx)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestLedgerConflictsBudgetAndScope(t *testing.T) {
	path := privateLedgerPath(t)
	ledger, err := OpenLedger(path, "household/p1", time.Now)
	require.NoError(t, err)
	defer func() { _ = ledger.Close() }()
	ctx := context.Background()
	action := Action{Tool: "home_refresh_screen", ScreenID: "tv"}
	record, _, err := ledger.BeginAction(ctx, "turn", "one", action)
	require.NoError(t, err)
	changed := action
	changed.ScreenID = "other"
	_, _, err = ledger.BeginAction(ctx, "turn", "one", changed)
	require.ErrorIs(t, err, ErrActionConflict)
	_, _, err = ledger.BeginAction(ctx, "turn", "two", action)
	require.NoError(t, err)
	_, _, err = ledger.BeginAction(ctx, "turn", "three", action)
	require.ErrorIs(t, err, ErrActionBudget)
	require.Error(t, ledger.Observe(ctx, record.OperationID, "command-1", "invented"))
	require.NoError(t, ledger.Observe(ctx, record.OperationID, "command-1", "accepted"))
	require.ErrorIs(t, ledger.Observe(ctx, record.OperationID, "command-2", "applied"), ErrCommandConflict)
	other, err := OpenLedger(path, "household/p2", time.Now)
	if other != nil {
		_ = other.Close()
	}
	require.ErrorIs(t, err, ErrScopeMismatch)
}

func privateLedgerPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	return filepath.Join(dir, "brain.db")
}

func TestLedgerRejectsUnsafeFilesAndInvalidActions(t *testing.T) {
	path := privateLedgerPath(t)
	require.NoError(t, os.WriteFile(path, []byte("do not change"), 0644))
	_, err := OpenLedger(path, "home/p", time.Now)
	require.Error(t, err)
	require.NoError(t, os.Remove(path))
	target := filepath.Join(filepath.Dir(path), "target")
	require.NoError(t, os.WriteFile(target, []byte("do not change"), 0600))
	require.NoError(t, os.Symlink(target, path))
	_, err = OpenLedger(path, "home/p", time.Now)
	require.Error(t, err)
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "do not change", string(content))
	require.NoError(t, os.Remove(path))
	ledger, err := OpenLedger(path, "home/p", time.Now)
	require.NoError(t, err)
	defer func() { _ = ledger.Close() }()
	for _, action := range []Action{{Tool: "shell", ScreenID: "tv"}, {Tool: "home_navigate_screen", ScreenID: "tv", Route: "photos"}, {Tool: "home_refresh_screen", ScreenID: "../private"}, {Tool: "home_show_photo", ScreenID: "tv", PhotoID: "https://private/photo"}} {
		_, _, err := ledger.BeginAction(context.Background(), "turn", "call", action)
		require.Error(t, err)
	}
	pending, err := ledger.Pending(context.Background())
	require.NoError(t, err)
	require.Empty(t, pending)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		require.Zero(t, info.Mode().Perm()&0077)
	}
}

func TestLedgerTerminalOutcomeCannotRegress(t *testing.T) {
	ledger, err := OpenLedger(privateLedgerPath(t), "home/p", time.Now)
	require.NoError(t, err)
	defer func() { _ = ledger.Close() }()
	ctx := context.Background()
	r, _, err := ledger.BeginAction(ctx, "turn", "call", Action{Tool: "home_refresh_screen", ScreenID: "tv"})
	require.NoError(t, err)
	require.NoError(t, ledger.Observe(ctx, r.OperationID, "cmd", "applied"))
	require.ErrorIs(t, ledger.Observe(ctx, r.OperationID, "cmd", "accepted"), ErrObservationConflict)
	require.NoError(t, ledger.Observe(ctx, r.OperationID, "cmd", "applied"))
}

func TestLedgerConfirmedUnknownIsTerminal(t *testing.T) {
	ledger, err := OpenLedger(privateLedgerPath(t), "home/p", time.Now)
	require.NoError(t, err)
	defer func() { _ = ledger.Close() }()
	ctx := context.Background()
	r, _, err := ledger.BeginAction(ctx, "turn", "call", Action{Tool: "home_refresh_screen", ScreenID: "tv"})
	require.NoError(t, err)
	require.NoError(t, ledger.Observe(ctx, r.OperationID, "cmd", "unknown"))
	require.ErrorIs(t, ledger.Observe(ctx, r.OperationID, "cmd", "applied"), ErrObservationConflict)
	require.ErrorIs(t, ledger.Observe(ctx, r.OperationID, "cmd", "accepted"), ErrObservationConflict)
	pending, err := ledger.Pending(ctx)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestLedgerPendingPagesDoNotStarveLaterActions(t *testing.T) {
	ledger, err := OpenLedger(privateLedgerPath(t), "home/p", time.Now)
	require.NoError(t, err)
	defer func() { _ = ledger.Close() }()
	ctx := context.Background()
	for i := 0; i < 101; i++ {
		_, _, err := ledger.BeginAction(ctx, fmt.Sprintf("turn-%d", i), "call", Action{Tool: "home_refresh_screen", ScreenID: "tv"})
		require.NoError(t, err)
	}
	first, next, err := ledger.PendingPage(ctx, "")
	require.NoError(t, err)
	require.Len(t, first, 100)
	require.NotEmpty(t, next)
	last, next, err := ledger.PendingPage(ctx, next)
	require.NoError(t, err)
	require.Len(t, last, 1)
	require.Empty(t, next)
	for _, r := range first {
		require.NotEqual(t, r.OperationID, last[0].OperationID)
	}
}

func TestLedgerRefusesForeignDatabaseWithoutAddingTables(t *testing.T) {
	path := privateLedgerPath(t)
	require.NoError(t, os.WriteFile(path, nil, 0600))
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE private_core_data(value TEXT);INSERT INTO private_core_data VALUES('preserve')`)
	require.NoError(t, err)
	_, err = OpenLedger(path, "home/p", time.Now)
	require.Error(t, err)
	var tables int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&tables))
	require.Equal(t, 1, tables)
	var value string
	require.NoError(t, db.QueryRow(`SELECT value FROM private_core_data`).Scan(&value))
	require.Equal(t, "preserve", value)
}
