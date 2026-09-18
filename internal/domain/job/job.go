// Package job persists the record of a long-running background operation
// (today: a Homebrew install/upgrade/uninstall -- internal/providers/
// pkgmanager/brew) so the UI can list/poll it and see it survive a page
// refresh, the same way a Deployment or PipelineRun's history does.
// Live progress itself streams over the existing WS "log" event, keyed by
// this Job's ID exactly like a Service's logs are -- this package only
// ever holds status/metadata, never the log content itself.
package job

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by Get when no matching row exists.
var ErrNotFound = errors.New("job: not found")

type Status string

const (
	StatusRunning Status = "running"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

// Job is one background package-management operation.
type Job struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`   // install | upgrade | uninstall | link
	Target     string     `json:"target"` // formula name
	Status     Status     `json:"status"`
	LogFile    string     `json:"logFile"`
	Error      string     `json:"error,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// Repository is the SQLite-backed CRUD layer for Job.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Create inserts a new job row in StatusRunning -- callers create it right
// as they exec the underlying command, so there's no separate "queued"
// state to persist (nothing in this daemon queues installs; each one just
// runs in its own goroutine immediately).
func (r *Repository) Create(kind, target, logFile string) (Job, error) {
	j := Job{ID: uuid.NewString(), Kind: kind, Target: target, Status: StatusRunning, LogFile: logFile, StartedAt: time.Now().UTC()}
	_, err := r.db.Exec(`
		INSERT INTO jobs (id, kind, target, status, log_file, started_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, j.ID, j.Kind, j.Target, string(j.Status), j.LogFile, j.StartedAt.Format(time.RFC3339))
	if err != nil {
		return Job{}, err
	}
	return j, nil
}

// SetLogFile records where a job's output is being written -- Create
// doesn't take this directly because the log file's own name is derived
// from the ID Create generates (see internal/providers/pkgmanager/brew's
// runJob), so it's always set in a second step immediately after.
func (r *Repository) SetLogFile(id, logFile string) error {
	_, err := r.db.Exec(`UPDATE jobs SET log_file = ? WHERE id = ?`, logFile, id)
	return err
}

// Finish records the terminal state of a job -- called once the
// underlying command exits, whatever the outcome.
func (r *Repository) Finish(id string, status Status, exitCode int, errMsg string) error {
	now := time.Now().UTC()
	var errVal any
	if errMsg != "" {
		errVal = errMsg
	}
	_, err := r.db.Exec(`
		UPDATE jobs SET status = ?, exit_code = ?, error = ?, finished_at = ? WHERE id = ?
	`, string(status), exitCode, errVal, now.Format(time.RFC3339), id)
	return err
}

// Get fetches one job by id.
func (r *Repository) Get(id string) (Job, error) {
	row := r.db.QueryRow(`
		SELECT id, kind, target, status, log_file, COALESCE(error, ''), exit_code, started_at, finished_at
		FROM jobs WHERE id = ?
	`, id)
	return scanJob(row)
}

// List returns every job, most recently started first. Ties on
// started_at (RFC3339 timestamps only have second resolution, and two
// jobs can easily start within the same second) break on rowid, so
// "most recent first" stays well-defined rather than depending on
// SQLite's unspecified tie order.
func (r *Repository) List() ([]Job, error) {
	rows, err := r.db.Query(`
		SELECT id, kind, target, status, log_file, COALESCE(error, ''), exit_code, started_at, finished_at
		FROM jobs ORDER BY started_at DESC, rowid DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var j Job
	var status, startedAt string
	var finishedAt sql.NullString
	var exitCode sql.NullInt64
	if err := row.Scan(&j.ID, &j.Kind, &j.Target, &status, &j.LogFile, &j.Error, &exitCode, &startedAt, &finishedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	j.Status = Status(status)
	if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
		j.StartedAt = t
	}
	if finishedAt.Valid {
		if t, err := time.Parse(time.RFC3339, finishedAt.String); err == nil {
			j.FinishedAt = &t
		}
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		j.ExitCode = &v
	}
	return j, nil
}
