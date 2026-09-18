package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// OpenSQLite opens (creating if needed) the daemon's structured-state
// database under cfgDir and applies any migrations that haven't run yet.
// Secrets never live here -- see internal/security and the Secret table's
// storage_backend column.
func OpenSQLite(cfgDir string) (*sql.DB, error) {
	path := filepath.Join(cfgDir, "volt.db")
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // sqlite: one writer at a time keeps this simple and correct
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (datetime('now')))`); err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := versionOf(name)
		if err != nil {
			return err
		}
		var applied int
		if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func versionOf(filename string) (int, error) {
	n := strings.SplitN(filename, "_", 2)[0]
	v, err := strconv.Atoi(n)
	if err != nil {
		return 0, fmt.Errorf("migration filename %q must start with a numeric version: %w", filename, err)
	}
	return v, nil
}
