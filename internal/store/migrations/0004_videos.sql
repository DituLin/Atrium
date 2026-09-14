-- Videos share source authorization, but never enter photo/preview jobs.
CREATE TABLE videos (
 id TEXT PRIMARY KEY,
 source_id TEXT NOT NULL REFERENCES data_sources(id),
 rel_path TEXT NOT NULL,
 ext TEXT NOT NULL CHECK (ext IN ('mp4', 'mov')),
 size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
 mtime_unix INTEGER NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
 status TEXT NOT NULL DEFAULT 'pending'
   CHECK (status IN ('pending','ready','unsupported','removed','excluded')),
 metadata_json TEXT NOT NULL DEFAULT '{}',
 last_seen_generation INTEGER NOT NULL,
 missing_generations INTEGER NOT NULL DEFAULT 0,
 last_missing_generation INTEGER NOT NULL DEFAULT 0,
 first_seen_at TEXT NOT NULL,
 last_seen_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(source_id, rel_path)
);
CREATE INDEX idx_videos_source_status ON videos(source_id,status);
CREATE INDEX idx_videos_recent ON videos(first_seen_at DESC,id DESC);

-- Source-wide completion fence also rejects late inserts of unseen paths.
CREATE TABLE video_scan_state (
 source_id TEXT PRIMARY KEY REFERENCES data_sources(id),
 completed_generation INTEGER NOT NULL CHECK (completed_generation > 0)
);
