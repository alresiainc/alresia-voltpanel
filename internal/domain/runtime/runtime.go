// Package runtime is the repository for the `runtimes`/`runtime_versions`
// SQLite tables (internal/storage/migrations/0001_init.sql). It holds only
// plain data + persistence -- no provider-specific logic (§4 of the plan:
// "internal/domain/* ... No provider-specific logic here"). The API layer
// (internal/api/v1/runtimes.go) is what bridges a
// providers.RuntimeProvider's detection results into this repository.
package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Runtime is one row of the `runtimes` table: a runtime kind ("node",
// "php", ...) VoltPanel knows about.
type Runtime struct {
	ID   string
	Kind string
	Name string
}

// Version is one row of the `runtime_versions` table: a specific installed
// (or previously-detected) version of a Runtime.
type Version struct {
	ID          string
	RuntimeID   string
	Version     string
	InstallPath string
	IsDefault   bool
	Status      string // installed|installing|missing
}

// DetectedVersion is the minimal shape the API layer passes in after
// calling a RuntimeProvider's DetectInstalled -- kept separate from
// Version so this package never needs to import internal/providers.
type DetectedVersion struct {
	Version     string
	InstallPath string
	IsDefault   bool
}

// WithVersions bundles a Runtime with all of its known Versions, which is
// the shape the runtimes list API returns.
type WithVersions struct {
	Runtime
	Versions []Version
}

var ErrVersionNotFound = errors.New("runtime version not found")

// Repository persists Runtime/Version rows in SQLite.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db (typically
// storage.Store.DB()).
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// EnsureRuntime returns the Runtime row for kind, inserting one (using
// name as its display name) if it doesn't exist yet. Safe to call
// repeatedly.
func (r *Repository) EnsureRuntime(ctx context.Context, kind, name string) (Runtime, error) {
	if existing, ok, err := r.getRuntimeByKind(ctx, kind); err != nil {
		return Runtime{}, err
	} else if ok {
		return existing, nil
	}
	rt := Runtime{ID: uuid.NewString(), Kind: kind, Name: name}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO runtimes (id, kind, name) VALUES (?, ?, ?)`, rt.ID, rt.Kind, rt.Name); err != nil {
		return Runtime{}, fmt.Errorf("insert runtime: %w", err)
	}
	return rt, nil
}

func (r *Repository) getRuntimeByKind(ctx context.Context, kind string) (Runtime, bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, kind, name FROM runtimes WHERE kind = ?`, kind)
	var rt Runtime
	if err := row.Scan(&rt.ID, &rt.Kind, &rt.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Runtime{}, false, nil
		}
		return Runtime{}, false, err
	}
	return rt, true, nil
}

// ReplaceDetected persists the result of a RuntimeProvider.DetectInstalled
// call for the given kind: it ensures the Runtime row exists, then
// replaces every Version row for that runtime with what was just
// detected. Existing versions not present in `detected` are marked
// "missing" (not deleted, so history/is_default intent isn't silently
// lost) rather than removed outright.
func (r *Repository) ReplaceDetected(ctx context.Context, kind, name string, detected []DetectedVersion) (Runtime, error) {
	rt, err := r.EnsureRuntime(ctx, kind, name)
	if err != nil {
		return Runtime{}, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Runtime{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE runtime_versions SET status = 'missing' WHERE runtime_id = ?`, rt.ID); err != nil {
		return Runtime{}, fmt.Errorf("mark existing versions missing: %w", err)
	}

	for _, d := range detected {
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM runtime_versions WHERE runtime_id = ? AND version = ?`, rt.ID, d.Version).Scan(&existingID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			id := uuid.NewString()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO runtime_versions (id, runtime_id, version, install_path, is_default, status) VALUES (?, ?, ?, ?, ?, 'installed')`,
				id, rt.ID, d.Version, d.InstallPath, boolToInt(d.IsDefault),
			); err != nil {
				return Runtime{}, fmt.Errorf("insert runtime_version: %w", err)
			}
		case err != nil:
			return Runtime{}, fmt.Errorf("lookup runtime_version: %w", err)
		default:
			if _, err := tx.ExecContext(ctx,
				`UPDATE runtime_versions SET install_path = ?, is_default = ?, status = 'installed' WHERE id = ?`,
				d.InstallPath, boolToInt(d.IsDefault), existingID,
			); err != nil {
				return Runtime{}, fmt.Errorf("update runtime_version: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return Runtime{}, err
	}
	return rt, nil
}

// ListRuntimes returns every known Runtime with its Versions, ordered by
// kind then version.
func (r *Repository) ListRuntimes(ctx context.Context) ([]WithVersions, error) {
	rtRows, err := r.db.QueryContext(ctx, `SELECT id, kind, name FROM runtimes ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rtRows.Close()

	out := []WithVersions{}
	for rtRows.Next() {
		var rt Runtime
		if err := rtRows.Scan(&rt.ID, &rt.Kind, &rt.Name); err != nil {
			return nil, err
		}
		out = append(out, WithVersions{Runtime: rt})
	}
	if err := rtRows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		versions, err := r.listVersions(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Versions = versions
	}
	return out, nil
}

func (r *Repository) listVersions(ctx context.Context, runtimeID string) ([]Version, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, runtime_id, version, install_path, is_default, status FROM runtime_versions WHERE runtime_id = ? ORDER BY version`, runtimeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanVersion(row rowScanner) (Version, error) {
	var v Version
	var installPath sql.NullString
	var isDefault int
	if err := row.Scan(&v.ID, &v.RuntimeID, &v.Version, &installPath, &isDefault, &v.Status); err != nil {
		return Version{}, err
	}
	v.InstallPath = installPath.String
	v.IsDefault = isDefault != 0
	return v, nil
}

// SetDefaultVersion marks `version` as the default for the runtime
// identified by kind, unsetting any other default for that same runtime.
// Returns ErrVersionNotFound if no such (kind, version) pair is known.
func (r *Repository) SetDefaultVersion(ctx context.Context, kind, version string) error {
	rt, ok, err := r.getRuntimeByKind(ctx, kind)
	if err != nil {
		return err
	}
	if !ok {
		return ErrVersionNotFound
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM runtime_versions WHERE runtime_id = ? AND version = ?`, rt.ID, version).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrVersionNotFound
	}

	if _, err := tx.ExecContext(ctx, `UPDATE runtime_versions SET is_default = 0 WHERE runtime_id = ?`, rt.ID); err != nil {
		return fmt.Errorf("clear existing default: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runtime_versions SET is_default = 1 WHERE runtime_id = ? AND version = ?`, rt.ID, version); err != nil {
		return fmt.Errorf("set new default: %w", err)
	}
	return tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
