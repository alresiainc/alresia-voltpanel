// Package project implements Project as a first-class entity (§17 Phase 3
// of the implementation plan): CRUD backed by the `projects` table SQLite
// migration 0001 already created, plus path validation and the framework-
// detection engine in the detect subpackage.
package project

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/project/detect"
	"github.com/google/uuid"
)

// ErrNotFound is returned by Get/Delete/Redetect when no project with the
// given id exists.
var ErrNotFound = errors.New("project not found")

// Project mirrors the `projects` table (§6 of the plan). RuntimeID/
// RuntimeVersion stay plain strings, empty when unset -- Phase 2 (runtime
// providers) owns populating them; this package only needs the columns to
// exist and round-trip, no join required.
type Project struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Path           string    `json:"path"`
	RuntimeID      string    `json:"runtimeId,omitempty"`
	RuntimeVersion string    `json:"runtimeVersion,omitempty"`
	DetectedKind   string    `json:"detectedKind"`
	RunCommand     string    `json:"runCommand"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Repository is the SQLite-backed CRUD layer for Project.
type Repository struct {
	db *sql.DB
}

// NewRepository builds a Repository over an already-migrated *sql.DB
// (internal/storage.Store.DB()).
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// ValidatePath enforces §17 Phase 3's stated scope: the path must be
// absolute and an existing directory. There is no per-project sandbox
// concept yet -- internal/security.Sandbox stays rooted at the home
// directory until a later phase wires projects into it -- so this is a
// plain filesystem check, not a security boundary.
func ValidatePath(p string) error {
	if p == "" {
		return fmt.Errorf("project path must not be empty")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("project path must be absolute: %q", p)
	}
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("project path does not exist or is not accessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project path is not a directory: %q", p)
	}
	return nil
}

// Create validates path, runs framework detection against it, and inserts
// the resulting row.
func (r *Repository) Create(name, path string) (Project, error) {
	if err := ValidatePath(path); err != nil {
		return Project{}, err
	}
	result, err := detect.Detect(path)
	if err != nil {
		return Project{}, fmt.Errorf("detect framework: %w", err)
	}

	now := time.Now().UTC()
	p := Project{
		ID:           uuid.NewString(),
		Name:         name,
		Path:         path,
		DetectedKind: string(result.Kind),
		RunCommand:   result.RunCommand,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err = r.db.Exec(`
		INSERT INTO projects (id, name, path, detected_kind, run_command, redis_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
	`, p.ID, p.Name, p.Path, p.DetectedKind, p.RunCommand, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return Project{}, err
	}
	return p, nil
}

// Get fetches one project by id.
func (r *Repository) Get(id string) (Project, error) {
	row := r.db.QueryRow(`
		SELECT id, name, path, COALESCE(runtime_id, ''), COALESCE(runtime_version, ''),
		       detected_kind, run_command, created_at, updated_at
		FROM projects WHERE id = ?
	`, id)
	return scanProject(row)
}

// List returns every project, oldest first.
func (r *Repository) List() ([]Project, error) {
	rows, err := r.db.Query(`
		SELECT id, name, path, COALESCE(runtime_id, ''), COALESCE(runtime_version, ''),
		       detected_kind, run_command, created_at, updated_at
		FROM projects ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Delete removes a project row. It does not touch anything on disk at
// path -- deleting a Project only forgets VoltPanel's record of it.
func (r *Repository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Redetect re-runs framework detection against the project's current path
// and persists the result -- used by POST .../detect when a project's
// dependencies changed since it was added (e.g. `next` was installed).
func (r *Repository) Redetect(id string) (Project, error) {
	p, err := r.Get(id)
	if err != nil {
		return Project{}, err
	}
	result, err := detect.Detect(p.Path)
	if err != nil {
		return Project{}, fmt.Errorf("detect framework: %w", err)
	}
	now := time.Now().UTC()
	_, err = r.db.Exec(`UPDATE projects SET detected_kind = ?, run_command = ?, updated_at = ? WHERE id = ?`,
		string(result.Kind), result.RunCommand, now.Format(time.RFC3339), id)
	if err != nil {
		return Project{}, err
	}
	p.DetectedKind = string(result.Kind)
	p.RunCommand = result.RunCommand
	p.UpdatedAt = now
	return p, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProject(row rowScanner) (Project, error) {
	var p Project
	var createdAt, updatedAt string
	if err := row.Scan(&p.ID, &p.Name, &p.Path, &p.RuntimeID, &p.RuntimeVersion,
		&p.DetectedKind, &p.RunCommand, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return Project{}, err
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		p.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		p.UpdatedAt = t
	}
	return p, nil
}
