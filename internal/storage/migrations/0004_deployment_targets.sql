-- Phase 9: per-project deployment configuration. The `deployments` table
-- itself already exists (0001_init.sql) with the three-way rollback split
-- already shaped correctly (code_rollback_ref/artifact_rollback_ref/
-- migration_rollback_supported) -- this migration only adds the reusable
-- target config a "Deploy" action runs against, per §17 Phase 9's
-- "DeploymentTarget config per project" feature.
CREATE TABLE IF NOT EXISTS deployment_targets (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    server_id TEXT NOT NULL REFERENCES servers(id),
    repo_url TEXT NOT NULL,
    integration_id TEXT REFERENCES integrations(id),
    branch TEXT NOT NULL DEFAULT 'main',
    deploy_path TEXT NOT NULL,
    install_command TEXT,
    restart_command TEXT,
    health_check_url TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployment_targets_project ON deployment_targets(project_id);
