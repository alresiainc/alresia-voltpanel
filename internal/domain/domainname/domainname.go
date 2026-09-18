// Package domainname implements Domain and Certificate as first-class
// entities (§17 Phase 4 of the implementation plan): a repository over the
// `domains`/`certificates` tables internal/storage/migrations/0001_init.sql
// already created. Named "domainname" (rather than "domain") specifically
// to avoid clashing with Go's own vocabulary/keyword-adjacent stdlib
// package naming conventions, per the plan's own note on this.
//
// This package holds only persisted metadata -- hostname, which project it
// belongs to, certificate validity window/status. It never stores key
// material (that lives on disk under internal/providers/ssl/localca's Dir,
// per §9.7/§23: secrets don't belong in the structured-state DB) and it
// never talks to the filesystem or shells out -- that's
// internal/providers/domainprovider/hosts and internal/providers/ssl/localca's
// job. internal/api/v1/domains.go and ssl.go are what glue this repository
// together with those providers.
package domainname

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by Get/Delete/GetCertificate when no matching
// row exists.
var ErrNotFound = errors.New("domainname: not found")

// ErrHostnameTaken is returned by CreateDomain when hostname is already
// registered (the `domains.hostname` column is UNIQUE) -- surfaced as a
// distinct error so callers/handlers can return 409 rather than a generic
// 500 for what is an entirely expected conflict.
var ErrHostnameTaken = errors.New("domainname: hostname is already registered")

// Domain mirrors the `domains` table (§6).
type Domain struct {
	ID         string    `json:"id"`
	Hostname   string    `json:"hostname"`
	ProjectID  string    `json:"projectId,omitempty"`
	Port       int       `json:"port,omitempty"`
	Provider   string    `json:"provider"`
	SSLEnabled bool      `json:"sslEnabled"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Certificate mirrors the `certificates` table (§6). CAID is a free-text
// identifier for which local CA issued it (not a foreign key -- the schema
// doesn't constrain it, and a machine may reasonably reuse or rotate CAs
// over time); the actual PEM material lives under localca.Provider's Dir,
// addressed by this same ID.
type Certificate struct {
	ID        string    `json:"id"`
	DomainID  string    `json:"domainId"`
	CAID      string    `json:"caId,omitempty"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	Status    string    `json:"status"`
}

// Repository is the SQLite-backed CRUD layer for Domain and Certificate.
type Repository struct {
	db *sql.DB
}

// NewRepository builds a Repository over an already-migrated *sql.DB
// (internal/storage.Store.DB()).
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// CreateDomain validates hostname is non-empty and inserts a new domains
// row. projectID may be empty (stored as NULL -- the domains.project_id
// column is a nullable foreign key, and SQLite's foreign_keys pragma is on
// in this daemon, so an empty string would fail the FK check where NULL
// does not).
func (r *Repository) CreateDomain(hostname, projectID string, port int) (Domain, error) {
	if hostname == "" {
		return Domain{}, fmt.Errorf("domainname: hostname must not be empty")
	}
	now := time.Now().UTC()
	d := Domain{
		ID:        uuid.NewString(),
		Hostname:  hostname,
		ProjectID: projectID,
		Port:      port,
		Provider:  "hosts",
		Enabled:   true,
		CreatedAt: now,
	}
	_, err := r.db.Exec(`
		INSERT INTO domains (id, hostname, project_id, port, provider, ssl_enabled, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, 0, 1, ?)
	`, d.ID, d.Hostname, nullableString(d.ProjectID), nullablePort(d.Port), d.Provider, d.CreatedAt.Format(time.RFC3339))
	if err != nil {
		if isUniqueConstraintErr(err) {
			return Domain{}, ErrHostnameTaken
		}
		return Domain{}, err
	}
	return d, nil
}

// GetDomain fetches one domain by id.
func (r *Repository) GetDomain(id string) (Domain, error) {
	row := r.db.QueryRow(`
		SELECT id, hostname, COALESCE(project_id, ''), COALESCE(port, 0), provider, ssl_enabled, enabled, created_at
		FROM domains WHERE id = ?
	`, id)
	return scanDomain(row)
}

// ListDomains returns every registered domain, oldest first.
func (r *Repository) ListDomains() ([]Domain, error) {
	rows, err := r.db.Query(`
		SELECT id, hostname, COALESCE(project_id, ''), COALESCE(port, 0), provider, ssl_enabled, enabled, created_at
		FROM domains ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteDomain removes a domain row. Callers (internal/api/v1/domains.go)
// are responsible for also removing the corresponding hosts-file entry via
// a DomainProvider *before* calling this, so the two stay in sync.
func (r *Repository) DeleteDomain(id string) error {
	res, err := r.db.Exec(`DELETE FROM domains WHERE id = ?`, id)
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

// SetSSLEnabled flips a domain's ssl_enabled flag -- called once a
// certificate has actually been issued for it.
func (r *Repository) SetSSLEnabled(id string, enabled bool) error {
	res, err := r.db.Exec(`UPDATE domains SET ssl_enabled = ? WHERE id = ?`, boolToInt(enabled), id)
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

// SetPort updates a domain's target port -- called once a bound project
// actually starts and is assigned a real listening port, so the port this
// domain reports reflects where it's really being proxied to rather than
// just the metadata a user typed in when creating it.
func (r *Repository) SetPort(id string, port int) error {
	res, err := r.db.Exec(`UPDATE domains SET port = ? WHERE id = ?`, nullablePort(port), id)
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

// ClearProject un-binds a domain from whatever project it pointed at
// (project_id -> NULL) without deleting the domain itself -- used when the
// project is deleted out from under it, so the hostname mapping and any
// issued certificate survive; only the (now-meaningless) project link and
// live port go away.
func (r *Repository) ClearProject(id string) error {
	_, err := r.db.Exec(`UPDATE domains SET project_id = NULL, port = NULL WHERE id = ?`, id)
	return err
}

// ListDomainsForProject returns every domain bound to projectID, in
// creation order -- used to sync the reverse proxy's routing table when a
// project starts, stops, or has a domain added/removed.
func (r *Repository) ListDomainsForProject(projectID string) ([]Domain, error) {
	rows, err := r.db.Query(`
		SELECT id, hostname, COALESCE(project_id, ''), COALESCE(port, 0), provider, ssl_enabled, enabled, created_at
		FROM domains WHERE project_id = ? ORDER BY created_at
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CreateCertificate persists a certificate row with an explicit id -- the
// caller (internal/api/v1/ssl.go) passes the same ID an SSLProvider's
// IssueCertificate returned, so the DB row and the on-disk PEM material
// under localca.Provider's Dir share one identifier.
func (r *Repository) CreateCertificate(id, domainID, caID string, notBefore, notAfter time.Time, status string) (Certificate, error) {
	if id == "" || domainID == "" {
		return Certificate{}, fmt.Errorf("domainname: certificate id and domainID must not be empty")
	}
	c := Certificate{ID: id, DomainID: domainID, CAID: caID, NotBefore: notBefore, NotAfter: notAfter, Status: status}
	_, err := r.db.Exec(`
		INSERT INTO certificates (id, domain_id, ca_id, not_before, not_after, status)
		VALUES (?, ?, ?, ?, ?, ?)
	`, c.ID, c.DomainID, nullableString(c.CAID), c.NotBefore.UTC().Format(time.RFC3339), c.NotAfter.UTC().Format(time.RFC3339), c.Status)
	if err != nil {
		return Certificate{}, err
	}
	return c, nil
}

// GetCertificate fetches one certificate by id.
func (r *Repository) GetCertificate(id string) (Certificate, error) {
	row := r.db.QueryRow(`
		SELECT id, domain_id, COALESCE(ca_id, ''), not_before, not_after, status
		FROM certificates WHERE id = ?
	`, id)
	return scanCertificate(row)
}

// ListCertificatesForDomain returns every certificate ever issued for a
// domain, oldest first (so the most recent -- the active one -- is last).
func (r *Repository) ListCertificatesForDomain(domainID string) ([]Certificate, error) {
	rows, err := r.db.Query(`
		SELECT id, domain_id, COALESCE(ca_id, ''), not_before, not_after, status
		FROM certificates WHERE domain_id = ? ORDER BY not_before
	`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Certificate{}
	for rows.Next() {
		c, err := scanCertificate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCertificateStatus updates a certificate's status and validity
// window in place -- used after Renew re-issues the same certificate id.
func (r *Repository) UpdateCertificateStatus(id, status string, notBefore, notAfter time.Time) error {
	res, err := r.db.Exec(`UPDATE certificates SET status = ?, not_before = ?, not_after = ? WHERE id = ?`,
		status, notBefore.UTC().Format(time.RFC3339), notAfter.UTC().Format(time.RFC3339), id)
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

func scanDomain(row rowScanner) (Domain, error) {
	var d Domain
	var sslEnabled, enabled int
	var createdAt string
	if err := row.Scan(&d.ID, &d.Hostname, &d.ProjectID, &d.Port, &d.Provider, &sslEnabled, &enabled, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Domain{}, ErrNotFound
		}
		return Domain{}, err
	}
	d.SSLEnabled = sslEnabled != 0
	d.Enabled = enabled != 0
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		d.CreatedAt = t
	}
	return d, nil
}

func scanCertificate(row rowScanner) (Certificate, error) {
	var c Certificate
	var notBefore, notAfter string
	if err := row.Scan(&c.ID, &c.DomainID, &c.CAID, &notBefore, &notAfter, &c.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Certificate{}, ErrNotFound
		}
		return Certificate{}, err
	}
	if t, err := time.Parse(time.RFC3339, notBefore); err == nil {
		c.NotBefore = t
	}
	if t, err := time.Parse(time.RFC3339, notAfter); err == nil {
		c.NotAfter = t
	}
	return c, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullablePort(p int) any {
	if p == 0 {
		return nil
	}
	return p
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueConstraintErr detects SQLite's UNIQUE-constraint-violation error
// without importing the sqlite driver package directly (modernc.org/sqlite
// wraps this as a plain error whose message contains this substring --
// checking the message is the driver-agnostic option other repositories in
// this codebase don't yet need, but domains.hostname is the first UNIQUE
// column a repository has had to handle post-insert).
func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed: UNIQUE")
}
