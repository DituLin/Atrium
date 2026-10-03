CREATE TABLE family_members (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE family_records (
 id TEXT PRIMARY KEY,
 member_id TEXT NOT NULL REFERENCES family_members(id),
 principal_id TEXT NOT NULL,
 request_id TEXT NOT NULL,
 payload_json TEXT NOT NULL,
 occurred_at TEXT NOT NULL,
 display_on_tv INTEGER NOT NULL CHECK(display_on_tv IN (0,1)),
 created_at TEXT NOT NULL,
 UNIQUE(principal_id, request_id)
);
CREATE INDEX family_records_member_time ON family_records(member_id, occurred_at DESC, id DESC);
CREATE TABLE family_record_exports (
 record_id TEXT PRIMARY KEY REFERENCES family_records(id),
 exported_at TEXT
);
