// Package server implements Server as a first-class entity (§17 Phase 7 of
// the implementation plan): CRUD over the `servers` table SQLite migration
// 0001 already created, plus TestConnection -- the connection-test-before-
// destructive-action check §9's server-security row calls for, used by the
// API layer before offering any real remote action (exec, file browse,
// deploy, ...).
//
// This package depends only on internal/providers' RemoteProvider
// interface, never a concrete provider package (internal/providers/remote/
// ssh) -- matching §7's architecture rule that domain services depend on
// the interface + registry, not a specific implementation.
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/google/uuid"
)

// ErrNotFound is returned by Get/Delete/TestConnection when no server with
// the given id exists.
var ErrNotFound = errors.New("server not found")

// Server mirrors the `servers` table (§6 of the plan).
type Server struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Hostname        string     `json:"hostname"`
	Port            int        `json:"port"`
	Username        string     `json:"username"`
	AuthMethod      string     `json:"authMethod"` // "agent" | "key"
	SecretRef       string     `json:"secretRef,omitempty"`
	LastConnectedAt *time.Time `json:"lastConnectedAt,omitempty"`
	OSInfo          string     `json:"osInfo,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// ToProviderServer converts to the minimal shape providers.RemoteProvider
// operates on.
func (s Server) ToProviderServer() providers.Server {
	return providers.Server{
		ID:         s.ID,
		Hostname:   s.Hostname,
		Port:       s.Port,
		Username:   s.Username,
		AuthMethod: s.AuthMethod,
		SecretRef:  s.SecretRef,
	}
}

// CreateRequest is what callers (the API layer) provide to register a new
// server. SecretRef is set by the caller only after storing key material
// through the Secret abstraction (internal/security.SecretStore) --
// this package never handles plaintext key/credential bytes itself.
type CreateRequest struct {
	// ID, when non-empty, is used as the new server's id instead of
	// generating a fresh one -- the API layer needs this for key-auth
	// servers, where the secret has to be stored (under the server's
	// eventual id, for the keychain account name) *before* the server row
	// exists, via NewID().
	ID         string
	Name       string
	Hostname   string
	Port       int
	Username   string
	AuthMethod string // "agent" (default) or "key"
	SecretRef  string
}

// NewID generates a fresh server id, for callers that need to know the id
// before calling Create (see CreateRequest.ID).
func NewID() string { return uuid.NewString() }

// Repository is the SQLite-backed CRUD layer for Server.
type Repository struct {
	db *sql.DB
}

// NewRepository builds a Repository over an already-migrated *sql.DB
// (internal/storage.Store.DB()).
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Create validates req and inserts a new server row.
func (r *Repository) Create(req CreateRequest) (Server, error) {
	if req.Name == "" {
		return Server{}, fmt.Errorf("server name is required")
	}
	if req.Hostname == "" {
		return Server{}, fmt.Errorf("server hostname is required")
	}
	if req.Username == "" {
		return Server{}, fmt.Errorf("server username is required")
	}
	port := req.Port
	if port <= 0 {
		port = 22
	}
	authMethod := req.AuthMethod
	if authMethod == "" {
		authMethod = "agent"
	}
	if authMethod != "agent" && authMethod != "key" {
		return Server{}, fmt.Errorf("unsupported auth_method %q (want %q or %q)", authMethod, "agent", "key")
	}
	if authMethod == "key" && req.SecretRef == "" {
		return Server{}, fmt.Errorf("auth_method %q requires a stored secret_ref", "key")
	}

	id := req.ID
	if id == "" {
		id = uuid.NewString()
	}
	now := time.Now().UTC()
	s := Server{
		ID:         id,
		Name:       req.Name,
		Hostname:   req.Hostname,
		Port:       port,
		Username:   req.Username,
		AuthMethod: authMethod,
		SecretRef:  req.SecretRef,
		CreatedAt:  now,
	}
	_, err := r.db.Exec(
		`INSERT INTO servers (id, name, hostname, port, username, auth_method, secret_ref, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Hostname, s.Port, s.Username, s.AuthMethod, nullable(s.SecretRef), now.Format(time.RFC3339),
	)
	if err != nil {
		return Server{}, err
	}
	return s, nil
}

// Get fetches one server by id.
func (r *Repository) Get(id string) (Server, error) {
	row := r.db.QueryRow(`
		SELECT id, name, hostname, port, username, auth_method, COALESCE(secret_ref, ''),
		       last_connected_at, COALESCE(os_info, ''), created_at
		FROM servers WHERE id = ?
	`, id)
	return scanServer(row)
}

// List returns every server, oldest first.
func (r *Repository) List() ([]Server, error) {
	rows, err := r.db.Query(`
		SELECT id, name, hostname, port, username, auth_method, COALESCE(secret_ref, ''),
		       last_connected_at, COALESCE(os_info, ''), created_at
		FROM servers ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Delete removes a server row. It does not revoke or delete any secret the
// server referenced -- callers (the API layer) that also want the secret
// gone should call SecretStore.Delete themselves, since the two are
// intentionally decoupled (a secret might be reused, e.g. a shared key
// across multiple servers).
func (r *Repository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM servers WHERE id = ?`, id)
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

// TestConnection is the lightweight connectivity check §9's server-security
// row calls for: connect, run a harmless no-op command, close. It never
// runs anything destructive and is meant to be called by the API layer
// before offering any real remote action. On success it records
// last_connected_at (and os_info, best-effort, via `uname -a`).
func (r *Repository) TestConnection(ctx context.Context, provider providers.RemoteProvider, id string) error {
	s, err := r.Get(id)
	if err != nil {
		return err
	}
	sess, err := provider.Connect(ctx, s.ToProviderServer())
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer sess.Close()

	if _, stderr, err := sess.Exec(ctx, "true"); err != nil {
		return fmt.Errorf("exec test command: %w (stderr: %s)", err, stderr)
	}

	osInfo := ""
	if out, _, err := sess.Exec(ctx, "uname -a"); err == nil {
		osInfo = trimTrailingNewline(out)
	}

	now := time.Now().UTC()
	if _, err := r.db.Exec(
		`UPDATE servers SET last_connected_at = ?, os_info = ? WHERE id = ?`,
		now.Format(time.RFC3339), nullable(osInfo), id,
	); err != nil {
		return fmt.Errorf("record connection result: %w", err)
	}
	return nil
}

func trimTrailingNewline(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanServer(row rowScanner) (Server, error) {
	var s Server
	var createdAt string
	var lastConnectedAt sql.NullString
	if err := row.Scan(&s.ID, &s.Name, &s.Hostname, &s.Port, &s.Username, &s.AuthMethod, &s.SecretRef,
		&lastConnectedAt, &s.OSInfo, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Server{}, ErrNotFound
		}
		return Server{}, err
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		s.CreatedAt = t
	}
	if lastConnectedAt.Valid {
		if t, err := time.Parse(time.RFC3339, lastConnectedAt.String); err == nil {
			s.LastConnectedAt = &t
		}
	}
	return s, nil
}
