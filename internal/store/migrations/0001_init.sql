-- Initial Atrium schema (technical design §5).

CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE data_sources (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL,
  status TEXT NOT NULL,
  revoked_at TEXT,
  revoke_reason TEXT,
  identity_bound TEXT,
  identity_bound_at TEXT,
  health TEXT NOT NULL DEFAULT 'unknown',
  health_detail TEXT,
  last_check_at TEXT,
  last_success_at TEXT,
  scan_generation INTEGER NOT NULL DEFAULT 0,
  last_scan_started_at TEXT,
  last_scan_completed_at TEXT,
  baseline_completed_at TEXT,
  share_total_bytes INTEGER,
  share_free_bytes INTEGER,
  share_stats_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE photos (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL REFERENCES data_sources(id),
  rel_path TEXT NOT NULL,
  ext TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  mtime_unix INTEGER NOT NULL,
  fingerprint TEXT,
  status TEXT NOT NULL,
  width INTEGER,
  height INTEGER,
  orientation INTEGER,
  captured_at TEXT,
  captured_offset_seconds INTEGER,
  captured_confidence TEXT NOT NULL DEFAULT 'unknown',
  captured_day TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  last_seen_generation INTEGER NOT NULL,
  missing_generations INTEGER NOT NULL DEFAULT 0,
  is_baseline INTEGER NOT NULL DEFAULT 0,
  meta_status TEXT NOT NULL DEFAULT 'pending',
  meta_error TEXT,
  preview_status TEXT NOT NULL DEFAULT 'pending',
  preview_error TEXT,
  preview_attempts INTEGER NOT NULL DEFAULT 0,
  preview_next_retry_at TEXT,
  removed_at TEXT,
  excluded_at TEXT,
  exclude_reason TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (source_id, rel_path)
);
CREATE INDEX idx_photos_recent ON photos (first_seen_at DESC, id DESC) WHERE status = 'ready' AND is_baseline = 0;
CREATE INDEX idx_photos_day ON photos (captured_day) WHERE status = 'ready';
CREATE INDEX idx_photos_status ON photos (source_id, status);
CREATE INDEX idx_photos_preview ON photos (preview_status, preview_next_retry_at);

CREATE TABLE photo_exclusions (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL,
  match_kind TEXT NOT NULL,
  pattern TEXT NOT NULL,
  reason TEXT,
  created_at TEXT NOT NULL,
  UNIQUE (source_id, match_kind, pattern)
);

CREATE TABLE preview_files (
  photo_id TEXT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
  variant TEXT NOT NULL,
  rel_path TEXT NOT NULL,
  bytes INTEGER NOT NULL,
  width INTEGER NOT NULL,
  height INTEGER NOT NULL,
  fingerprint TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_access_at TEXT NOT NULL,
  PRIMARY KEY (photo_id, variant)
);
CREATE INDEX idx_preview_lru ON preview_files (last_access_at);

CREATE TABLE jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  photo_id TEXT,
  source_id TEXT,
  status TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_run_at TEXT NOT NULL,
  locked_by TEXT,
  locked_at TEXT,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_jobs_dedup ON jobs (kind, photo_id) WHERE status IN ('queued','running') AND photo_id IS NOT NULL;
CREATE INDEX idx_jobs_pick ON jobs (status, next_run_at, priority);

CREATE TABLE scan_runs (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL,
  mode TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  files_seen INTEGER DEFAULT 0,
  files_new INTEGER DEFAULT 0,
  files_changed INTEGER DEFAULT 0,
  files_missing INTEGER DEFAULT 0,
  files_removed INTEGER DEFAULT 0,
  files_unsupported INTEGER DEFAULT 0,
  errors INTEGER DEFAULT 0,
  note TEXT
);
CREATE INDEX idx_scan_runs_source ON scan_runs (source_id, started_at DESC);

CREATE TABLE screens (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  approved_at TEXT NOT NULL,
  revoked_at TEXT,
  last_seen_at TEXT,
  last_ip TEXT,
  client_version TEXT,
  current_route TEXT,
  last_sequence INTEGER NOT NULL DEFAULT 0,
  applied_sequence INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE pairings (
  id TEXT PRIMARY KEY,
  code TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  client_hint TEXT,
  remote_ip TEXT,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  approved_screen_id TEXT,
  approved_at TEXT,
  claimed_at TEXT
);

CREATE TABLE screen_commands (
  id TEXT PRIMARY KEY,
  screen_id TEXT NOT NULL REFERENCES screens(id),
  sequence INTEGER NOT NULL,
  kind TEXT NOT NULL,
  payload TEXT NOT NULL,
  issued_by TEXT NOT NULL,
  issued_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  status TEXT NOT NULL,
  delivered_at TEXT,
  resolved_at TEXT,
  error_code TEXT,
  result TEXT,
  UNIQUE (screen_id, sequence)
);
CREATE INDEX idx_commands_open ON screen_commands (status, expires_at);
CREATE INDEX idx_commands_screen ON screen_commands (screen_id, issued_at DESC);

CREATE TABLE admin_tokens (
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  label TEXT,
  created_at TEXT NOT NULL,
  revoked_at TEXT,
  last_used_at TEXT
);

CREATE TABLE widget_cache (
  widget TEXT PRIMARY KEY,
  payload TEXT NOT NULL,
  fetched_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  error TEXT
);

CREATE TABLE audit_log (
  id TEXT PRIMARY KEY,
  at TEXT NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT,
  detail TEXT
);
CREATE INDEX idx_audit_at ON audit_log (at DESC);
