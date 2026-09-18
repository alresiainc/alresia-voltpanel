// Package integration implements Integration (§6/§17 Phase 8 of the
// implementation plan) as a first-class entity backed by the `integrations`
// table (internal/storage/migrations/0001_init.sql). An Integration row
// never holds a credential itself -- only a SecretRef pointing at a row in
// the `secrets` table, resolved through internal/security.SecretStore.
package integration

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by Get/Delete when no integration with the given
// id exists.
var ErrNotFound = errors.New("integration not found")

// Integration mirrors the `integrations` table. SecretRef is intentionally
// excluded from JSON -- it's an opaque id into the `secrets` table, not a
// credential itself, but callers (API responses) have no legitimate need to
// see it either, so it stays server-side only.
type Integration struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	AccountRef string    `json:"accountRef"`
	SecretRef  string    `json:"-"`
	Scopes     []string  `json:"scopes"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Repository is the SQLite-backed CRUD layer for Integration.
type Repository struct {
	db *sql.DB
}

// NewRepository builds a Repository over an already-migrated *sql.DB.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Create inserts a new integration row. secretRef must already point at a
// row created via internal/security.SecretStore.Put -- this package never
// handles plaintext credentials itself.
func (r *Repository) Create(kind, accountRef, secretRef string, scopes []string) (Integration, error) {
	if scopes == nil {
		scopes = []string{}
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return Integration{}, err
	}
	now := time.Now().UTC()
	i := Integration{
		ID:         uuid.NewString(),
		Kind:       kind,
		AccountRef: accountRef,
		SecretRef:  secretRef,
		Scopes:     scopes,
		CreatedAt:  now,
	}
	_, err = r.db.Exec(
		`INSERT INTO integrations (id, kind, account_ref, secret_ref, scopes, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		i.ID, i.Kind, i.AccountRef, i.SecretRef, string(scopesJSON), now.Format(time.RFC3339),
	)
	if err != nil {
		return Integration{}, err
	}
	return i, nil
}

// Get fetches one integration by id.
func (r *Repository) Get(id string) (Integration, error) {
	row := r.db.QueryRow(
		`SELECT id, kind, COALESCE(account_ref, ''), COALESCE(secret_ref, ''), scopes, created_at FROM integrations WHERE id = ?`,
		id,
	)
	return scanIntegration(row)
}

// List returns every integration, oldest first.
func (r *Repository) List() ([]Integration, error) {
	rows, err := r.db.Query(
		`SELECT id, kind, COALESCE(account_ref, ''), COALESCE(secret_ref, ''), scopes, created_at FROM integrations ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Integration{}
	for rows.Next() {
		i, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// Delete removes an integration row. It does not delete the underlying
// secret -- callers are expected to also call SecretStore.Delete(secretRef)
// (fetched via Get before calling this) so both halves are cleaned up.
func (r *Repository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM integrations WHERE id = ?`, id)
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

func scanIntegration(row rowScanner) (Integration, error) {
	var i Integration
	var scopesJSON, createdAt string
	if err := row.Scan(&i.ID, &i.Kind, &i.AccountRef, &i.SecretRef, &scopesJSON, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Integration{}, ErrNotFound
		}
		return Integration{}, err
	}
	if err := json.Unmarshal([]byte(scopesJSON), &i.Scopes); err != nil {
		return Integration{}, fmt.Errorf("decode scopes: %w", err)
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		i.CreatedAt = t
	}
	return i, nil
}
