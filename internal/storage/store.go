package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/google/uuid"
)

type Config struct {
	Token string `json:"token"`
	Port  int    `json:"port"`
	CreatedAt time.Time `json:"createdAt"`
	// SessionSecret signs short-lived session tokens issued on top of Token
	// (see internal/security.SessionAuth). Generated once and persisted;
	// rotating it invalidates all outstanding sessions.
	SessionSecret string `json:"sessionSecret,omitempty"`
}

type Store struct {
	cfgDir string
	db     *sql.DB
	fs     *security.Sandbox
}

// DB exposes the underlying SQLite connection for repositories in later
// phases (Project, Server, Deployment, ...) that don't yet have their own
// Store-level wrapper.
func (s *Store) DB() *sql.DB { return s.db }

type App struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Command string `json:"command"`
	Args []string `json:"args"`
	Cwd string `json:"cwd"`
	Env map[string]string `json:"env"`
	PID int `json:"pid"`
	Status string `json:"status"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	ExitedAt *time.Time `json:"exitedAt,omitempty"`
	Code *int `json:"code,omitempty"`
	LogFile string `json:"logFile"`
}

// NewStore creates a Store whose file-manager operations are sandboxed to
// the user's home directory (no Project concept exists yet to scope it
// more tightly to).
func NewStore(cfgDir string) (*Store, error) {
	root, err := os.UserHomeDir()
	if err != nil {
		root = cfgDir
	}
	return NewStoreWithRoot(cfgDir, root)
}

// NewStoreWithRoot is like NewStore but lets callers (mainly tests) choose
// the file-manager sandbox root explicitly.
func NewStoreWithRoot(cfgDir, fileRoot string) (*Store, error) {
	db, err := OpenSQLite(cfgDir)
	if err != nil {
		return nil, fmt.Errorf("open storage: %w", err)
	}
	if err := importLegacyApps(db, cfgDir); err != nil {
		log.Printf("volt: warning: failed to import legacy apps.json: %v", err)
	}
	s := &Store{cfgDir: cfgDir, db: db}
	sb, err := security.NewSandbox(fileRoot)
	if err != nil {
		sb, _ = security.NewSandbox(cfgDir)
	}
	s.fs = sb
	return s, nil
}

// legacyConfigDirNames are prior config-directory names Volt (né VoltPanel)
// has used. On first run under a new name, matching data is imported into
// the new directory; the legacy directory itself is never modified.
var legacyConfigDirNames = []string{".alresia-voltpanel", ".alresia-volt"}

func EnsureDirs() (string, error) {
	home, err := os.UserHomeDir(); if err != nil { return "", err }
	d := filepath.Join(home, ".volt")
	if err := migrateLegacyDir(home, d); err != nil {
		log.Printf("volt: warning: failed to migrate legacy config dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(d, "logs"), 0o755); err != nil { return "", err }
	if err := os.MkdirAll(filepath.Join(d, "runtime"), 0o755); err != nil { return "", err }
	return d, nil
}

// migrateLegacyDir copies config.json/apps.json from the first legacy
// config directory it finds into newDir, once. It is a no-op if newDir
// already exists (already migrated, or already in active use) and it never
// deletes or modifies the legacy directory.
func migrateLegacyDir(home, newDir string) error {
	if _, err := os.Stat(newDir); err == nil {
		return nil
	}
	for _, legacyName := range legacyConfigDirNames {
		legacy := filepath.Join(home, legacyName)
		info, err := os.Stat(legacy)
		if err != nil || !info.IsDir() {
			continue
		}
		if err := os.MkdirAll(newDir, 0o755); err != nil {
			return err
		}
		for _, f := range []string{"config.json", "apps.json"} {
			b, err := os.ReadFile(filepath.Join(legacy, f))
			if err != nil {
				continue
			}
			if err := os.WriteFile(filepath.Join(newDir, f), b, 0o600); err != nil {
				return err
			}
		}
		log.Printf("volt: imported legacy config from %s into %s (original left untouched)", legacy, newDir)
		return nil
	}
	return nil
}

func configPath(dir string) string { return filepath.Join(dir, "config.json") }
func appsPath(dir string) string { return filepath.Join(dir, "apps.json") }
func (s *Store) LogDir() string { return filepath.Join(s.cfgDir, "logs") }

func LoadOrInitConfig() (Config, error) {
	d, err := EnsureDirs(); if err != nil { return Config{}, err }
	p := configPath(d)
	if _, err := os.Stat(p); err == nil {
		b, err := os.ReadFile(p); if err != nil { return Config{}, err }
		var c Config; if err := json.Unmarshal(b, &c); err != nil { return Config{}, err }
		if c.SessionSecret == "" {
			// Backfill for configs written before session auth existed.
			secret, err := security.NewSessionSecret()
			if err != nil { return Config{}, err }
			c.SessionSecret = secret
			if err := SaveConfig(c); err != nil { return Config{}, err }
		}
		return c, nil
	}
	secret, err := security.NewSessionSecret()
	if err != nil { return Config{}, err }
	c := Config{Token: uuid.NewString(), Port: 7788, CreatedAt: time.Now(), SessionSecret: secret}
	if err := SaveConfig(c); err != nil { return Config{}, err }
	return c, nil
}

func SaveConfig(c Config) error {
	d, err := EnsureDirs(); if err != nil { return err }
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(configPath(d), b, 0o600)
}

// UpsertApp, GetApp and ListApps are backed by SQLite (see services_repo.go)
// as of Phase 1. Signatures are unchanged so internal/agent.Manager, the
// sole caller, needed no changes -- Phase 5 replaces this whole App/Manager
// concept with the formalized Service domain entity.
func (s *Store) UpsertApp(a App) error { return upsertService(s.db, a) }

func (s *Store) GetApp(id string) (App, bool) {
	a, ok, err := getService(s.db, id)
	if err != nil {
		log.Printf("volt: GetApp(%s): %v", id, err)
		return App{}, false
	}
	return a, ok
}

func (s *Store) ListApps() []App {
	out, err := listServices(s.db)
	if err != nil {
		log.Printf("volt: ListApps: %v", err)
		return nil
	}
	return out
}

type FileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	IsDir bool  `json:"isDir"`
}

func (s *Store) ListPath(p string) ([]FileEntry, error) {
	real, err := s.fs.Resolve(p)
	if err != nil { return nil, err }
	entries, err := os.ReadDir(real)
	if err != nil { return nil, err }
	out := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, FileEntry{ Name: e.Name(), Path: filepath.Join(real, e.Name()), IsDir: e.IsDir() })
	}
	return out, nil
}

func (s *Store) WriteFile(p string, b []byte) error {
	real, err := s.fs.Resolve(p)
	if err != nil { return err }
	return os.WriteFile(real, b, 0o644)
}

func (s *Store) DeletePath(p string) error {
	real, err := s.fs.Resolve(p)
	if err != nil { return err }
	if real == s.fs.Root() {
		return fmt.Errorf("refusing to delete the file-manager root")
	}
	return os.RemoveAll(real)
}

func (s *Store) Mkdir(p string) error {
	real, err := s.fs.Resolve(p)
	if err != nil { return err }
	return os.MkdirAll(real, 0o755)
}

func (s *Store) Move(src, dst string) error {
	realSrc, err := s.fs.Resolve(src)
	if err != nil { return err }
	realDst, err := s.fs.Resolve(dst)
	if err != nil { return err }
	return os.Rename(realSrc, realDst)
}

func (s *Store) Copy(src, dst string) error {
	realSrc, err := s.fs.Resolve(src)
	if err != nil { return err }
	realDst, err := s.fs.Resolve(dst)
	if err != nil { return err }
	info, err := os.Stat(realSrc)
	if err != nil { return err }
	if info.IsDir() {
		return fmt.Errorf("copying directories is not yet supported")
	}
	b, err := os.ReadFile(realSrc)
	if err != nil { return err }
	return os.WriteFile(realDst, b, info.Mode())
}
