-- Latest normalized, explicitly whitelisted observations only. No provider payloads.
CREATE TABLE iot_observations (
 instance_id TEXT NOT NULL,
 device_id TEXT NOT NULL,
 capability_id TEXT NOT NULL,
 observation_json TEXT NOT NULL,
 sync_at TEXT NOT NULL DEFAULT '',
 binding_hash TEXT NOT NULL,
 PRIMARY KEY (instance_id, device_id, capability_id)
);
