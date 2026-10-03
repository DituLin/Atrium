-- Metadata is registered only after local original publication. Acceptance
-- commits blob + logical document + later source/task rows together. A rollback
-- never deletes an original: the next acceptance can verify and reuse it.
CREATE TABLE archive_blobs (
 household_id TEXT NOT NULL REFERENCES households(id),
 sha256 TEXT NOT NULL CHECK (typeof(sha256)='text' AND length(CAST(sha256 AS BLOB))=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
 size_bytes INTEGER NOT NULL CHECK (typeof(size_bytes)='integer' AND size_bytes>0),
 mime TEXT NOT NULL CHECK (length(mime) BETWEEN 1 AND 255),
 storage_version INTEGER NOT NULL CHECK (storage_version=1),
 created_at TEXT NOT NULL CHECK (length(created_at)>0),
 PRIMARY KEY (household_id,sha256)
);

CREATE TRIGGER archive_blob_metadata_immutable BEFORE UPDATE ON archive_blobs
BEGIN SELECT RAISE(ABORT,'archive blob metadata is immutable'); END;

CREATE TABLE archive_documents (
 id TEXT NOT NULL,
 household_id TEXT NOT NULL,
 blob_sha256 TEXT NOT NULL,
 input_kind TEXT NOT NULL CHECK (input_kind IN ('file','text')),
 tv_visible INTEGER NOT NULL DEFAULT 1 CHECK (tv_visible IN (0,1)),
 created_at TEXT NOT NULL CHECK (length(created_at)>0),
 PRIMARY KEY (household_id,id),
 FOREIGN KEY (household_id,blob_sha256) REFERENCES archive_blobs(household_id,sha256)
);
-- Intentionally not unique: identical text bytes may be separate occurrences.
CREATE INDEX archive_documents_blob ON archive_documents(household_id,blob_sha256);

CREATE TRIGGER archive_document_identity_immutable BEFORE UPDATE ON archive_documents
WHEN NEW.id IS NOT OLD.id OR NEW.household_id IS NOT OLD.household_id
 OR NEW.blob_sha256 IS NOT OLD.blob_sha256 OR NEW.input_kind IS NOT OLD.input_kind
 OR NEW.created_at IS NOT OLD.created_at
BEGIN SELECT RAISE(ABORT,'archive document identity is immutable'); END;
