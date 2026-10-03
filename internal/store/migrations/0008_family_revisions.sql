-- Revisions are immutable records. A record may have only one successor.
ALTER TABLE family_records ADD COLUMN supersedes_id TEXT REFERENCES family_records(id);
CREATE UNIQUE INDEX family_records_successor ON family_records(supersedes_id) WHERE supersedes_id IS NOT NULL;
