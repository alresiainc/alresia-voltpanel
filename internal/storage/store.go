package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/google/uuid"
)

type Config struct {
	Token string `json:"token"`
	Port  int    `json:"port"`
	CreatedAt time.Time `json:"createdAt"`
}

type Store struct {
	cfgDir string
	mu sync.Mutex
	apps map[string]App
	fs *security.Sandbox
}

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
func NewStore(cfgDir string) *Store {
	root, err := os.UserHomeDir()
	if err != nil {
		root = cfgDir
	}
	return NewStoreWithRoot(cfgDir, root)
}

// NewStoreWithRoot is like NewStore but lets callers (mainly tests) choose
// the file-manager sandbox root explicitly.
func NewStoreWithRoot(cfgDir, fileRoot string) *Store {
	s := &Store{cfgDir: cfgDir, apps: map[string]App{}}
	s.loadApps()
	sb, err := security.NewSandbox(fileRoot)
	if err != nil {
		sb, _ = security.NewSandbox(cfgDir)
	}
	s.fs = sb
	return s
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
		return c, nil
	}
	c := Config{Token: uuid.NewString(), Port: 7788, CreatedAt: time.Now()}
	if err := SaveConfig(c); err != nil { return Config{}, err }
	return c, nil
}

func SaveConfig(c Config) error {
	d, err := EnsureDirs(); if err != nil { return err }
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(configPath(d), b, 0o600)
}

func (s *Store) loadApps() error {
	p := appsPath(s.cfgDir)
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) { s.apps = map[string]App{}; return nil }
		return err
	}
	var out struct{ Apps []App `json:"apps"` }
	if err := json.Unmarshal(b, &out); err != nil { return err }
	m := map[string]App{}
	for _, a := range out.Apps { m[a.ID] = a }
	s.apps = m
	return nil
}

func (s *Store) saveApps() error {
	apps := make([]App, 0, len(s.apps))
	for _, a := range s.apps { apps = append(apps, a) }
	b, _ := json.MarshalIndent(struct{ Apps []App `json:"apps"` }{apps}, "", "  ")
	return os.WriteFile(appsPath(s.cfgDir), b, 0o644)
}

func (s *Store) UpsertApp(a App) error {
	s.mu.Lock(); defer s.mu.Unlock()
	s.apps[a.ID] = a
	return s.saveApps()
}

func (s *Store) GetApp(id string) (App, bool) { s.mu.Lock(); defer s.mu.Unlock(); a, ok := s.apps[id]; return a, ok }
func (s *Store) ListApps() []App {
	s.mu.Lock(); defer s.mu.Unlock()
	out := make([]App, 0, len(s.apps))
	for _, a := range s.apps { out = append(out, a) }
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
