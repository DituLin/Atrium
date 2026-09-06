package store

import (
	"context"
	"errors"
	"time"
)

// Retention windows from design §5. Commands, scan runs, the audit log and
// finished jobs are operational history, not system state, so a week is
// enough to diagnose an incident without accumulating forever.
const (
	HistoryRetention = 7 * 24 * time.Hour
	PairingRetention = 24 * time.Hour
)

// RetentionResult counts what one pass deleted.
type RetentionResult struct {
	Commands int64 `json:"commands"`
	ScanRuns int64 `json:"scan_runs"`
	AuditLog int64 `json:"audit_log"`
	Jobs     int64 `json:"jobs"`
	Pairings int64 `json:"pairings"`
}

// Total returns the number of rows removed.
func (r RetentionResult) Total() int64 {
	return r.Commands + r.ScanRuns + r.AuditLog + r.Jobs + r.Pairings
}

// RunRetention deletes history past its window. It is run daily and once at
// startup so a long outage cannot leave a month of rows behind.
func (d *DB) RunRetention(ctx context.Context, now time.Time) (RetentionResult, error) {
	history := now.Add(-HistoryRetention)
	pairings := now.Add(-PairingRetention)

	var res RetentionResult
	var errs []error
	collect := func(n int64, err error, into *int64) {
		if err != nil {
			errs = append(errs, err)
			return
		}
		*into = n
	}

	n, err := d.Commands().DeleteOlderThan(ctx, history)
	collect(n, err, &res.Commands)
	n, err = d.ScanRuns().DeleteOlderThan(ctx, history)
	collect(n, err, &res.ScanRuns)
	n, err = d.Audit().DeleteOlderThan(ctx, history)
	collect(n, err, &res.AuditLog)
	n, err = d.Jobs().DeleteTerminalBefore(ctx, history)
	collect(n, err, &res.Jobs)
	n, err = d.Pairings().DeleteOlderThan(ctx, pairings)
	collect(n, err, &res.Pairings)

	return res, errors.Join(errs...)
}
