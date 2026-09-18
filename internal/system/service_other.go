//go:build !windows && !darwin && !linux

// This file covers any other Unix Volt might build on (e.g. freebsd) that
// doesn't have a real internal/platform/* implementation yet -- kept as a
// no-op placeholder like the original stubs, rather than failing to
// compile on an OS this phase didn't scope real service registration for.
package system

type ServiceOptions struct {
	Name     string
	ExecPath string
	Args     []string
}

func Install(opts ServiceOptions) error  { return nil }
func Uninstall(name string) error        { return nil }
func Status(name string) (string, error) { return "not supported on this platform", nil }
