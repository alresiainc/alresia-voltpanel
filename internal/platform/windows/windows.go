//go:build windows

// Package windows implements real Windows Service registration for the
// Volt daemon (§8/§17 Phase 5) via golang.org/x/sys/windows/svc, replacing
// internal/system's former no-op stubs. This package only needs to
// cross-compile correctly (GOOS=windows go build ./...); its behavior
// cannot be exercised on the darwin/linux machine this was written on, so
// keep it small, well-isolated, and lean on the SCM's own APIs rather than
// anything clever.
package windows

import (
	"fmt"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Options describes the Windows Service to install.
type Options struct {
	// Name is the service's short name (used by SCM commands).
	Name string
	// DisplayName is the human-readable name shown in services.msc.
	DisplayName string
	// ExecPath is the absolute path to the voltpanel binary.
	ExecPath string
	Args     []string
}

func applyDefaults(opts Options) Options {
	if opts.DisplayName == "" {
		opts.DisplayName = "Alresia Volt Daemon"
	}
	return opts
}

// Install registers the service with the Windows Service Control Manager,
// set to start automatically. This is a real, side-effecting registration
// -- it requires the calling process to be elevated (§8: privilege
// escalation happens via a narrowly-scoped, explicit helper invocation,
// never the daemon running elevated by default).
func Install(opts Options) error {
	opts = applyDefaults(opts)
	if opts.Name == "" {
		return fmt.Errorf("windows: Name is required")
	}
	if opts.ExecPath == "" {
		return fmt.Errorf("windows: ExecPath is required")
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("windows: connect to service manager: %w", err)
	}
	defer m.Disconnect()

	if existing, err := m.OpenService(opts.Name); err == nil {
		existing.Close()
		return fmt.Errorf("windows: service %q already exists", opts.Name)
	}

	s, err := m.CreateService(opts.Name, opts.ExecPath, mgr.Config{
		DisplayName: opts.DisplayName,
		StartType:   mgr.StartAutomatic,
	}, opts.Args...)
	if err != nil {
		return fmt.Errorf("windows: create service: %w", err)
	}
	defer s.Close()
	return nil
}

// Uninstall stops (best-effort) and deletes the named service.
func Uninstall(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("windows: connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("windows: open service %q: %w", name, err)
	}
	defer s.Close()

	_, _ = s.Control(svc.Stop) // best-effort; Delete below is what matters
	return s.Delete()
}

// Status reports the service's current SCM state, or "not installed" if
// no such service is registered.
func Status(name string) (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("windows: connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return "not installed", nil
	}
	defer s.Close()

	st, err := s.Query()
	if err != nil {
		return "", fmt.Errorf("windows: query service status: %w", err)
	}
	return stateString(st.State), nil
}

func stateString(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start-pending"
	case svc.StopPending:
		return "stop-pending"
	case svc.Running:
		return "running"
	case svc.ContinuePending:
		return "continue-pending"
	case svc.PausePending:
		return "pause-pending"
	case svc.Paused:
		return "paused"
	default:
		return fmt.Sprintf("unknown(%d)", state)
	}
}

// Handler adapts an arbitrary run/stop pair to svc.Handler, so
// cmd/voltpanel can register itself as an SCM-managed service entry point
// in a future phase (RunAsService below wires it to svc.Run). Not called
// from main.go yet -- wiring a real Windows service main loop can't be
// verified on this machine, so it's left present-but-unused rather than
// half-integrated.
type Handler struct {
	// Run should block until the daemon should stop, honoring ctx-less
	// cancellation via the returned stop func being called.
	Run func(stop <-chan struct{}) error
}

func (h *Handler) Execute(_ []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	s <- svc.Status{State: svc.StartPending}
	stop := make(chan struct{})
	errCh := make(chan error, 1)
	go func() { errCh <- h.Run(stop) }()

	s <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case <-errCh:
			return false, 0
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				s <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				close(stop)
				<-errCh
				return false, 0
			}
		}
	}
}

// RunAsService starts the SCM dispatch loop for name, blocking until the
// service is asked to stop. Only meaningful when the process was actually
// launched by the SCM (isWindowsService below should gate the call site).
func RunAsService(name string, h *Handler) error {
	return svc.Run(name, h)
}

// IsWindowsService reports whether the current process was started by the
// Windows Service Control Manager, so main.go can decide whether to run
// the normal foreground path or dispatch through RunAsService.
func IsWindowsService() (bool, error) {
	return svc.IsWindowsService()
}
