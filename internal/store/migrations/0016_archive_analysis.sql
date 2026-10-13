CREATE TABLE archive_analysis_snapshots (
 household_id TEXT NOT NULL REFERENCES households(id),
 id TEXT NOT NULL,
 authority TEXT NOT NULL,
 public INTEGER NOT NULL CHECK(public IN (0,1)),
 epoch INTEGER NOT NULL,
 generation INTEGER NOT NULL,
 algorithm_version TEXT NOT NULL,
 query_json TEXT NOT NULL,
 input_digest TEXT NOT NULL,
 result_json TEXT NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(household_id,id)
);
CREATE INDEX archive_analysis_expiry ON archive_analysis_snapshots(household_id,created_at);
