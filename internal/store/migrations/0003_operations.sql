CREATE TABLE integration_operations (
 principal_id TEXT NOT NULL REFERENCES integration_principals(id),
 operation_id TEXT NOT NULL,
 payload_hash TEXT NOT NULL,
 command_id TEXT NOT NULL UNIQUE REFERENCES screen_commands(id) ON DELETE CASCADE,
 created_at TEXT NOT NULL,
 PRIMARY KEY (principal_id, operation_id)
);
