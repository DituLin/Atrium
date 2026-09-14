CREATE TABLE video_work (
 video_id TEXT PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL,
 source_generation TEXT NOT NULL,
 token TEXT NOT NULL UNIQUE,
 lease_until TEXT NOT NULL,
 next_run_at TEXT NOT NULL,
 attempts INTEGER NOT NULL CHECK (attempts > 0),
 error_code TEXT NOT NULL DEFAULT '',
 cover_bytes INTEGER NOT NULL DEFAULT 0,
 cover_width INTEGER NOT NULL DEFAULT 0,
 cover_height INTEGER NOT NULL DEFAULT 0
);
