package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/storage"
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
)

// HealthCheckDelay is how long StartAutostart waits after starting a
// service before treating it as healthy enough for whatever depends on it
// to start (§15: "process still running after a couple seconds" is a fine
// definition of healthy at this phase -- provider-specific health checks
// are future work). Tests lower this so the suite doesn't need to sleep for
// real.
var HealthCheckDelay = 2 * time.Second

// Manager is this package's ServiceLifecycle-shaped implementation --
// Start/Stop(graceful)/Status(Get)/Logs(ReadLog), loosely matching §7's
// ServiceLifecycle interface. It replaces internal/agent.Manager: same
// exec.Command + pipe-to-logfile-and-hub pattern (kept, not rewritten, per
// §2's note that it's reusable), plus graceful stop, crash detection with a
// restart policy, and dependency-ordered autostart.
type Manager struct {
	store *storage.Store
	hub   *ws.Hub

	mu    sync.Mutex
	procs map[string]*runningProc
}

type runningProc struct {
	svc  Service
	cmd  *exec.Cmd
	done chan struct{} // closed once cmd.Wait() has returned and cleanup has run

	stopRequested bool        // true once Stop() asked for this exit deliberately
	restartTimes  []time.Time // recent auto-restart timestamps, for the backoff window
}

func NewManager(store *storage.Store, hub *ws.Hub) *Manager {
	return &Manager{store: store, hub: hub, procs: map[string]*runningProc{}}
}

// Start launches a new service process. Returns an error if a service with
// this ID is already running (matches internal/agent.Manager's old
// behavior) or if Command is empty.
func (m *Manager) Start(req StartRequest) (Service, error) {
	m.mu.Lock()
	_, running := m.procs[req.ID]
	m.mu.Unlock()
	if running {
		return Service{}, fmt.Errorf("service already running: %s", req.ID)
	}
	if req.Command == "" {
		return Service{}, errors.New("command required")
	}
	return m.startProcess(req.toService(), nil)
}

// startProcess execs svc.Command and installs the log-piping and
// wait/restart goroutines. restartTimes carries forward backoff bookkeeping
// across an automatic restart; it's nil for a fresh manual Start.
func (m *Manager) startProcess(svc Service, restartTimes []time.Time) (Service, error) {
	logFile := svc.LogFile
	if logFile == "" {
		logFile = filepath.Join(m.store.LogDir(), svc.ID+".log")
	}
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return Service{}, err
	}
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return Service{}, err
	}

	cmd := exec.Command(svc.Command, svc.Args...)
	cmd.Dir = svc.Cwd
	cmd.Env = os.Environ()
	for k, v := range svc.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		_ = lf.Close()
		return Service{}, err
	}

	now := time.Now()
	svc.PID = cmd.Process.Pid
	svc.Status = StatusRunning
	svc.StartedAt = &now
	svc.ExitedAt = nil
	svc.ExitCode = nil
	svc.LogFile = logFile

	rp := &runningProc{svc: svc, cmd: cmd, done: make(chan struct{}), restartTimes: restartTimes}
	m.mu.Lock()
	m.procs[svc.ID] = rp
	m.mu.Unlock()

	m.persist(svc)

	go m.pipeLogs(svc.ID, stdout, lf)
	go m.pipeLogs(svc.ID, stderr, lf)
	go m.awaitExit(rp, lf)

	return svc, nil
}

func (m *Manager) pipeLogs(id string, r io.Reader, lf *os.File) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text() + "\n"
		_, _ = lf.WriteString(line)
		payload, _ := json.Marshal(map[string]any{"type": "log", "id": id, "data": line, "ts": time.Now().UnixMilli()})
		m.hub.Emit(payload)
	}
}

// awaitExit blocks on the process exiting, then either records a clean
// stop (deliberate Stop() call), or -- for an unexpected exit -- applies
// the service's restart policy: restart with basic backoff if under
// budget, otherwise mark it failed and give up (§ crash detection).
func (m *Manager) awaitExit(rp *runningProc, lf *os.File) {
	waitErr := rp.cmd.Wait()
	_ = lf.Close()
	// Close done only once every persist/restart decision below has run to
	// completion -- Stop() unblocks as soon as done closes, so closing it
	// any earlier would let a caller (or a test's t.TempDir cleanup) race
	// ahead of this goroutine still writing to storage or spawning a
	// restart.
	defer close(rp.done)

	m.mu.Lock()
	deliberate := rp.stopRequested
	svc := rp.svc
	delete(m.procs, svc.ID)
	m.mu.Unlock()

	now := time.Now()
	svc.ExitedAt = &now
	exitCode := 0
	if waitErr != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	svc.ExitCode = &exitCode
	svc.PID = 0

	if deliberate {
		svc.Status = StatusStopped
		m.persist(svc)
		return
	}

	crashed := exitCode != 0
	shouldRestart := svc.RestartPolicy == RestartAlways || (svc.RestartPolicy == RestartOnFailure && crashed)

	if !shouldRestart {
		if crashed {
			svc.Status = StatusFailed
		} else {
			svc.Status = StatusStopped
		}
		m.persist(svc)
		return
	}

	restartTimes := pruneRestartWindow(append(rp.restartTimes, now), svc.RestartWindow)
	if len(restartTimes) > svc.MaxRestarts {
		log.Printf("volt: service %s crashed %d times within %s, giving up (restart policy %s)", svc.ID, len(restartTimes), svc.RestartWindow, svc.RestartPolicy)
		svc.Status = StatusFailed
		m.persist(svc)
		return
	}

	log.Printf("volt: service %s exited unexpectedly (code=%d), restarting per policy %q (%d/%d used in window)", svc.ID, exitCode, svc.RestartPolicy, len(restartTimes), svc.MaxRestarts)
	if _, err := m.startProcess(svc, restartTimes); err != nil {
		log.Printf("volt: service %s restart failed: %v", svc.ID, err)
		svc.Status = StatusFailed
		m.persist(svc)
	}
}

func pruneRestartWindow(times []time.Time, window time.Duration) []time.Time {
	if window <= 0 {
		window = defaultRestartWindow
	}
	cutoff := time.Now().Add(-window)
	out := make([]time.Time, 0, len(times))
	for _, t := range times {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}

// Stop asks a running service to exit. When graceful is true, it sends a
// SIGTERM-equivalent (see terminate in signal_unix.go/signal_windows.go)
// and waits up to timeout (falling back to the service's configured
// GracefulTimeout, then a package default, if timeout <= 0) before hard-
// killing. When graceful is false, it kills immediately. Stop always
// blocks until the process has fully exited and been cleaned up.
func (m *Manager) Stop(id string, graceful bool, timeout time.Duration) error {
	m.mu.Lock()
	rp, ok := m.procs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("not running: %s", id)
	}
	rp.stopRequested = true
	if timeout <= 0 {
		timeout = rp.svc.GracefulTimeout
	}
	if timeout <= 0 {
		timeout = defaultGracefulTimeout
	}
	proc := rp.cmd.Process
	done := rp.done
	m.mu.Unlock()

	if proc == nil {
		return fmt.Errorf("no process for %s", id)
	}

	if !graceful {
		killErr := proc.Kill()
		<-done
		return killErr
	}

	if err := terminate(proc); err != nil {
		// Couldn't even deliver the signal (e.g. process already gone) --
		// fall back to a hard kill rather than hanging on done forever.
		killErr := proc.Kill()
		<-done
		return killErr
	}

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		killErr := proc.Kill()
		<-done
		return killErr
	}
}

// Restart stops (gracefully) a running service and starts it again with
// the same configuration, or -- if it isn't currently running in this
// process's lifetime -- starts it fresh from its persisted record.
func (m *Manager) Restart(id string) error {
	m.mu.Lock()
	rp, running := m.procs[id]
	m.mu.Unlock()

	var svc Service
	if running {
		svc = rp.svc
		if err := m.Stop(id, true, 0); err != nil {
			return err
		}
	} else {
		rec, ok := m.store.GetServiceRecord(id)
		if !ok {
			return fmt.Errorf("unknown service: %s", id)
		}
		svc = fromRecord(rec)
	}
	_, err := m.startProcess(svc, nil)
	return err
}

// List returns every service this Manager currently has running (live
// state only, in this process's lifetime -- callers merge with persisted
// state from storage for services that survived a daemon restart).
func (m *Manager) List() []Service {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Service, 0, len(m.procs))
	for _, rp := range m.procs {
		out = append(out, rp.svc)
	}
	return out
}

// Get returns the live state of a running service, if this Manager has it.
func (m *Manager) Get(id string) (Service, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rp, ok := m.procs[id]
	if !ok {
		return Service{}, false
	}
	return rp.svc, true
}

// ReadLog reads a service's log file from its persisted record (works even
// if the process isn't currently running in this daemon lifetime).
func (m *Manager) ReadLog(id string, tail bool) ([]byte, error) {
	rec, ok := m.store.GetServiceRecord(id)
	if !ok {
		return nil, fmt.Errorf("unknown id: %s", id)
	}
	b, err := os.ReadFile(rec.LogFile)
	if err != nil {
		return nil, err
	}
	if !tail {
		return b, nil
	}
	if len(b) > 64*1024 {
		return b[len(b)-64*1024:], nil
	}
	return b, nil
}

func (m *Manager) persist(svc Service) {
	if err := m.store.UpsertServiceRecord(toRecord(svc)); err != nil {
		log.Printf("volt: persist service %s: %v", svc.ID, err)
	}
}

// StartAutostart reads every service with autostart=true from storage,
// topologically sorts them by DependsOn[] (§15, Kahn's algorithm via
// TopoSort), and starts them in that order -- waiting HealthCheckDelay
// after each one before moving on to whatever depends on it. A dependency
// cycle aborts with a clear error rather than guessing an order; one
// service failing to start is logged and skipped rather than blocking
// unrelated services (§15: independent services stay independent).
func (m *Manager) StartAutostart(ctx context.Context) error {
	records, err := m.store.ListServiceRecords()
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}

	var autostart []Service
	for _, r := range records {
		if !r.Autostart {
			continue
		}
		autostart = append(autostart, fromRecord(r))
	}
	if len(autostart) == 0 {
		return nil
	}

	order, err := TopoSort(autostart)
	if err != nil {
		return fmt.Errorf("autostart dependency order: %w", err)
	}

	byID := make(map[string]Service, len(autostart))
	for _, s := range autostart {
		byID[s.ID] = s
	}

	for _, id := range order {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if _, ok := m.Get(id); ok {
			continue // already running
		}
		svc := byID[id]
		if _, err := m.startProcess(svc, nil); err != nil {
			log.Printf("volt: autostart: service %s failed to start: %v", id, err)
			continue
		}
		time.Sleep(HealthCheckDelay)
		if _, ok := m.Get(id); !ok {
			log.Printf("volt: autostart: service %s did not stay up through its health-check window; dependents may start unhealthy", id)
		}
	}
	return nil
}
