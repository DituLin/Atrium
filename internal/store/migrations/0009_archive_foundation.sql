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
 state_json TEXT NOT NULL CHECK (json_valid(state_json) AND json_type(state_json)='object'),
 result_json TEXT NOT NULL CHECK (json_valid(result_json) AND json_type(result_json)='object'),
 clarification_json TEXT NOT NULL CHECK (json_valid(clarification_json) AND json_type(clarification_json)='object'),
 available_at TEXT NOT NULL,
 lease_token TEXT NOT NULL DEFAULT '',
 lease_expires_at TEXT,
 last_error_code TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY (household_id,id),
 UNIQUE (household_id,request_key),
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
