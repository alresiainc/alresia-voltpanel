-- Full data model per the implementation plan (§6). Most tables go unused
-- until their owning phase lands (Project until Phase 3, Server/Deployment
-- until Phase 7/9, etc.) -- they're created together now so later phases are
-- additive (new columns/repos), never a second migration reshaping this one.

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    path TEXT NOT NULL,
    runtime_id TEXT,
    runtime_version TEXT,
    primary_domain_id TEXT,
    webserver_kind TEXT,
    database_id TEXT,
    redis_enabled INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS runtimes (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS runtime_versions (
    id TEXT PRIMARY KEY,
    runtime_id TEXT NOT NULL REFERENCES runtimes(id),
    version TEXT NOT NULL,
    install_path TEXT,
    is_default INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'missing'
);

CREATE TABLE IF NOT EXISTS services (
    id TEXT PRIMARY KEY,
    project_id TEXT REFERENCES projects(id),
    kind TEXT NOT NULL DEFAULT 'native',
    name TEXT NOT NULL,
    command TEXT NOT NULL,
    args TEXT NOT NULL DEFAULT '[]',
    cwd TEXT,
    env TEXT NOT NULL DEFAULT '{}',
    autostart INTEGER NOT NULL DEFAULT 0,
    depends_on TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'stopped',
    pid INTEGER,
    log_path TEXT,
    started_at TEXT,
    exited_at TEXT,
    exit_code INTEGER,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS domains (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL UNIQUE,
    project_id TEXT REFERENCES projects(id),
    port INTEGER,
    provider TEXT NOT NULL DEFAULT 'hosts',
    ssl_enabled INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS certificates (
    id TEXT PRIMARY KEY,
    domain_id TEXT NOT NULL REFERENCES domains(id),
    ca_id TEXT,
    not_before TEXT,
    not_after TEXT,
    status TEXT NOT NULL DEFAULT 'valid'
);

CREATE TABLE IF NOT EXISTS servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    hostname TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 22,
    username TEXT NOT NULL,
    auth_method TEXT NOT NULL DEFAULT 'key',
    secret_ref TEXT,
    last_connected_at TEXT,
    os_info TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS deployments (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    server_id TEXT NOT NULL REFERENCES servers(id),
    commit_sha TEXT,
    branch TEXT,
    pipeline_run_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    started_at TEXT,
    finished_at TEXT,
    duration_ms INTEGER,
    log_ref TEXT,
    code_rollback_ref TEXT,
    artifact_rollback_ref TEXT,
    migration_rollback_supported INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS pipelines (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    definition_yaml TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pipeline_runs (
    id TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL REFERENCES pipelines(id),
    trigger_kind TEXT NOT NULL DEFAULT 'manual',
    status TEXT NOT NULL DEFAULT 'pending',
    steps TEXT NOT NULL DEFAULT '[]',
    started_at TEXT,
    finished_at TEXT
);

CREATE TABLE IF NOT EXISTS integrations (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    account_ref TEXT,
    secret_ref TEXT,
    scopes TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS extensions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    kind TEXT NOT NULL,
    source TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    permissions TEXT NOT NULL DEFAULT '[]'
);

-- Secret rows never hold plaintext -- storage_backend says where the real
-- value lives (OS keychain, or an encrypted local blob); see internal/security.
CREATE TABLE IF NOT EXISTS secrets (
    id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    storage_backend TEXT NOT NULL,
    encrypted_blob BLOB,
    created_at TEXT NOT NULL,
    rotated_at TEXT
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    result TEXT NOT NULL,
    created_at TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_services_project ON services(project_id);
CREATE INDEX IF NOT EXISTS idx_domains_project ON domains(project_id);
CREATE INDEX IF NOT EXISTS idx_deployments_project ON deployments(project_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_target ON audit_events(target_type, target_id);
