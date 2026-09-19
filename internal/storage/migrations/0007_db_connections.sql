-- Database admin tool (§ "something like Adminer"): a saved connection to
-- a real MySQL/PostgreSQL server. The password lives behind
-- internal/security.SecretStore (OS keychain, or an encrypted blob as
-- fallback) -- secret_ref is the only credential-shaped thing this table
-- ever holds, matching servers.secret_ref's existing pattern for SSH keys.
CREATE TABLE IF NOT EXISTS db_connections (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,          -- mysql | postgres
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    username TEXT NOT NULL,
    database_name TEXT,          -- default database/schema to connect to, if any
    secret_ref TEXT,
    created_at TEXT NOT NULL
);
