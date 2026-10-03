-- Stable random identity is independent of home display configuration.
CREATE TABLE households (id TEXT PRIMARY KEY NOT NULL);
CREATE TABLE local_household (
 singleton INTEGER PRIMARY KEY CHECK (singleton=1),
 household_id TEXT NOT NULL REFERENCES households(id)
);
INSERT INTO households(id) VALUES(lower(hex(randomblob(16))));
INSERT INTO local_household(singleton,household_id) SELECT 1,id FROM households;

-- Rebuild only the unreleased task tables to scope request keys to the origin.
-- Legacy rows remain explicitly unbound and cannot be claimed by any worker.
CREATE TEMP TABLE archive_tasks_old AS SELECT * FROM archive_tasks;
CREATE TEMP TABLE archive_events_old AS SELECT * FROM archive_task_events;
CREATE TEMP TABLE archive_resolutions_old AS SELECT * FROM archive_task_resolutions;
DROP TABLE archive_task_resolutions;
DROP TABLE archive_task_events;
DROP TABLE archive_tasks;
-- Durable household-scoped work. Input/request identity never changes; state,
-- result and clarification checkpoints commit with a versioned event.
CREATE TABLE archive_tasks (
 id TEXT NOT NULL,
 household_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 request_key TEXT NOT NULL,
 input_json TEXT NOT NULL CHECK (json_valid(input_json) AND json_type(input_json)='object'),
 status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','waiting_input','retry_wait','failed','cancelled')),
 stage TEXT NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
 attempt_id TEXT NOT NULL DEFAULT '',
 version INTEGER NOT NULL CHECK (version >= 1),
 state_version INTEGER NOT NULL CHECK (state_version >= 1),
 state_json TEXT NOT NULL CHECK (json_valid(state_json) AND json_type(state_json)='object'),
 result_json TEXT NOT NULL CHECK (json_valid(result_json) AND json_type(result_json)='object'),
 clarification_json TEXT NOT NULL CHECK (json_valid(clarification_json) AND json_type(clarification_json)='object'),
 available_at TEXT NOT NULL,
 lease_token TEXT NOT NULL DEFAULT '',
 lease_expires_at TEXT,
 last_error_code TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 principal_id TEXT REFERENCES integration_principals(id),
 policy_version INTEGER,
 CHECK ((principal_id IS NULL AND policy_version IS NULL) OR (principal_id IS NOT NULL AND policy_version IS NOT NULL AND policy_version > 0)),
 PRIMARY KEY (household_id,id),
 UNIQUE (household_id,principal_id,request_key),
 CHECK ((status='running' AND lease_token<>'' AND lease_expires_at IS NOT NULL AND attempt_id<>'')
     OR (status<>'running' AND lease_token='' AND lease_expires_at IS NULL))
);
CREATE INDEX archive_tasks_runnable ON archive_tasks(household_id,status,available_at,created_at,id);
CREATE INDEX archive_tasks_expired ON archive_tasks(household_id,status,lease_expires_at);

CREATE TABLE archive_task_events (
 household_id TEXT NOT NULL,
 task_id TEXT NOT NULL,
 version INTEGER NOT NULL,
 kind TEXT NOT NULL,
 event_json TEXT NOT NULL CHECK (json_valid(event_json) AND json_type(event_json)='object'),
 created_at TEXT NOT NULL,
 PRIMARY KEY (household_id,task_id,version),
 FOREIGN KEY (household_id,task_id) REFERENCES archive_tasks(household_id,id)
);

CREATE TABLE archive_task_resolutions (
 household_id TEXT NOT NULL,
 task_id TEXT NOT NULL,
 resolution_key TEXT NOT NULL,
 expected_version INTEGER NOT NULL,
 input_json TEXT NOT NULL CHECK (json_valid(input_json) AND json_type(input_json)='object'),
 resolved_version INTEGER NOT NULL,
 PRIMARY KEY (household_id,task_id,resolution_key),
 FOREIGN KEY (household_id,task_id,resolved_version) REFERENCES archive_task_events(household_id,task_id,version)
);

INSERT INTO archive_tasks SELECT *,NULL,NULL FROM archive_tasks_old;
INSERT INTO archive_task_events SELECT * FROM archive_events_old;
INSERT INTO archive_task_resolutions SELECT * FROM archive_resolutions_old;
DROP TABLE archive_resolutions_old;
DROP TABLE archive_events_old;
DROP TABLE archive_tasks_old;

CREATE TRIGGER archive_task_identity_immutable BEFORE UPDATE ON archive_tasks
WHEN NEW.household_id IS NOT OLD.household_id OR NEW.id IS NOT OLD.id
 OR NEW.principal_id IS NOT OLD.principal_id OR NEW.policy_version IS NOT OLD.policy_version
 OR NEW.kind IS NOT OLD.kind OR NEW.request_key IS NOT OLD.request_key OR NEW.input_json IS NOT OLD.input_json
BEGIN SELECT RAISE(ABORT,'archive task identity is immutable'); END;
CREATE TRIGGER archive_task_bound_household BEFORE INSERT ON archive_tasks
WHEN NEW.principal_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM households WHERE id=NEW.household_id)
BEGIN SELECT RAISE(ABORT,'unknown archive household'); END;
