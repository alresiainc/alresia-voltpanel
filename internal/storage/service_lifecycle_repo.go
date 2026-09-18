package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ServiceRecord is the full persisted shape of a `services` row, including
// the Phase 5 dependency/restart-policy columns that upsertService/
// getService/listServices (services_repo.go) don't touch -- those stay as
// they were for the legacy App/agent.Manager import path (see
// importLegacyApps), while internal/domain/service.Manager reads and writes
// through ServiceRecord for the formalized Service entity. Kept as a plain
// struct (not the domain type) so this package never imports
// internal/domain/service -- storage knows about persistence, not lifecycle
// semantics.
type ServiceRecord struct {
	ID        string
	ProjectID string
	Kind      string
	Name      string
	Command   string
	Args      []string
	Cwd       string
	Env       map[string]string

	Autostart bool
	DependsOn []string

	RestartPolicy          string
	GracefulTimeoutSeconds int
	MaxRestarts            int
	RestartWindowSeconds   int

	Status  string
	PID     int
	LogFile string

	StartedAt *time.Time
	ExitedAt  *time.Time
	ExitCode  *int
}

func (s *Store) UpsertServiceRecord(r ServiceRecord) error { return upsertServiceRecord(s.db, r) }

func (s *Store) GetServiceRecord(id string) (ServiceRecord, bool) {
	r, ok, err := getServiceRecord(s.db, id)
	if err != nil {
		return ServiceRecord{}, false
	}
	return r, ok
}

func (s *Store) ListServiceRecords() ([]ServiceRecord, error) {
	return listServiceRecords(s.db)
}

// DeleteServiceRecordsForProject removes every persisted service row tied
// to projectID -- called when a project itself is deleted, since
// services.project_id is a foreign key with no ON DELETE CASCADE and
// leaving a row behind would otherwise make the project delete fail
// outright. Callers are responsible for stopping any of these still
// actually running first; this only touches the persisted record.
func (s *Store) DeleteServiceRecordsForProject(projectID string) error {
	_, err := s.db.Exec(`DELETE FROM services WHERE project_id = ?`, projectID)
	return err
}

func upsertServiceRecord(db *sql.DB, r ServiceRecord) error {
	args, _ := json.Marshal(r.Args)
	env, _ := json.Marshal(r.Env)
	dependsOn, _ := json.Marshal(r.DependsOn)
	now := time.Now().UTC().Format(time.RFC3339)

	var projectID, startedAt, exitedAt any
	if r.ProjectID != "" {
		projectID = r.ProjectID
	}
	if r.StartedAt != nil {
		startedAt = r.StartedAt.UTC().Format(time.RFC3339)
	}
	if r.ExitedAt != nil {
		exitedAt = r.ExitedAt.UTC().Format(time.RFC3339)
	}
	kind := r.Kind
	if kind == "" {
		kind = "native"
	}
	restartPolicy := r.RestartPolicy
	if restartPolicy == "" {
		restartPolicy = "on-failure"
	}

	_, err := db.Exec(`
		INSERT INTO services (
			id, project_id, kind, name, command, args, cwd, env, autostart, depends_on,
			restart_policy, graceful_timeout_seconds, max_restarts, restart_window_seconds,
			status, pid, log_path, started_at, exited_at, exit_code, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			project_id=excluded.project_id, kind=excluded.kind, name=excluded.name, command=excluded.command,
			args=excluded.args, cwd=excluded.cwd, env=excluded.env, autostart=excluded.autostart,
			depends_on=excluded.depends_on, restart_policy=excluded.restart_policy,
			graceful_timeout_seconds=excluded.graceful_timeout_seconds, max_restarts=excluded.max_restarts,
			restart_window_seconds=excluded.restart_window_seconds, status=excluded.status, pid=excluded.pid,
			log_path=excluded.log_path, started_at=excluded.started_at, exited_at=excluded.exited_at,
			exit_code=excluded.exit_code, updated_at=excluded.updated_at
	`, r.ID, projectID, kind, r.Name, r.Command, string(args), r.Cwd, string(env), boolToInt(r.Autostart), string(dependsOn),
		restartPolicy, r.GracefulTimeoutSeconds, r.MaxRestarts, r.RestartWindowSeconds,
		r.Status, nullableInt(r.PID), r.LogFile, startedAt, exitedAt, r.ExitCode, now, now)
	return err
}

func getServiceRecord(db *sql.DB, id string) (ServiceRecord, bool, error) {
	row := db.QueryRow(`
		SELECT id, project_id, kind, name, command, args, cwd, env, autostart, depends_on,
			restart_policy, graceful_timeout_seconds, max_restarts, restart_window_seconds,
			status, pid, log_path, started_at, exited_at, exit_code
		FROM services WHERE id = ?`, id)
	r, err := scanServiceRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ServiceRecord{}, false, nil
	}
	if err != nil {
		return ServiceRecord{}, false, err
	}
	return r, true, nil
}

func listServiceRecords(db *sql.DB) ([]ServiceRecord, error) {
	rows, err := db.Query(`
		SELECT id, project_id, kind, name, command, args, cwd, env, autostart, depends_on,
			restart_policy, graceful_timeout_seconds, max_restarts, restart_window_seconds,
			status, pid, log_path, started_at, exited_at, exit_code
		FROM services ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceRecord{}
	for rows.Next() {
		r, err := scanServiceRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanServiceRecord(row rowScanner) (ServiceRecord, error) {
	var r ServiceRecord
	var args, env, dependsOn string
	var projectID, cwd, logPath, startedAt, exitedAt sql.NullString
	var autostart, pid, code sql.NullInt64

	if err := row.Scan(
		&r.ID, &projectID, &r.Kind, &r.Name, &r.Command, &args, &cwd, &env, &autostart, &dependsOn,
		&r.RestartPolicy, &r.GracefulTimeoutSeconds, &r.MaxRestarts, &r.RestartWindowSeconds,
		&r.Status, &pid, &logPath, &startedAt, &exitedAt, &code,
	); err != nil {
		return ServiceRecord{}, err
	}

	_ = json.Unmarshal([]byte(args), &r.Args)
	_ = json.Unmarshal([]byte(env), &r.Env)
	_ = json.Unmarshal([]byte(dependsOn), &r.DependsOn)
	r.ProjectID = projectID.String
	r.Cwd = cwd.String
	r.LogFile = logPath.String
	r.Autostart = autostart.Valid && autostart.Int64 != 0
	if pid.Valid {
		r.PID = int(pid.Int64)
	}
	if startedAt.Valid {
		if t, err := time.Parse(time.RFC3339, startedAt.String); err == nil {
			r.StartedAt = &t
		}
	}
	if exitedAt.Valid {
		if t, err := time.Parse(time.RFC3339, exitedAt.String); err == nil {
			r.ExitedAt = &t
		}
	}
	if code.Valid {
		c := int(code.Int64)
		r.ExitCode = &c
	}
	return r, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullableInt turns a zero PID into a SQL NULL rather than storing a
// misleading 0 for "not running".
func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
