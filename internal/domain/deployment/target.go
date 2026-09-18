// Package deployment implements Phase 9 (§17): DeploymentTarget config per
// project, a manual Deploy action over SSH (Phase 7's RemoteProvider), and
// deployment history with the three-way rollback split from §18 --
// code_rollback_ref (git revert/checkout), artifact_rollback_ref (unused
// here -- this deploys straight from a git checkout, not a built
// artifact), and migration_rollback_supported, which this package never
// sets true: rolling back code here never claims to undo a migration.
package deployment

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by Get/Delete for an unknown id.
var ErrNotFound = errors.New("deployment: not found")

// Target is a reusable "how to deploy this project" config -- one project
// can have at most a simple 1:1 target for now (multiple environments per
// project is a real future need, not required by this phase's acceptance
// criterion).
type Target struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"projectId"`
	ServerID       string    `json:"serverId"`
	RepoURL        string    `json:"repoUrl"`
	IntegrationID  string    `json:"integrationId,omitempty"`
	Branch         string    `json:"branch"`
	DeployPath     string    `json:"deployPath"`
	InstallCommand string    `json:"installCommand,omitempty"`
	RestartCommand string    `json:"restartCommand,omitempty"`
	HealthCheckURL string    `json:"healthCheckUrl,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type TargetRepository struct {
	db *sql.DB
}

func NewTargetRepository(db *sql.DB) *TargetRepository { return &TargetRepository{db: db} }

type CreateTargetRequest struct {
	ProjectID      string
	ServerID       string
	RepoURL        string
	IntegrationID  string
	Branch         string
	DeployPath     string
	InstallCommand string
	RestartCommand string
	HealthCheckURL string
}

func (r *TargetRepository) Create(req CreateTargetRequest) (Target, error) {
	if req.Branch == "" {
		req.Branch = "main"
	}
	now := time.Now().UTC()
	t := Target{
		ID: uuid.NewString(), ProjectID: req.ProjectID, ServerID: req.ServerID,
		RepoURL: req.RepoURL, IntegrationID: req.IntegrationID, Branch: req.Branch,
		DeployPath: req.DeployPath, InstallCommand: req.InstallCommand,
		RestartCommand: req.RestartCommand, HealthCheckURL: req.HealthCheckURL,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err := r.db.Exec(
		`INSERT INTO deployment_targets (id, project_id, server_id, repo_url, integration_id, branch, deploy_path, install_command, restart_command, health_check_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.ServerID, t.RepoURL, nullable(t.IntegrationID), t.Branch, t.DeployPath,
		nullable(t.InstallCommand), nullable(t.RestartCommand), nullable(t.HealthCheckURL),
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		return Target{}, err
	}
	return t, nil
}

func (r *TargetRepository) Get(id string) (Target, error) {
	row := r.db.QueryRow(
		`SELECT id, project_id, server_id, repo_url, integration_id, branch, deploy_path, install_command, restart_command, health_check_url, created_at, updated_at
		 FROM deployment_targets WHERE id = ?`, id)
	return scanTarget(row)
}

func (r *TargetRepository) ListByProject(projectID string) ([]Target, error) {
	rows, err := r.db.Query(
		`SELECT id, project_id, server_id, repo_url, integration_id, branch, deploy_path, install_command, restart_command, health_check_url, created_at, updated_at
		 FROM deployment_targets WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Target{}
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TargetRepository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM deployment_targets WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanTarget(row scannable) (Target, error) {
	var t Target
	var integrationID, installCmd, restartCmd, healthURL sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&t.ID, &t.ProjectID, &t.ServerID, &t.RepoURL, &integrationID, &t.Branch, &t.DeployPath,
		&installCmd, &restartCmd, &healthURL, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Target{}, ErrNotFound
		}
		return Target{}, err
	}
	t.IntegrationID, t.InstallCommand, t.RestartCommand, t.HealthCheckURL = integrationID.String, installCmd.String, restartCmd.String, healthURL.String
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return t, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
