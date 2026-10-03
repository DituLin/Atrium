package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// ArchiveTasks owns durable tasks, fenced worker leases and atomic checkpoints.
// Household IDs must come from trusted host context, including worker scope.
// This repository does not execute JSON payloads or grant source-file access.
type ArchiveTasks struct{ db *DB }

func (d *DB) ArchiveTasks() *ArchiveTasks { return &ArchiveTasks{db: d} }

const archiveTaskColumns = `id,household_id,kind,request_key,input_json,status,stage,attempts,attempt_id,version,state_json,result_json,clarification_json,available_at,lease_token,lease_expires_at,last_error_code,created_at,updated_at`

func scanArchiveTask(row interface{ Scan(...any) error }) (*domain.ArchiveTask, error) {
	var task domain.ArchiveTask
	var input, state, result, clarification, available, created, updated string
	var lease sql.NullString
	err := row.Scan(&task.ID, &task.HouseholdID, &task.Kind, &task.RequestKey, &input, &task.Status, &task.Stage, &task.Attempts, &task.AttemptID, &task.Version, &state, &result, &clarification, &available, &task.LeaseToken, &lease, &task.LastErrorCode, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	task.InputJSON = json.RawMessage(input)
	task.StateJSON = json.RawMessage(state)
	task.ResultJSON = json.RawMessage(result)
	task.ClarificationJSON = json.RawMessage(clarification)
	if task.AvailableAt, err = ParseTime(available); err != nil {
		return nil, err
	}
	if task.CreatedAt, err = ParseTime(created); err != nil {
		return nil, err
	}
	if task.UpdatedAt, err = ParseTime(updated); err != nil {
		return nil, err
	}
	if lease.Valid {
		until, err := ParseTime(lease.String)
		if err != nil {
			return nil, err
		}
		task.LeaseExpiresAt = &until
	}
	return &task, nil
}

func archiveIdentifiers(values ...string) error {
	for _, value := range values {
		if err := domain.ValidateArchiveIdentifier(value); err != nil {
			return err
		}
	}
	return nil
}
func archiveTime(now time.Time) error {
	if now.IsZero() || now.UTC().Year() < 1970 || now.UTC().Year() > 9998 {
		return domain.Errorf(domain.CodeInvalidRequest, "invalid archive timestamp")
	}
	return nil
}
func archiveLeaseDuration(ttl time.Duration) error {
	if ttl <= 0 || ttl > domain.MaxArchiveLease {
		return domain.Errorf(domain.CodeInvalidRequest, "archive lease must be positive and at most ten minutes")
	}
	return nil
}
func archiveTaskIn(ctx context.Context, ex execer, household, id string) (*domain.ArchiveTask, error) {
	return scanArchiveTask(ex.QueryRowContext(ctx, `SELECT `+archiveTaskColumns+` FROM archive_tasks WHERE household_id=? AND id=?`, household, id))
}

// Enqueue creates a host-generated task identity, or returns the current task
// for an identical household/request key. InputJSON remains immutable even after
// clarification. Changed kind/input under the same key is an idempotency conflict.
func (r *ArchiveTasks) Enqueue(ctx context.Context, household, kind, requestKey string, input json.RawMessage, now time.Time) (*domain.ArchiveTask, bool, error) {
	if err := archiveIdentifiers(household, kind, requestKey); err != nil {
		return nil, false, err
	}
	if err := archiveTime(now); err != nil {
		return nil, false, err
	}
	normalized, err := domain.NormalizeArchiveObject(input)
	if err != nil {
		return nil, false, err
	}
	var out *domain.ArchiveTask
	replayed := false
	err = r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanArchiveTask(tx.QueryRowContext(ctx, `SELECT `+archiveTaskColumns+` FROM archive_tasks WHERE household_id=? AND request_key=?`, household, requestKey))
		if err == nil {
			if existing.Kind != kind || string(existing.InputJSON) != string(normalized) {
				return domain.Errorf(domain.CodeIdempotencyConflict, "archive request key already has different input")
			}
			out = existing
			replayed = true
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		out = &domain.ArchiveTask{ID: domain.NewID(), HouseholdID: household, Kind: kind, RequestKey: requestKey, InputJSON: normalized, Status: domain.ArchiveTaskQueued, Stage: "intake", Version: 1, StateJSON: json.RawMessage(`{}`), ResultJSON: json.RawMessage(`{}`), ClarificationJSON: json.RawMessage(`{}`), AvailableAt: now.UTC(), CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
		_, err = tx.ExecContext(ctx, `INSERT INTO archive_tasks (`+archiveTaskColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, out.ID, household, kind, requestKey, string(normalized), out.Status, out.Stage, 0, "", 1, "{}", "{}", "{}", FormatTime(now), "", nil, "", FormatTime(now), FormatTime(now))
		if err != nil {
			return err
		}
		return archiveEvent(ctx, tx, out, "enqueued")
	})
	if err != nil {
		return nil, false, err
	}
	return out, replayed, nil
}

func (r *ArchiveTasks) Get(ctx context.Context, household, id string) (*domain.ArchiveTask, error) {
	if err := archiveIdentifiers(household, id); err != nil {
		return nil, err
	}
	return archiveTaskIn(ctx, r.db.sql, household, id)
}

// Events returns transitions in ascending version order after the given version.
// Tokens are excluded from every event. Task existence is scoped to the household.
func (r *ArchiveTasks) Events(ctx context.Context, household, id string, afterVersion int64, limit int) ([]domain.ArchiveTaskEvent, error) {
	if err := archiveIdentifiers(household, id); err != nil {
		return nil, err
	}
	if afterVersion < 0 || limit < 1 || limit > 1000 {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "invalid archive event page")
	}
	if _, err := r.Get(ctx, household, id); err != nil {
		return nil, err
	}
	rows, err := r.db.sql.QueryContext(ctx, `SELECT event_json FROM archive_task_events WHERE household_id=? AND task_id=? AND version>? ORDER BY version LIMIT ?`, household, id, afterVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ArchiveTaskEvent{}
	for rows.Next() {
		var raw string
		var event domain.ArchiveTaskEvent
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

// Claim atomically selects due work in one household and issues a new secret
// token and independent attempt identity. Nil means no task is runnable. Expiry
// alone revokes worker authority, even before another worker reclaims the task.
func (r *ArchiveTasks) Claim(ctx context.Context, household string, now time.Time, ttl time.Duration) (*domain.ArchiveTask, error) {
	if err := archiveIdentifiers(household); err != nil {
		return nil, err
	}
	if err := archiveTime(now); err != nil {
		return nil, err
	}
	if err := archiveLeaseDuration(ttl); err != nil {
		return nil, err
	}
	var out *domain.ArchiveTask
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		task, err := scanArchiveTask(tx.QueryRowContext(ctx, `SELECT `+archiveTaskColumns+` FROM archive_tasks WHERE household_id=? AND ((status IN ('queued','retry_wait') AND available_at<=?) OR (status='running' AND lease_expires_at<=?)) ORDER BY available_at,created_at,id LIMIT 1`, household, FormatTime(now), FormatTime(now)))
		if errors.Is(err, domain.ErrNotFound) {
			out = nil
			return nil
		}
		if err != nil {
			return err
		}
		if now.Before(task.UpdatedAt) {
			return domain.Errorf(domain.CodeConflict, "archive clock precedes current task version")
		}
		task.Status = domain.ArchiveTaskRunning
		task.LeaseToken = rand.Text()
		task.AttemptID = domain.NewID()
		task.Attempts++
		until := now.Add(ttl).UTC()
		task.LeaseExpiresAt = &until
		if err := archiveSave(ctx, tx, task, "claimed", now); err != nil {
			return err
		}
		out = task
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// mutate holds the write lock while checking authority and saving a transition.
// No caller-supplied task snapshot can overwrite a newer checkpoint.
func (r *ArchiveTasks) mutate(ctx context.Context, household, id, token string, needsLease bool, kind string, now time.Time, change func(*domain.ArchiveTask) error) (*domain.ArchiveTask, error) {
	if err := archiveIdentifiers(household, id); err != nil {
		return nil, err
	}
	if err := archiveTime(now); err != nil {
		return nil, err
	}
	if needsLease {
		if err := archiveIdentifiers(token); err != nil {
			return nil, err
		}
	}
	var out *domain.ArchiveTask
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		task, err := archiveTaskIn(ctx, tx, household, id)
		if err != nil {
			return err
		}
		if now.Before(task.UpdatedAt) {
			return domain.Errorf(domain.CodeConflict, "archive clock precedes current task version")
		}
		if needsLease && (task.Status != domain.ArchiveTaskRunning || task.LeaseToken != token || task.LeaseExpiresAt == nil || !now.Before(*task.LeaseExpiresAt)) {
			return domain.Errorf(domain.CodeConflict, "archive lease is no longer current")
		}
		if !needsLease && task.Status == domain.ArchiveTaskCancelled {
			out = task
			return nil
		}
		if err := change(task); err != nil {
			return err
		}
		if err := archiveSave(ctx, tx, task, kind, now); err != nil {
			return err
		}
		out = task
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ArchiveTasks) Renew(ctx context.Context, household, id, token string, now time.Time, ttl time.Duration) (*domain.ArchiveTask, error) {
	if err := archiveLeaseDuration(ttl); err != nil {
		return nil, err
	}
	return r.mutate(ctx, household, id, token, true, "renewed", now, func(task *domain.ArchiveTask) error {
		until := now.Add(ttl).UTC()
		if until.Before(*task.LeaseExpiresAt) {
			return domain.Errorf(domain.CodeInvalidRequest, "renewal cannot shorten the archive lease")
		}
		task.LeaseExpiresAt = &until
		return nil
	})
}

func normalizeArchiveUpdate(update domain.ArchiveTaskUpdate) (domain.ArchiveTaskUpdate, error) {
	if err := archiveIdentifiers(update.Stage); err != nil {
		return update, err
	}
	if update.LastErrorCode != "" {
		if err := archiveIdentifiers(update.LastErrorCode); err != nil {
			return update, err
		}
	}
	if len(update.StateJSON) == 0 {
		update.StateJSON = json.RawMessage(`{}`)
	}
	if len(update.ResultJSON) == 0 {
		update.ResultJSON = json.RawMessage(`{}`)
	}
	var err error
	if update.StateJSON, err = domain.NormalizeArchiveObject(update.StateJSON); err != nil {
		return update, err
	}
	update.ResultJSON, err = domain.NormalizeArchiveObject(update.ResultJSON)
	return update, err
}
func applyArchiveUpdate(task *domain.ArchiveTask, update domain.ArchiveTaskUpdate, status domain.ArchiveTaskStatus) {
	task.Stage = update.Stage
	task.StateJSON = update.StateJSON
	task.ResultJSON = update.ResultJSON
	task.LastErrorCode = update.LastErrorCode
	task.Status = status
	if status != domain.ArchiveTaskRunning {
		task.LeaseToken = ""
		task.LeaseExpiresAt = nil
	}
}

// Checkpoint persists completed stage references without giving up the lease.
// It does not publish facts or make external side effects exactly-once; those
// services must use the task/checkpoint identities for their own idempotency.
func (r *ArchiveTasks) Checkpoint(ctx context.Context, household, id, token string, update domain.ArchiveTaskUpdate, now time.Time) (*domain.ArchiveTask, error) {
	update, err := normalizeArchiveUpdate(update)
	if err != nil {
		return nil, err
	}
	return r.mutate(ctx, household, id, token, true, "checkpointed", now, func(task *domain.ArchiveTask) error {
		applyArchiveUpdate(task, update, domain.ArchiveTaskRunning)
		return nil
	})
}

// Finish atomically publishes the final result and event, rejecting stale leases.
func (r *ArchiveTasks) Finish(ctx context.Context, household, id, token string, status domain.ArchiveTaskStatus, update domain.ArchiveTaskUpdate, now time.Time) (*domain.ArchiveTask, error) {
	if status != domain.ArchiveTaskSucceeded && status != domain.ArchiveTaskFailed {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "archive finish requires succeeded or failed")
	}
	update, err := normalizeArchiveUpdate(update)
	if err != nil {
		return nil, err
	}
	return r.mutate(ctx, household, id, token, true, "finished", now, func(task *domain.ArchiveTask) error { applyArchiveUpdate(task, update, status); return nil })
}

func (r *ArchiveTasks) Retry(ctx context.Context, household, id, token string, update domain.ArchiveTaskUpdate, availableAt, now time.Time) (*domain.ArchiveTask, error) {
	if err := archiveTime(availableAt); err != nil {
		return nil, err
	}
	if availableAt.Before(now) || availableAt.Sub(now) > domain.MaxArchiveRetryDelay {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "archive retry must be due within thirty days")
	}
	update, err := normalizeArchiveUpdate(update)
	if err != nil {
		return nil, err
	}
	return r.mutate(ctx, household, id, token, true, "retry_scheduled", now, func(task *domain.ArchiveTask) error {
		applyArchiveUpdate(task, update, domain.ArchiveTaskRetryWait)
		task.AvailableAt = availableAt.UTC()
		return nil
	})
}

func (r *ArchiveTasks) Wait(ctx context.Context, household, id, token string, update domain.ArchiveTaskUpdate, now time.Time) (*domain.ArchiveTask, error) {
	update, err := normalizeArchiveUpdate(update)
	if err != nil {
		return nil, err
	}
	return r.mutate(ctx, household, id, token, true, "waiting_input", now, func(task *domain.ArchiveTask) error {
		applyArchiveUpdate(task, update, domain.ArchiveTaskWaitingInput)
		return nil
	})
}

// Cancel revokes a lease immediately. Repeated cancellation is idempotent;
// succeeded/failed tasks cannot be changed into cancelled tasks.
func (r *ArchiveTasks) Cancel(ctx context.Context, household, id string, now time.Time) (*domain.ArchiveTask, error) {
	return r.mutate(ctx, household, id, "", false, "cancelled", now, func(task *domain.ArchiveTask) error {
		if task.Status == domain.ArchiveTaskSucceeded || task.Status == domain.ArchiveTaskFailed {
			return domain.Errorf(domain.CodeConflict, "archive task is already terminal")
		}
		task.Status = domain.ArchiveTaskCancelled
		task.LeaseToken = ""
		task.LeaseExpiresAt = nil
		return nil
	})
}

// ResolveWaiting resumes the same task with a separate clarification object.
// A key binds the expected waiting version and canonical input. Exact replays
// return the current task (even after further work), creating no extra event.
func (r *ArchiveTasks) ResolveWaiting(ctx context.Context, household, id string, expectedVersion int64, resolutionKey string, input json.RawMessage, now time.Time) (*domain.ArchiveTask, bool, error) {
	if err := archiveIdentifiers(household, id, resolutionKey); err != nil {
		return nil, false, err
	}
	if err := archiveTime(now); err != nil {
		return nil, false, err
	}
	if expectedVersion < 1 {
		return nil, false, domain.Errorf(domain.CodeInvalidRequest, "invalid archive resolution version")
	}
	normalized, err := domain.NormalizeArchiveObject(input)
	if err != nil {
		return nil, false, err
	}
	var out *domain.ArchiveTask
	replayed := false
	err = r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		task, err := archiveTaskIn(ctx, tx, household, id)
		if err != nil {
			return err
		}
		var previousVersion int64
		var previousInput string
		err = tx.QueryRowContext(ctx, `SELECT expected_version,input_json FROM archive_task_resolutions WHERE household_id=? AND task_id=? AND resolution_key=?`, household, id, resolutionKey).Scan(&previousVersion, &previousInput)
		if err == nil {
			if previousVersion != expectedVersion || previousInput != string(normalized) {
				return domain.Errorf(domain.CodeIdempotencyConflict, "archive resolution key already has different input or version")
			}
			out = task
			replayed = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if task.Status != domain.ArchiveTaskWaitingInput || task.Version != expectedVersion || now.Before(task.UpdatedAt) {
			return domain.Errorf(domain.CodeConflict, "archive task is not waiting at the expected version")
		}
		task.Status = domain.ArchiveTaskQueued
		task.ClarificationJSON = normalized
		task.AvailableAt = now.UTC()
		if err := archiveSave(ctx, tx, task, "resolved", now); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO archive_task_resolutions(household_id,task_id,resolution_key,expected_version,input_json,resolved_version) VALUES(?,?,?,?,?,?)`, household, id, resolutionKey, expectedVersion, string(normalized), task.Version)
		if err != nil {
			return err
		}
		out = task
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, replayed, nil
}

func archiveSave(ctx context.Context, tx *sql.Tx, task *domain.ArchiveTask, kind string, now time.Time) error {
	previousVersion := task.Version
	task.Version++
	task.UpdatedAt = now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE archive_tasks SET status=?,stage=?,attempts=?,attempt_id=?,version=?,state_json=?,result_json=?,clarification_json=?,available_at=?,lease_token=?,lease_expires_at=?,last_error_code=?,updated_at=? WHERE household_id=? AND id=? AND version=?`, task.Status, task.Stage, task.Attempts, task.AttemptID, task.Version, string(task.StateJSON), string(task.ResultJSON), string(task.ClarificationJSON), FormatTime(task.AvailableAt), task.LeaseToken, FormatTimePtr(task.LeaseExpiresAt), task.LastErrorCode, FormatTime(now), task.HouseholdID, task.ID, previousVersion)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return domain.Errorf(domain.CodeConflict, "archive task version changed")
	}
	return archiveEvent(ctx, tx, task, kind)
}

func archiveEvent(ctx context.Context, tx *sql.Tx, task *domain.ArchiveTask, kind string) error {
	event := domain.ArchiveTaskEvent{HouseholdID: task.HouseholdID, TaskID: task.ID, Version: task.Version, Kind: kind, Status: task.Status, Stage: task.Stage, Attempts: task.Attempts, AttemptID: task.AttemptID, InputJSON: task.InputJSON, StateJSON: task.StateJSON, ResultJSON: task.ResultJSON, ClarificationJSON: task.ClarificationJSON, LastErrorCode: task.LastErrorCode, AvailableAt: task.AvailableAt, LeaseExpiresAt: task.LeaseExpiresAt, CreatedAt: task.UpdatedAt}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO archive_task_events(household_id,task_id,version,kind,event_json,created_at) VALUES(?,?,?,?,?,?)`, task.HouseholdID, task.ID, task.Version, kind, string(raw), FormatTime(task.UpdatedAt))
	return err
}
