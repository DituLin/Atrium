CREATE TABLE integration_principals (
 id TEXT PRIMARY KEY, label TEXT NOT NULL, enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 revoked_at TEXT, expires_at TEXT NOT NULL, policy_json TEXT NOT NULL,
 policy_version INTEGER NOT NULL CHECK(policy_version > 0), created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE integration_credentials (
 id TEXT PRIMARY KEY, principal_id TEXT NOT NULL REFERENCES integration_principals(id),
 token_hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT
);
CREATE UNIQUE INDEX integration_current_credential ON integration_credentials(principal_id) WHERE revoked_at IS NULL;
