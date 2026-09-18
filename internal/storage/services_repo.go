package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// serviceRow (de)serializes an App to/from the SQLite services table. This
// stays a thin persistence layer for the pre-existing App/agent.Manager
// concept -- Phase 5 is where Service becomes the real formalized domain
// entity with dependency ordering and graceful stop; Phase 1 only moves the
// storage backend, keeping every caller-visible behavior identical.
func upsertService(db *sql.DB, a App) error {
	args, _ := json.Marshal(a.Args)
	env, _ := json.Marshal(a.Env)
	now := time.Now().UTC().Format(time.RFC3339)
	var startedAt, exitedAt any
	if a.StartedAt != nil {
		startedAt = a.StartedAt.UTC().Format(time.RFC3339)
	}
	if a.ExitedAt != nil {
		exitedAt = a.ExitedAt.UTC().Format(time.RFC3339)
	}
	_, err := db.Exec(`
		INSERT INTO services (id, kind, name, command, args, cwd, env, status, pid, log_path, started_at, exited_at, exit_code, created_at, updated_at)
		VALUES (?, 'native', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, command=excluded.command, args=excluded.args, cwd=excluded.cwd,
			env=excluded.env, status=excluded.status, pid=excluded.pid, log_path=excluded.log_path,
			started_at=excluded.started_at, exited_at=excluded.exited_at, exit_code=excluded.exit_code,
			updated_at=excluded.updated_at
	`, a.ID, a.Name, a.Command, string(args), a.Cwd, string(env), a.Status, a.PID, a.LogFile,
		startedAt, exitedAt, a.Code, now, now)
	return err
}

func getService(db *sql.DB, id string) (App, bool, error) {
	row := db.QueryRow(`SELECT id, name, command, args, cwd, env, status, pid, log_path, started_at, exited_at, exit_code FROM services WHERE id = ?`, id)
	a, err := scanService(row)
	if errors.Is(err, sql.ErrNoRows) {
		return App{}, false, nil
	}
	if err != nil {
		return App{}, false, err
	}
	return a, true, nil
}

func listServices(db *sql.DB) ([]App, error) {
	rows, err := db.Query(`SELECT id, name, command, args, cwd, env, status, pid, log_path, started_at, exited_at, exit_code FROM services ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		a, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanService(row rowScanner) (App, error) {
	var a App
	var args, env string
	var cwd, logPath, startedAt, exitedAt sql.NullString
	var pid, code sql.NullInt64
	if err := row.Scan(&a.ID, &a.Name, &a.Command, &args, &cwd, &env, &a.Status, &pid, &logPath, &startedAt, &exitedAt, &code); err != nil {
		return App{}, err
	}
	_ = json.Unmarshal([]byte(args), &a.Args)
	_ = json.Unmarshal([]byte(env), &a.Env)
	a.Cwd = cwd.String
	a.LogFile = logPath.String
	if pid.Valid {
		a.PID = int(pid.Int64)
	}
	if startedAt.Valid {
		if t, err := time.Parse(time.RFC3339, startedAt.String); err == nil {
			a.StartedAt = &t
		}
	}
	if exitedAt.Valid {
		if t, err := time.Parse(time.RFC3339, exitedAt.String); err == nil {
			a.ExitedAt = &t
		}
	}
	if code.Valid {
		c := int(code.Int64)
		a.Code = &c
	}
	return a, nil
}

// importLegacyApps loads cfgDir/apps.json (the pre-SQLite JSON store, if
// present) and inserts any service not already in SQLite. It never
// overwrites an existing row and never touches apps.json itself, so it's
// safe to call on every startup.
func importLegacyApps(db *sql.DB, cfgDir string) error {
	apps, err := readLegacyApps(cfgDir)
	if err != nil || len(apps) == 0 {
		return err
	}
	for _, a := range apps {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(1) FROM services WHERE id = ?`, a.ID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if err := upsertService(db, a); err != nil {
			return err
		}
	}
	return nil
}
