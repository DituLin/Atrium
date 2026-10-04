package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/DituLin/Atrium/internal/clock"
	"github.com/DituLin/Atrium/internal/domain"
)

// ArchiveTasks owns durable tasks, fenced worker leases and atomic checkpoints.
// Client actors come from authenticated host context; worker scope is household-bound.
// This repository does not execute JSON payloads or grant source-file access.
type ArchiveTasks struct {
	db    *DB
	clock clock.Clock
}

func (d *DB) ArchiveTasks() *ArchiveTasks { return d.ArchiveTasksWithClock(clock.SystemClock{}) }

// ArchiveTasksWithClock installs a trusted host clock, not a timestamp supplied
// by a worker or request payload. The clock must support concurrent Now calls.
// Each write samples it after acquiring the transaction lock, including retries.
func (d *DB) ArchiveTasksWithClock(c clock.Clock) *ArchiveTasks {
	if c == nil {
		c = clock.SystemClock{}
	}
	return &ArchiveTasks{db: d, clock: c}
}

func (r *ArchiveTasks) currentTime() (time.Time, error) {
	now := r.clock.Now().UTC()
	return now, archiveTime(now)
}

const archiveTaskColumns = `id,household_id,kind,request_key,input_json,status,stage,attempts,attempt_id,version,state_version,state_json,result_json,clarification_json,available_at,lease_token,lease_expires_at,last_error_code,created_at,updated_at,principal_id,policy_version`

func scanArchiveTask(row interface{ Scan(...any) error }) (*domain.ArchiveTask, error) {
	var task domain.ArchiveTask
	var input, state, result, clarification, available, created, updated string
	var lease, principal sql.NullString
	var policyVersion sql.NullInt64
	err := row.Scan(&task.ID, &task.HouseholdID, &task.Kind, &task.RequestKey, &input, &task.Status, &task.Stage, &task.Attempts, &task.AttemptID, &task.Version, &task.StateVersion, &state, &result, &clarification, &available, &task.LeaseToken, &lease, &task.LastErrorCode, &created, &updated, &principal, &policyVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	task.PrincipalID = principal.String
	task.PolicyVersion = policyVersion.Int64
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
// for an identical household/principal/request key. InputJSON remains immutable
// after clarification. Changed kind/input is an idempotency conflict. An actor
// without archive.read receives only receipt fields, including on replay.
func (r *ArchiveTasks) Enqueue(ctx context.Context, actor domain.IntegrationActor, household, kind, requestKey string, input json.RawMessage) (*domain.ArchiveTask, bool, error) {
	var out *domain.ArchiveTask
	var replayed bool
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		out, replayed, err = r.enqueueInTx(ctx, tx, actor, household, kind, requestKey, input, now)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return out, replayed, nil
}

// enqueueInTx allows later source/task acceptance to share this authorization
// and commit boundary. Only trusted store code provides the lock-time sample.
func (r *ArchiveTasks) enqueueInTx(ctx context.Context, tx *sql.Tx, actor domain.IntegrationActor, household, kind, requestKey string, input json.RawMessage, now time.Time) (*domain.ArchiveTask, bool, error) {
	if err := archiveIdentifiers(household, kind, requestKey); err != nil {
		return nil, false, err
	}
	normalized, err := domain.NormalizeArchiveObject(input)
	if err != nil {
		return nil, false, err
	}
	p, err := r.authorizeArchiveClient(ctx, tx, actor, household, "archive.ingest", now)
	if err != nil {
		return nil, false, err
	}
	existing, err := scanArchiveTask(tx.QueryRowContext(ctx, `SELECT `+archiveTaskColumns+` FROM archive_tasks WHERE household_id=? AND principal_id=? AND request_key=?`, household, p.ID, requestKey))
	if err == nil {
		if existing.Kind != kind || string(existing.InputJSON) != string(normalized) {
			return nil, false, domain.Errorf(domain.CodeIdempotencyConflict, "archive request key already has different input")
		}
		existing.DelegationErrorCode, err = r.archiveDelegationCode(ctx, tx, existing, now)
		if err != nil {
			return nil, false, err
		}
		return archiveClientResult(existing, p), true, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, false, err
	}
	out := &domain.ArchiveTask{ID: domain.NewID(), HouseholdID: household, PrincipalID: p.ID, PolicyVersion: p.PolicyVersion, Kind: kind, RequestKey: requestKey, InputJSON: normalized, Status: domain.ArchiveTaskQueued, Stage: "intake", Version: 1, StateVersion: 1, StateJSON: json.RawMessage(`{}`), ResultJSON: json.RawMessage(`{}`), ClarificationJSON: json.RawMessage(`{}`), AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO archive_tasks (`+archiveTaskColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, out.ID, household, kind, requestKey, string(normalized), out.Status, out.Stage, 0, "", 1, 1, "{}", "{}", "{}", FormatTime(now), "", nil, "", FormatTime(now), FormatTime(now), p.ID, p.PolicyVersion)
	if err != nil {
		return nil, false, err
	}
	if err = archiveEvent(ctx, tx, out, "enqueued", actor.PrincipalID); err != nil {
		return nil, false, err
	}
	return archiveClientResult(out, p), false, nil
}

func (r *ArchiveTasks) Get(ctx context.Context, actor domain.IntegrationActor, household, id string) (*domain.ArchiveTask, error) {
	if err := archiveIdentifiers(household, id); err != nil {
		return nil, err
	}
	var out *domain.ArchiveTask
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		if _, err = r.authorizeArchiveClient(ctx, tx, actor, household, "archive.read", now); err != nil {
			return err
		}
		out, err = archiveTaskIn(ctx, tx, household, id)
		if err != nil {
			return err
		}
		out.DelegationErrorCode, err = r.archiveDelegationCode(ctx, tx, out, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Events authorizes and reads one household-qualified task in a single snapshot.
// BEGIN IMMEDIATE also serializes these small reads with authorization changes.
func (r *ArchiveTasks) Events(ctx context.Context, actor domain.IntegrationActor, household, id string, afterVersion int64, limit int) ([]domain.ArchiveTaskEvent, error) {
	if err := archiveIdentifiers(household, id); err != nil {
		return nil, err
	}
	if afterVersion < 0 || limit < 1 || limit > 1000 {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "invalid archive event page")
	}
	var out []domain.ArchiveTaskEvent
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		if _, err = r.authorizeArchiveClient(ctx, tx, actor, household, "archive.read", now); err != nil {
			return err
		}
		if _, err = archiveTaskIn(ctx, tx, household, id); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT event_json FROM archive_task_events WHERE household_id=? AND task_id=? AND version>? ORDER BY version LIMIT ?`, household, id, afterVersion, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = []domain.ArchiveTaskEvent{}
		for rows.Next() {
			var raw string
			var event domain.ArchiveTaskEvent
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(raw), &event); err != nil {
				return err
			}
			out = append(out, event)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Claim atomically selects due work in one household and issues a new secret
// token and independent attempt identity. SQL filters invalid delegations before
// LIMIT, so old/unbound work cannot block authorized work. Such tasks retain their
// history; Get reports the current delegation_error_code. Nil means no authorized
// task is runnable. Expiry
// alone revokes worker authority, even before another worker reclaims the task.
func (r *ArchiveTasks) Claim(ctx context.Context, household string, ttl time.Duration) (*domain.ArchiveTask, error) {
	return r.claimKinds(ctx, household, ttl, nil)
}

// ClaimKinds only leases explicitly supported work; an empty set claims nothing.
func (r *ArchiveTasks) ClaimKinds(ctx context.Context, household string, ttl time.Duration, kinds []string) (*domain.ArchiveTask, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	if len(kinds) > 16 {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "invalid archive pipeline request")
	}
	for _, k := range kinds {
		if e := archiveIdentifiers(k); e != nil {
			return nil, e
		}
	}
	return r.claimKinds(ctx, household, ttl, kinds)
}
func (r *ArchiveTasks) claimKinds(ctx context.Context, household string, ttl time.Duration, kinds []string) (*domain.ArchiveTask, error) {
	if err := archiveIdentifiers(household); err != nil {
		return nil, err
	}
	if err := archiveLeaseDuration(ttl); err != nil {
		return nil, err
	}
	var out *domain.ArchiveTask
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		task, err := scanArchiveTask(tx.QueryRowContext(ctx, `SELECT `+archiveTaskColumns+` FROM archive_tasks
 WHERE household_id=? AND (?='null' OR kind IN (SELECT value FROM json_each(?))) AND EXISTS (
  SELECT 1 FROM integration_principals p
  WHERE p.id=archive_tasks.principal_id AND p.policy_version=archive_tasks.policy_version
   AND p.enabled=1 AND p.revoked_at IS NULL AND p.expires_at>?
   AND EXISTS (SELECT 1 FROM json_each(p.policy_json,'$.permissions') WHERE value='archive.ingest')
   AND EXISTS (SELECT 1 FROM json_each(p.policy_json,'$.household_ids') WHERE value=archive_tasks.household_id)
 ) AND ((status IN ('queued','retry_wait') AND available_at<=?)
     OR (status='running' AND lease_expires_at<=?))
 ORDER BY available_at,created_at,id LIMIT 1`, household, entityJSON(kinds), entityJSON(kinds), FormatTime(now), FormatTime(now), FormatTime(now)))
		if errors.Is(err, domain.ErrNotFound) {
			out = nil
			return nil
		}
		if err != nil {
			return err
		}
		if err := r.authorizeArchiveDelegate(ctx, tx, task, now); err != nil {
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
		if err := archiveSave(ctx, tx, task, "claimed", now, ""); err != nil {
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
func (r *ArchiveTasks) mutate(ctx context.Context, actor *domain.IntegrationActor, household, id, token string, needsLease bool, kind string, change func(*domain.ArchiveTask, time.Time) error) (*domain.ArchiveTask, error) {
	var out *domain.ArchiveTask
	err := r.db.InWriteTx(ctx, func(tx *sql.Tx) error {
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		out, err = r.mutateInTx(ctx, tx, actor, household, id, token, needsLease, kind, now, change)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (r *ArchiveTasks) mutateInTx(ctx context.Context, tx *sql.Tx, actor *domain.IntegrationActor, household, id, token string, needsLease bool, kind string, now time.Time, change func(*domain.ArchiveTask, time.Time) error) (*domain.ArchiveTask, error) {
	if err := archiveIdentifiers(household, id); err != nil {
		return nil, err
	}
	if needsLease {
		if err := archiveIdentifiers(token); err != nil {
			return nil, err
		}
	}
	var principal *domain.IntegrationPrincipal
	var err error
	if actor != nil {
		principal, err = r.authorizeArchiveClient(ctx, tx, *actor, household, "", now)
		if err != nil {
			return nil, err
		}
	}
	task, err := archiveTaskIn(ctx, tx, household, id)
	if err != nil {
		return nil, err
	}
	actorID := ""
	if actor != nil {
		if err := archiveTaskClientPermission(principal, task, "archive.ingest"); err != nil {
			return nil, err
		}
		actorID = actor.PrincipalID
	}
	if now.Before(task.UpdatedAt) {
		return nil, domain.Errorf(domain.CodeConflict, "archive clock precedes current task version")
	}
	if needsLease && (task.Status != domain.ArchiveTaskRunning || task.LeaseToken != token || task.LeaseExpiresAt == nil || !now.Before(*task.LeaseExpiresAt)) {
		return nil, domain.Errorf(domain.CodeConflict, "archive lease is no longer current")
	}
	if needsLease {
		if err := r.authorizeArchiveDelegate(ctx, tx, task, now); err != nil {
			return nil, err
		}
	}
	if !needsLease && task.Status == domain.ArchiveTaskCancelled {
		return archiveClientResult(task, principal), nil
	}
	if err := change(task, now); err != nil {
		return nil, err
	}
	if err := archiveSave(ctx, tx, task, kind, now, actorID); err != nil {
		return nil, err
	}
	if actor != nil {
		return archiveClientResult(task, principal), nil
	}
	return task, nil
}

func (r *ArchiveTasks) Renew(ctx context.Context, household, id, token string, ttl time.Duration) (*domain.ArchiveTask, error) {
	if err := archiveLeaseDuration(ttl); err != nil {
		return nil, err
	}
	return r.mutate(ctx, nil, household, id, token, true, "renewed", func(task *domain.ArchiveTask, now time.Time) error {
		until := now.Add(ttl).UTC()
		if until.Before(*task.LeaseExpiresAt) {
			return domain.Errorf(domain.CodeInvalidRequest, "renewal cannot shorten the archive lease")
		}
		task.LeaseExpiresAt = &until
		return nil
	})
}

func normalizeArchiveUpdate(update domain.ArchiveTaskUpdate) (domain.ArchiveTaskUpdate, error) {
	if update.ExpectedStateVersion < 1 {
		return update, domain.Errorf(domain.CodeInvalidRequest, "archive update requires a checkpoint state version")
	}
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
func applyArchiveUpdate(task *domain.ArchiveTask, update domain.ArchiveTaskUpdate, status domain.ArchiveTaskStatus) error {
	if task.StateVersion != update.ExpectedStateVersion {
		return domain.Errorf(domain.CodeConflict, "archive checkpoint state version changed")
	}
	task.StateVersion++
	task.Stage = update.Stage
	task.StateJSON = update.StateJSON
	task.ResultJSON = update.ResultJSON
	task.LastErrorCode = update.LastErrorCode
	task.Status = status
	if status != domain.ArchiveTaskRunning {
		task.LeaseToken = ""
		task.LeaseExpiresAt = nil
	}
	return nil
}

// Checkpoint persists completed stage references without giving up the lease.
// It does not publish facts or make external side effects exactly-once; those
// services must use the task/checkpoint identities for their own idempotency.
func (r *ArchiveTasks) Checkpoint(ctx context.Context, household, id, token string, update domain.ArchiveTaskUpdate) (*domain.ArchiveTask, error) {
	update, err := normalizeArchiveUpdate(update)
	if err != nil {
		return nil, err
	}
	return r.mutate(ctx, nil, household, id, token, true, "checkpointed", func(task *domain.ArchiveTask, now time.Time) error {
		return applyArchiveUpdate(task, update, domain.ArchiveTaskRunning)
	})
}

// Finish atomically publishes the final result and event, rejecting stale leases.
func (r *ArchiveTasks) Finish(ctx context.Context, household, id, token string, status domain.ArchiveTaskStatus, update domain.ArchiveTaskUpdate) (*domain.ArchiveTask, error) {
	if status != domain.ArchiveTaskSucceeded && status != domain.ArchiveTaskFailed {
		return nil, domain.Errorf(domain.CodeInvalidRequest, "archive finish requires succeeded or failed")
	}
	update, err := normalizeArchiveUpdate(update)
	if err != nil {
		return nil, err
	}
	return r.mutate(ctx, nil, household, id, token, true, "finished", func(task *domain.ArchiveTask, now time.Time) error { return applyArchiveUpdate(task, update, status) })
}

func (r *ArchiveTasks) Retry(ctx context.Context, household, id, token string, update domain.ArchiveTaskUpdate, availableAt time.Time) (*domain.ArchiveTask, error) {
	if err := archiveTime(availableAt); err != nil {
		return nil, err
	}
	update, err := normalizeArchiveUpdate(update)
	if err != nil {
		return nil, err
	}
	return r.mutate(ctx, nil, household, id, token, true, "retry_scheduled", func(task *domain.ArchiveTask, now time.Time) error {
		if availableAt.Before(now) || availableAt.Sub(now) > domain.MaxArchiveRetryDelay {
			return domain.Errorf(domain.CodeInvalidRequest, "archive retry must be due within thirty days")
		}
		if err := applyArchiveUpdate(task, update, domain.ArchiveTaskRetryWait); err != nil {
			return err
		}
		task.AvailableAt = availableAt.UTC()
		return nil
	})
}

func (r *ArchiveTasks) Wait(ctx context.Context, household, id, token string, update domain.ArchiveTaskUpdate) (*domain.ArchiveTask, error) {
	update, err := normalizeArchiveUpdate(update)
	if err != nil {
		return nil, err
	}
	return r.mutate(ctx, nil, household, id, token, true, "waiting_input", func(task *domain.ArchiveTask, now time.Time) error {
		return applyArchiveUpdate(task, update, domain.ArchiveTaskWaitingInput)
	})
}

// Cancel revokes a lease immediately. Repeated cancellation is idempotent;
// succeeded/failed tasks cannot be changed into cancelled tasks. Current owner
// ingest authority or household read+organize authority can cancel even when the
// original delegation is no longer valid; cancellation never refreshes it.
func (r *ArchiveTasks) Cancel(ctx context.Context, actor domain.IntegrationActor, household, id string) (*domain.ArchiveTask, error) {
	return r.mutate(ctx, &actor, household, id, "", false, "cancelled", func(task *domain.ArchiveTask, now time.Time) error {
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
// The owner needs organize; another household integration needs read+organize.
// Both current requester authority and original delegation are checked on replay.
func (r *ArchiveTasks) ResolveWaiting(ctx context.Context, actor domain.IntegrationActor, household, id string, expectedVersion int64, resolutionKey string, input json.RawMessage) (*domain.ArchiveTask, bool, error) {
	if err := archiveIdentifiers(household, id, resolutionKey); err != nil {
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
		now, err := r.currentTime()
		if err != nil {
			return err
		}
		principal, err := r.authorizeArchiveClient(ctx, tx, actor, household, "", now)
		if err != nil {
			return err
		}
		task, err := archiveTaskIn(ctx, tx, household, id)
		if err != nil {
			return err
		}
		var businessQuestions int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_questions WHERE household_id=? AND task_id=?`, household, id).Scan(&businessQuestions); err != nil {
			return err
		}
		if businessQuestions > 0 {
			return domain.ErrConflict
		}
		if err := archiveTaskClientPermission(principal, task, "archive.organize"); err != nil {
			return err
		}
		if err := r.authorizeArchiveDelegate(ctx, tx, task, now); err != nil {
			return err
		}
		var previousVersion int64
		var previousInput string
		err = tx.QueryRowContext(ctx, `SELECT expected_version,input_json FROM archive_task_resolutions WHERE household_id=? AND task_id=? AND resolution_key=?`, household, id, resolutionKey).Scan(&previousVersion, &previousInput)
		if err == nil {
			if previousVersion != expectedVersion || previousInput != string(normalized) {
				return domain.Errorf(domain.CodeIdempotencyConflict, "archive resolution key already has different input or version")
			}
			out = archiveClientResult(task, principal)
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
		task.StateVersion++
		task.AvailableAt = now.UTC()
		if err := archiveSave(ctx, tx, task, "resolved", now, actor.PrincipalID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO archive_task_resolutions(household_id,task_id,resolution_key,expected_version,input_json,resolved_version) VALUES(?,?,?,?,?,?)`, household, id, resolutionKey, expectedVersion, string(normalized), task.Version)
		if err != nil {
			return err
		}
		out = archiveClientResult(task, principal)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, replayed, nil
}

func archiveSave(ctx context.Context, tx *sql.Tx, task *domain.ArchiveTask, kind string, now time.Time, actorID string) error {
	previousVersion := task.Version
	task.Version++
	task.UpdatedAt = now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE archive_tasks SET status=?,stage=?,attempts=?,attempt_id=?,version=?,state_version=?,state_json=?,result_json=?,clarification_json=?,available_at=?,lease_token=?,lease_expires_at=?,last_error_code=?,updated_at=? WHERE household_id=? AND id=? AND version=?`, task.Status, task.Stage, task.Attempts, task.AttemptID, task.Version, task.StateVersion, string(task.StateJSON), string(task.ResultJSON), string(task.ClarificationJSON), FormatTime(task.AvailableAt), task.LeaseToken, FormatTimePtr(task.LeaseExpiresAt), task.LastErrorCode, FormatTime(now), task.HouseholdID, task.ID, previousVersion)
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
	return archiveEvent(ctx, tx, task, kind, actorID)
}

func archiveEvent(ctx context.Context, tx *sql.Tx, task *domain.ArchiveTask, kind, actorID string) error {
	event := domain.ArchiveTaskEvent{PrincipalID: task.PrincipalID, PolicyVersion: task.PolicyVersion, ActorPrincipalID: actorID, HouseholdID: task.HouseholdID, TaskID: task.ID, Version: task.Version, StateVersion: task.StateVersion, Kind: kind, Status: task.Status, Stage: task.Stage, Attempts: task.Attempts, AttemptID: task.AttemptID, InputJSON: task.InputJSON, StateJSON: task.StateJSON, ResultJSON: task.ResultJSON, ClarificationJSON: task.ClarificationJSON, LastErrorCode: task.LastErrorCode, AvailableAt: task.AvailableAt, LeaseExpiresAt: task.LeaseExpiresAt, CreatedAt: task.UpdatedAt}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO archive_task_events(household_id,task_id,version,kind,event_json,created_at) VALUES(?,?,?,?,?,?)`, task.HouseholdID, task.ID, task.Version, kind, string(raw), FormatTime(task.UpdatedAt))
	return err
}
