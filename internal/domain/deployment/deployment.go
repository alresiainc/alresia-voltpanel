package deployment

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Deployment mirrors the `deployments` table (§6, already created in
// 0001_init.sql). MigrationRollbackSupported defaults false and this
// package never sets it true -- rolling back code here never implies a
// migration was undone (§13/§18).
type Deployment struct {
	ID                         string `json:"id"`
	ProjectID                  string `json:"projectId"`
	ServerID                   string `json:"serverId"`
	CommitSHA                  string `json:"commitSha,omitempty"`
	Branch                     string `json:"branch,omitempty"`
	Status                     string `json:"status"`
	StartedAt                  string `json:"startedAt,omitempty"`
	FinishedAt                 string `json:"finishedAt,omitempty"`
	DurationMS                 int64  `json:"durationMs,omitempty"`
	LogRef                     string `json:"logRef,omitempty"`
	CodeRollbackRef            string `json:"codeRollbackRef,omitempty"`
	ArtifactRollbackRef        string `json:"artifactRollbackRef,omitempty"`
	MigrationRollbackSupported bool   `json:"migrationRollbackSupported"`
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) create(d Deployment) error {
	_, err := r.db.Exec(
		`INSERT INTO deployments (id, project_id, server_id, commit_sha, branch, status, started_at, migration_rollback_supported)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		d.ID, d.ProjectID, d.ServerID, nullable(d.CommitSHA), nullable(d.Branch), d.Status, nullable(d.StartedAt),
	)
	return err
}

func (r *Repository) finish(id, status, commitSHA, codeRollbackRef, logRef string, durationMS int64) error {
	_, err := r.db.Exec(
		`UPDATE deployments SET status = ?, finished_at = ?, duration_ms = ?, commit_sha = ?, code_rollback_ref = ?, log_ref = ? WHERE id = ?`,
		status, time.Now().UTC().Format(time.RFC3339), durationMS, nullable(commitSHA), nullable(codeRollbackRef), nullable(logRef), id,
	)
	return err
}

func (r *Repository) Get(id string) (Deployment, error) {
	row := r.db.QueryRow(
		`SELECT id, project_id, server_id, commit_sha, branch, status, started_at, finished_at, duration_ms, log_ref, code_rollback_ref, artifact_rollback_ref, migration_rollback_supported
		 FROM deployments WHERE id = ?`, id)
	return scanDeployment(row)
}

func (r *Repository) ListByProject(projectID string) ([]Deployment, error) {
	rows, err := r.db.Query(
		`SELECT id, project_id, server_id, commit_sha, branch, status, started_at, finished_at, duration_ms, log_ref, code_rollback_ref, artifact_rollback_ref, migration_rollback_supported
		 FROM deployments WHERE project_id = ? ORDER BY started_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Deployment{}
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) Log(id string) (string, error) {
	d, err := r.Get(id)
	if err != nil {
		return "", err
	}
	if d.LogRef == "" {
		return "", nil
	}
	b, err := readLogFile(d.LogRef)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func scanDeployment(row scannable) (Deployment, error) {
	var d Deployment
	var commitSHA, branch, startedAt, finishedAt, logRef, codeRollback, artifactRollback sql.NullString
	var durationMS sql.NullInt64
	var migrationSupported int
	if err := row.Scan(&d.ID, &d.ProjectID, &d.ServerID, &commitSHA, &branch, &d.Status, &startedAt, &finishedAt,
		&durationMS, &logRef, &codeRollback, &artifactRollback, &migrationSupported); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Deployment{}, ErrNotFound
		}
		return Deployment{}, err
	}
	d.CommitSHA, d.Branch, d.StartedAt, d.FinishedAt = commitSHA.String, branch.String, startedAt.String, finishedAt.String
	d.LogRef, d.CodeRollbackRef, d.ArtifactRollbackRef = logRef.String, codeRollback.String, artifactRollback.String
	d.DurationMS = durationMS.Int64
	d.MigrationRollbackSupported = migrationSupported != 0
	return d, nil
}

func newID() string { return uuid.NewString() }
