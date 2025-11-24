package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

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

func NewStore(cfgDir string) *Store { s := &Store{cfgDir: cfgDir, apps: map[string]App{}}; s.loadApps(); return s }

func EnsureDirs() (string, error) {
	home, err := os.UserHomeDir(); if err != nil { return "", err }
	d := filepath.Join(home, ".alresia-voltpanel")
	if err := os.MkdirAll(filepath.Join(d, "logs"), 0o755); err != nil { return "", err }
	if err := os.MkdirAll(filepath.Join(d, "runtime"), 0o755); err != nil { return "", err }
	return d, nil
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
	if p == "" { return nil, fmt.Errorf("path required") }
	entries, err := os.ReadDir(p)
	if err != nil { return nil, err }
	out := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, FileEntry{ Name: e.Name(), Path: filepath.Join(p, e.Name()), IsDir: e.IsDir() })
	}
	return out, nil
}

func (s *Store) WriteFile(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }
func (s *Store) DeletePath(p string) error { return os.RemoveAll(p) }
