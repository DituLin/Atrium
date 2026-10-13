CREATE TABLE archive_query_receipts (
 id TEXT PRIMARY KEY,
 household_id TEXT NOT NULL REFERENCES households(id),
 principal_id TEXT NOT NULL,
 credential_id TEXT NOT NULL,
 filter_json TEXT NOT NULL,
 analysis_json TEXT,
 expires_at TEXT NOT NULL
);
CREATE INDEX archive_query_receipt_expiry ON archive_query_receipts(expires_at);
CREATE TABLE archive_screen_views (
 id TEXT PRIMARY KEY,
 query_ref TEXT NOT NULL REFERENCES archive_query_receipts(id),
 household_id TEXT NOT NULL REFERENCES households(id),
 principal_id TEXT NOT NULL,
 credential_id TEXT NOT NULL,
 screen_id TEXT NOT NULL REFERENCES screens(id),
 operation_id TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 UNIQUE(principal_id,operation_id)
);
