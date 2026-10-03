ALTER TABLE archive_documents ADD COLUMN visibility_version INTEGER NOT NULL DEFAULT 1 CHECK (visibility_version >= 1);

CREATE TABLE archive_sources (
 household_id TEXT NOT NULL,
 source_key TEXT NOT NULL CHECK(typeof(source_key)='text' AND length(source_key)=64 AND length(CAST(source_key AS BLOB))=64 AND source_key NOT GLOB '*[^0-9a-f]*'),
 principal_id TEXT NOT NULL REFERENCES integration_principals(id),
 receipt_id TEXT NOT NULL UNIQUE,
 document_id TEXT NOT NULL,
 task_id TEXT NOT NULL,
 metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json) AND json_type(metadata_json)='object'),
 sha256 TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes > 0),
 received_at TEXT NOT NULL,
 PRIMARY KEY(household_id,source_key),
 UNIQUE(household_id,task_id),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id),
 FOREIGN KEY(household_id,task_id) REFERENCES archive_tasks(household_id,id),
 FOREIGN KEY(household_id,sha256) REFERENCES archive_blobs(household_id,sha256)
);
CREATE TRIGGER archive_source_immutable BEFORE UPDATE ON archive_sources
BEGIN SELECT RAISE(ABORT,'archive source receipt is immutable'); END;

CREATE TABLE archive_visibility_events (
 household_id TEXT NOT NULL,
 document_id TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version >= 1),
 actor_principal_id TEXT NOT NULL REFERENCES integration_principals(id),
 source_key TEXT NOT NULL,
 before_visible INTEGER CHECK(before_visible IN (0,1)),
 after_visible INTEGER NOT NULL CHECK(after_visible IN (0,1)),
 intent TEXT NOT NULL CHECK(intent='private'),
 created_at TEXT NOT NULL,
 PRIMARY KEY(household_id,document_id,version),
 FOREIGN KEY(household_id,document_id) REFERENCES archive_documents(household_id,id),
 FOREIGN KEY(household_id,source_key) REFERENCES archive_sources(household_id,source_key)
);
CREATE TRIGGER archive_visibility_event_immutable BEFORE UPDATE ON archive_visibility_events
BEGIN SELECT RAISE(ABORT,'archive visibility event is immutable'); END;
