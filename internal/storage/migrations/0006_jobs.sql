-- Package/runtime install-management (Homebrew-backed): installing or
-- upgrading real software can take minutes, so it always runs as an
-- async job the UI polls/streams progress for (over the same WS "log"
-- event every service/deployment log already uses) rather than blocking
-- an HTTP request. This table is that job's persisted record.
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,      -- install | upgrade | uninstall | link
    target TEXT NOT NULL,    -- formula name, e.g. "php@8.3"
    status TEXT NOT NULL,    -- queued | running | success | failed
    log_file TEXT NOT NULL,
    error TEXT,
    exit_code INTEGER,
    started_at TEXT NOT NULL,
    finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_started_at ON jobs(started_at);
