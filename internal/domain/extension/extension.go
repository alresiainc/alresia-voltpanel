// Package extension is the repository for the `extensions` SQLite table
// (internal/storage/migrations/0001_init.sql) plus the glue that turns a
// persisted row into a live provider: Enable loads the extension's
// subprocess via internal/pluginhost and registers it into the shared
// providers.Registry -- exactly alongside the built-in Node/PHP
// RuntimeProviders from Phase 2 -- and Disable unregisters it and stops
// the subprocess. This is Phase 11's "let external providers register
// without core changes" (§17): everything above the pluginhost.
// ExternalProvider boundary (this package, the API layer, the Registry)
// only ever sees the providers.RuntimeProvider interface.
package extension

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/alresiainc/alresia-voltpanel/internal/pluginhost"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/google/uuid"
)

// Extension is one row of the `extensions` table.
type Extension struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Kind    string `json:"kind"` // provider category: "runtime" is the only one Enable supports today
	// Source is the extension's own directory on disk (containing its
	// volt-extension.json manifest and executable) -- what
	// pluginhost.LoadExtension is pointed at when this extension is
	// enabled.
	Source      string   `json:"source"`
	Enabled     bool     `json:"enabled"`
	Permissions []string `json:"permissions"`
}

var (
	// ErrNotFound is returned by Get/Enable/Disable/Remove for an unknown id.
	ErrNotFound = errors.New("extension not found")
	// ErrUnsupportedKind is returned by Enable for a manifest `kind` this
	// build doesn't know how to register into a Registry yet. Only
	// "runtime" (providers.RuntimeProvider) is supported today, matching
	// the task's scope -- a later phase can widen this without changing
	// the table shape or this package's public API.
	ErrUnsupportedKind = errors.New("extension kind is not supported for registration")
)

// Repository persists Extension rows and, for enabled ones, tracks the
// live *pluginhost.ExternalProvider subprocess backing them.
type Repository struct {
	db       *sql.DB
	registry *providers.Registry

	mu     sync.Mutex
	loaded map[string]*pluginhost.ExternalProvider // extension id -> running subprocess
}

// NewRepository returns a Repository backed by db (typically
// storage.Store.DB()) that registers/unregisters providers into registry
// on Enable/Disable.
func NewRepository(db *sql.DB, registry *providers.Registry) *Repository {
	return &Repository{db: db, registry: registry, loaded: map[string]*pluginhost.ExternalProvider{}}
}

func scanExtension(row rowScanner) (Extension, error) {
	var ext Extension
	var enabled int
	var permissionsJSON string
	if err := row.Scan(&ext.ID, &ext.Name, &ext.Version, &ext.Kind, &ext.Source, &enabled, &permissionsJSON); err != nil {
		return Extension{}, err
	}
	ext.Enabled = enabled != 0
	if permissionsJSON != "" {
		if err := json.Unmarshal([]byte(permissionsJSON), &ext.Permissions); err != nil {
			return Extension{}, fmt.Errorf("decode permissions for extension %s: %w", ext.ID, err)
		}
	}
	return ext, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

// List returns every persisted extension, ordered by name.
func (r *Repository) List(ctx context.Context) ([]Extension, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, version, kind, source, enabled, permissions FROM extensions ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Extension{}
	for rows.Next() {
		ext, err := scanExtension(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ext)
	}
	return out, rows.Err()
}

// Get returns the extension with the given id. ok is false (with a nil
// error) when no such row exists.
func (r *Repository) Get(ctx context.Context, id string) (Extension, bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, name, version, kind, source, enabled, permissions FROM extensions WHERE id = ?`, id)
	ext, err := scanExtension(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Extension{}, false, nil
		}
		return Extension{}, false, err
	}
	return ext, true, nil
}

// Install registers a new extension from a local path: sourcePath is
// either an extension's directory (containing volt-extension.json) or the
// manifest file itself. The manifest is read and validated -- but the
// subprocess is never started here -- so the daemon knows exactly what
// it's being asked to run (name/version/kind/permissions) before agreeing
// to run anything; Enable is the explicit, separate action that actually
// launches it (§9.6's "never silent" pattern: install and enable are two
// distinct, deliberate steps).
//
// Installing from a URL (fetching and running arbitrary remote code) is
// explicitly deferred -- see internal/api/v1/extensions.go's comment; this
// only ever reads a path already present on the local filesystem.
func (r *Repository) Install(ctx context.Context, sourcePath string) (Extension, error) {
	manifest, dir, err := pluginhost.ReadManifest(sourcePath)
	if err != nil {
		return Extension{}, err
	}

	permissionsJSON, err := json.Marshal(manifest.Permissions)
	if err != nil {
		return Extension{}, err
	}

	ext := Extension{
		ID:          uuid.NewString(),
		Name:        manifest.Name,
		Version:     manifest.Version,
		Kind:        manifest.Kind,
		Source:      dir,
		Enabled:     false,
		Permissions: manifest.Permissions,
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO extensions (id, name, version, kind, source, enabled, permissions) VALUES (?, ?, ?, ?, ?, 0, ?)`,
		ext.ID, ext.Name, ext.Version, ext.Kind, ext.Source, string(permissionsJSON),
	); err != nil {
		return Extension{}, fmt.Errorf("insert extension: %w", err)
	}
	return ext, nil
}

// Remove disables the extension first (if currently enabled -- stopping
// its subprocess and unregistering it) and then deletes its row.
func (r *Repository) Remove(ctx context.Context, id string) error {
	ext, ok, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if ext.Enabled {
		if _, err := r.Disable(ctx, id); err != nil {
			return fmt.Errorf("disable before remove: %w", err)
		}
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM extensions WHERE id = ?`, id); err != nil {
		return err
	}
	return nil
}

// loadAndRegister launches ext's subprocess via pluginhost.LoadExtension
// and registers it into the Registry under its own advertised Kind(). It
// never touches the database -- callers (Enable, LoadEnabled) decide
// whether/how to persist the result.
func (r *Repository) loadAndRegister(ext Extension) (*pluginhost.ExternalProvider, error) {
	if ext.Kind != "runtime" {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedKind, ext.Kind)
	}

	r.mu.Lock()
	if existing, already := r.loaded[ext.ID]; already {
		_ = existing.Close()
		delete(r.loaded, ext.ID)
	}
	r.mu.Unlock()

	ep, err := pluginhost.LoadExtension(ext.Source)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.loaded[ext.ID] = ep
	r.mu.Unlock()
	r.registry.RegisterRuntime(ep)
	return ep, nil
}

// Enable loads the extension's subprocess and registers it into the
// shared providers.Registry, then persists enabled=1. If the extension
// was already enabled (e.g. re-enabling after a manual disable), its
// previous subprocess is closed first rather than leaking a duplicate
// one.
func (r *Repository) Enable(ctx context.Context, id string) (Extension, error) {
	ext, ok, err := r.Get(ctx, id)
	if err != nil {
		return Extension{}, err
	}
	if !ok {
		return Extension{}, ErrNotFound
	}

	if _, err := r.loadAndRegister(ext); err != nil {
		return Extension{}, err
	}

	if _, err := r.db.ExecContext(ctx, `UPDATE extensions SET enabled = 1 WHERE id = ?`, id); err != nil {
		return Extension{}, err
	}
	ext.Enabled = true
	return ext, nil
}

// Disable unregisters the extension's provider from the Registry and
// stops its subprocess, then persists enabled=0. Safe to call on an
// extension that isn't currently loaded (e.g. after a daemon restart that
// never re-loaded it) -- it just persists enabled=0 in that case.
func (r *Repository) Disable(ctx context.Context, id string) (Extension, error) {
	ext, ok, err := r.Get(ctx, id)
	if err != nil {
		return Extension{}, err
	}
	if !ok {
		return Extension{}, ErrNotFound
	}

	r.mu.Lock()
	ep, running := r.loaded[id]
	delete(r.loaded, id)
	r.mu.Unlock()

	if running {
		r.registry.UnregisterRuntime(ep.Kind())
		_ = ep.Close()
	}

	if _, err := r.db.ExecContext(ctx, `UPDATE extensions SET enabled = 0 WHERE id = ?`, id); err != nil {
		return Extension{}, err
	}
	ext.Enabled = false
	return ext, nil
}

// LoadEnabled re-launches every persisted-enabled extension's subprocess
// and registers it, intended to be called once at daemon startup so a
// restart doesn't silently lose previously-enabled extensions (mirroring
// how autostart services are re-launched -- internal/domain/service's
// StartAutostart). A failure loading one extension (missing source,
// unsupported version, misbehaving subprocess, ...) is logged and
// skipped rather than treated as fatal: a broken extension must never
// prevent the daemon itself from starting.
func (r *Repository) LoadEnabled(ctx context.Context) {
	exts, err := r.List(ctx)
	if err != nil {
		log.Printf("volt: extension: listing extensions at startup: %v", err)
		return
	}
	for _, ext := range exts {
		if !ext.Enabled {
			continue
		}
		if _, err := r.loadAndRegister(ext); err != nil {
			log.Printf("volt: extension %q failed to load at startup: %v", ext.Name, err)
		}
	}
}

// CloseAll stops every currently-loaded extension subprocess without
// touching persisted enabled state -- for a clean daemon shutdown path.
func (r *Repository) CloseAll() {
	r.mu.Lock()
	loaded := r.loaded
	r.loaded = map[string]*pluginhost.ExternalProvider{}
	r.mu.Unlock()

	for id, ep := range loaded {
		r.registry.UnregisterRuntime(ep.Kind())
		if err := ep.Close(); err != nil {
			log.Printf("volt: extension %s: error closing subprocess: %v", id, err)
		}
	}
}
