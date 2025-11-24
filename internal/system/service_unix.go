//go:build !windows

package system

// Placeholder for systemd/launchd helpers
// Actual registration handled by packaging templates and docs.

type ServiceOptions struct {
	Name string
}

func Install(opts ServiceOptions) error { return nil }
func Uninstall(name string) error { return nil }
