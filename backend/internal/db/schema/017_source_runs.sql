-- Market-data run log (ADR 0010): one row per source per run, written by
-- scheduler.RunLog and read by GET /api/admin/sources. Not user data, so no
-- user_id, like prices and settings. kind is scheduled, catchup, backfill or
-- manual. Timestamps are UTC RFC3339; finished_at is NULL while the run is
-- going, error is NULL on success.
CREATE TABLE source_runs (
    id INTEGER PRIMARY KEY,
    source TEXT NOT NULL,
    kind TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    rows INTEGER NOT NULL DEFAULT 0,
    error TEXT
);
