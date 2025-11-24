package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/storage"
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
)

type StartRequest struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
}

type ProcInfo struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Cwd       string            `json:"cwd"`
	Env       map[string]string `json:"env"`
	PID       int               `json:"pid"`
	Status    string            `json:"status"`
	StartedAt time.Time         `json:"startedAt"`
	ExitedAt  *time.Time        `json:"exitedAt,omitempty"`
	Code      *int              `json:"code,omitempty"`
	LogFile   string            `json:"logFile"`
}

func NewManager(store *storage.Store) *Manager {
	return &Manager{store: store, procs: map[string]*proc{}}
}

type Manager struct {
	store *storage.Store
	mu    sync.Mutex
	procs map[string]*proc
}

type proc struct {
	info ProcInfo
	cmd  *exec.Cmd
}

func (m *Manager) Start(req StartRequest, hub *ws.Hub) (ProcInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.procs[req.ID]; ok {
		return ProcInfo{}, fmt.Errorf("process already running: %s", req.ID)
	}
	if req.Command == "" {
		return ProcInfo{}, errors.New("command required")
	}
	logFile := filepath.Join(m.store.LogDir(), req.ID+".log")
	_ = os.MkdirAll(filepath.Dir(logFile), 0o755)
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil { return ProcInfo{}, err }

	cmd := exec.Command(req.Command, req.Args...)
	cmd.Dir = req.Cwd
	cmd.Env = os.Environ()
	for k, v := range req.Env { cmd.Env = append(cmd.Env, k+"="+v) }
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil { _ = lf.Close(); return ProcInfo{}, err }

	info := ProcInfo{
		ID: req.ID, Name: req.Name, Command: req.Command, Args: req.Args, Cwd: req.Cwd, Env: req.Env,
		PID: cmd.Process.Pid, Status: "running", StartedAt: time.Now(), LogFile: logFile,
	}
	p := &proc{info: info, cmd: cmd}
	m.procs[req.ID] = p
	_ = m.store.UpsertApp(storage.App{
		ID: info.ID, Name: info.Name, Command: info.Command, Args: info.Args, Cwd: info.Cwd, Env: info.Env,
		PID: info.PID, Status: info.Status, LogFile: info.LogFile,
	})

	go m.pipeLogs(req.ID, stdout, lf, hub)
	go m.pipeLogs(req.ID, stderr, lf, hub)
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil {
			code := -1
			info.Code = &code
		} else {
			c := 0
			info.Code = &c
		}
		now := time.Now()
		info.ExitedAt = &now
		info.Status = "exited"
		m.procs[req.ID].info = info
		_ = m.store.UpsertApp(storage.App{
			ID: info.ID, Name: info.Name, Command: info.Command, Args: info.Args, Cwd: info.Cwd, Env: info.Env,
			PID: info.PID, Status: info.Status, LogFile: info.LogFile,
		})
	}()
	return info, nil
}

func (m *Manager) pipeLogs(id string, r io.Reader, lf *os.File, hub *ws.Hub) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text() + "\n"
		_, _ = lf.WriteString(line)
		payload, _ := json.Marshal(map[string]any{"type": "log", "id": id, "data": line, "ts": time.Now().UnixMilli()})
		hub.Emit(payload)
	}
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.procs[id]
	if !ok { return fmt.Errorf("not running: %s", id) }
	if p.cmd.Process == nil { return fmt.Errorf("no process") }
	return p.cmd.Process.Kill()
}

func (m *Manager) Restart(id string, hub *ws.Hub) error {
	app, ok := m.store.GetApp(id)
	if !ok { return fmt.Errorf("unknown id: %s", id) }
	_ = m.Stop(id)
	_, err := m.Start(StartRequest{ID: app.ID, Name: app.Name, Command: app.Command, Args: app.Args, Cwd: app.Cwd, Env: app.Env}, hub)
	return err
}

func (m *Manager) List() []ProcInfo {
	m.mu.Lock(); defer m.mu.Unlock()
	out := make([]ProcInfo, 0, len(m.procs))
	for _, p := range m.procs { out = append(out, p.info) }
	return out
}

func (m *Manager) ReadLog(id string, tail bool) ([]byte, error) {
	p, ok := m.store.GetApp(id)
	if !ok { return nil, fmt.Errorf("unknown id: %s", id) }
	b, err := os.ReadFile(p.LogFile)
	if err != nil { return nil, err }
	if !tail { return b, nil }
	if len(b) > 64*1024 { return b[len(b)-64*1024:], nil }
	return b, nil
}
