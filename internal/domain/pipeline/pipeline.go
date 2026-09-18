// Package pipeline is the repository for the `pipelines`/`pipeline_runs`
// tables (§6, §17 Phase 10). internal/pipeline holds the actual step-
// running engine; this package only persists definitions and run history.
package pipeline

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("pipeline: not found")

type Pipeline struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"projectId"`
	Name           string    `json:"name"`
	DefinitionYAML string    `json:"definitionYaml"`
	WebhookSecret  string    `json:"-"` // never serialized to the API response
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Run struct {
	ID          string           `json:"id"`
	PipelineID  string           `json:"pipelineId"`
	TriggerKind string           `json:"triggerKind"`
	Status      string           `json:"status"`
	Steps       []map[string]any `json:"steps"`
	StartedAt   string           `json:"startedAt,omitempty"`
	FinishedAt  string           `json:"finishedAt,omitempty"`
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func newWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (r *Repository) Create(projectID, name, definitionYAML string) (Pipeline, error) {
	secret, err := newWebhookSecret()
	if err != nil {
		return Pipeline{}, err
	}
	now := time.Now().UTC()
	p := Pipeline{ID: uuid.NewString(), ProjectID: projectID, Name: name, DefinitionYAML: definitionYAML, WebhookSecret: secret, CreatedAt: now, UpdatedAt: now}
	_, err = r.db.Exec(
		`INSERT INTO pipelines (id, project_id, name, definition_yaml, webhook_secret, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.ProjectID, p.Name, p.DefinitionYAML, p.WebhookSecret, now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		return Pipeline{}, err
	}
	return p, nil
}

func (r *Repository) Get(id string) (Pipeline, error) {
	row := r.db.QueryRow(`SELECT id, project_id, name, definition_yaml, webhook_secret, created_at, updated_at FROM pipelines WHERE id = ?`, id)
	return scanPipeline(row)
}

func (r *Repository) ListByProject(projectID string) ([]Pipeline, error) {
	rows, err := r.db.Query(`SELECT id, project_id, name, definition_yaml, webhook_secret, created_at, updated_at FROM pipelines WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Pipeline{}
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM pipelines WHERE id = ?`, id)
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

func scanPipeline(row scannable) (Pipeline, error) {
	var p Pipeline
	var createdAt, updatedAt string
	if err := row.Scan(&p.ID, &p.ProjectID, &p.Name, &p.DefinitionYAML, &p.WebhookSecret, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Pipeline{}, ErrNotFound
		}
		return Pipeline{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return p, nil
}

func (r *Repository) CreateRun(pipelineID, triggerKind string) (Run, error) {
	run := Run{ID: uuid.NewString(), PipelineID: pipelineID, TriggerKind: triggerKind, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	_, err := r.db.Exec(
		`INSERT INTO pipeline_runs (id, pipeline_id, trigger_kind, status, started_at) VALUES (?, ?, ?, ?, ?)`,
		run.ID, run.PipelineID, run.TriggerKind, run.Status, run.StartedAt,
	)
	return run, err
}

func (r *Repository) FinishRun(id, status string, steps []map[string]any) error {
	b, err := json.Marshal(steps)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(
		`UPDATE pipeline_runs SET status = ?, steps = ?, finished_at = ? WHERE id = ?`,
		status, string(b), time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

func (r *Repository) ListRuns(pipelineID string) ([]Run, error) {
	rows, err := r.db.Query(`SELECT id, pipeline_id, trigger_kind, status, steps, started_at, finished_at FROM pipeline_runs WHERE pipeline_id = ? ORDER BY started_at DESC`, pipelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var run Run
		var stepsJSON string
		var startedAt, finishedAt sql.NullString
		if err := rows.Scan(&run.ID, &run.PipelineID, &run.TriggerKind, &run.Status, &stepsJSON, &startedAt, &finishedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(stepsJSON), &run.Steps)
		run.StartedAt, run.FinishedAt = startedAt.String, finishedAt.String
		out = append(out, run)
	}
	return out, rows.Err()
}
