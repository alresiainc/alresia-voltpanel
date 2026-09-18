//go:build linux

package system

import "github.com/alresiainc/alresia-voltpanel/internal/platform/linux"

// ServiceOptions describes the Volt daemon service to register. Callers
// are expected to pass an explicit ExecPath (e.g. from os.Executable());
// this package makes no assumption about it.
type ServiceOptions struct {
	Name     string
	ExecPath string
	Args     []string
}

func Install(opts ServiceOptions) error {
	return linux.Install(linux.Options{Name: opts.Name, ExecPath: opts.ExecPath, Args: opts.Args})
}

func Uninstall(name string) error {
	return linux.Uninstall(name)
}

func Status(name string) (string, error) {
	return linux.Status(name)
}
