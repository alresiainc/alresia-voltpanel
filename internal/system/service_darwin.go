//go:build darwin

// Package system exposes real per-OS service registration for the Volt
// daemon itself (§8/§17 Phase 5) -- ServiceOptions/Install/Uninstall/Status
// used to be no-op stubs ("handled by packaging templates and docs"); they
// now delegate to internal/platform/{darwin,linux,windows}, which do the
// actual launchd/systemd/SCM work. Kept as a thin per-OS delegating shim so
// callers (CLI, future UI) depend on one stable interface regardless of
// platform.
package system

import "github.com/alresiainc/alresia-voltpanel/internal/platform/darwin"

// ServiceOptions describes the Volt daemon service to register. Callers
// are expected to pass an explicit ExecPath (e.g. from os.Executable());
// this package makes no assumption about it.
type ServiceOptions struct {
	Name     string
	ExecPath string
	Args     []string
}

func Install(opts ServiceOptions) error {
	return darwin.Install(darwin.Options{Label: opts.Name, ExecPath: opts.ExecPath, Args: opts.Args})
}

func Uninstall(name string) error {
	return darwin.Uninstall(name)
}

func Status(name string) (string, error) {
	return darwin.Status(name)
}
