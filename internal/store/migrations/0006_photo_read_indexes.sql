-- Random gallery pages enumerate eligible IDs in ID order. Include every
-- photo-side predicate and join key so this stays a covering index read,
-- without fetching the large photo rows or sorting the whole eligible set.
CREATE INDEX idx_photos_random_cover ON photos (status, preview_status, id, source_id);

-- Every gallery response reports this count. Cover the filter and source join
-- instead of scanning all ready photo rows to find unknown capture times.
CREATE INDEX idx_photos_unknown_cover ON photos (captured_confidence, status, source_id);
