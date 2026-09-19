// Package dbconn implements DBConnection as a first-class entity (the
// database-admin tool's "saved connection" -- § "something like Adminer"):
// CRUD over the `db_connections` table, mirroring internal/domain/server's
// existing pattern for a credentialed remote connection almost exactly.
// The actual SQL work (connecting, browsing, running queries) lives in
// internal/providers/dbadmin -- this package only ever holds metadata.
// Passwords never touch this package: callers store them through
// internal/security.SecretStore first and pass in the resulting
// SecretRef, the same two-step flow servers.go already uses for SSH keys.
package dbconn

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by Get/Delete when no connection with the given
// id exists.
var ErrNotFound = errors.New("db connection not found")

// Kind enumerates the database engines the admin tool speaks.
type Kind string

const (
	KindMySQL    Kind = "mysql"
	KindPostgres Kind = "postgres"
)

// DBConnection mirrors the `db_connections` table.
type DBConnection struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Kind         Kind      `json:"kind"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	Username     string    `json:"username"`
	DatabaseName string    `json:"databaseName,omitempty"`
	SecretRef    string    `json:"secretRef,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// CreateRequest is what the API layer provides to register a new
// connection -- SecretRef is set by the caller only after storing the
// password through SecretStore, same as server.CreateRequest.
type CreateRequest struct {
	ID           string // set by the caller via NewID() so the secret can be stored under it first
	Name         string
	Kind         Kind
	Host         string
	Port         int
	Username     string
	DatabaseName string
	SecretRef    string
}

// NewID generates a fresh connection id, for callers that need to know
// the id before Create (to store the password under it first).
func NewID() string { return uuid.NewString() }

// Repository is the SQLite-backed CRUD layer for DBConnection.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(req CreateRequest) (DBConnection, error) {
	if req.Name == "" || req.Host == "" || req.Username == "" {
		return DBConnection{}, fmt.Errorf("dbconn: name, host, and username are required")
	}
	if req.Kind != KindMySQL && req.Kind != KindPostgres {
		return DBConnection{}, fmt.Errorf("dbconn: unknown kind %q", req.Kind)
	}
	id := req.ID
	if id == "" {
		id = NewID()
	}
	c := DBConnection{
		ID: id, Name: req.Name, Kind: req.Kind, Host: req.Host, Port: req.Port,
		Username: req.Username, DatabaseName: req.DatabaseName, SecretRef: req.SecretRef,
		CreatedAt: time.Now().UTC(),
	}
	_, err := r.db.Exec(`
		INSERT INTO db_connections (id, name, kind, host, port, username, database_name, secret_ref, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, c.ID, c.Name, string(c.Kind), c.Host, c.Port, c.Username, nullableString(c.DatabaseName), nullableString(c.SecretRef), c.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return DBConnection{}, err
	}
	return c, nil
}

func (r *Repository) Get(id string) (DBConnection, error) {
	row := r.db.QueryRow(`
		SELECT id, name, kind, host, port, username, COALESCE(database_name, ''), COALESCE(secret_ref, ''), created_at
		FROM db_connections WHERE id = ?
	`, id)
	return scanConn(row)
}

func (r *Repository) List() ([]DBConnection, error) {
	rows, err := r.db.Query(`
		SELECT id, name, kind, host, port, username, COALESCE(database_name, ''), COALESCE(secret_ref, ''), created_at
		FROM db_connections ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBConnection{}
	for rows.Next() {
		c, err := scanConn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM db_connections WHERE id = ?`, id)
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

type rowScanner interface {
	Scan(dest ...any) error
}

func scanConn(row rowScanner) (DBConnection, error) {
	var c DBConnection
	var kind, createdAt string
	if err := row.Scan(&c.ID, &c.Name, &kind, &c.Host, &c.Port, &c.Username, &c.DatabaseName, &c.SecretRef, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DBConnection{}, ErrNotFound
		}
		return DBConnection{}, err
	}
	c.Kind = Kind(kind)
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		c.CreatedAt = t
	}
	return c, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
