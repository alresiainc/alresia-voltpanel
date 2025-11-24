//go:build windows

package system

type ServiceOptions struct { Name string }

func Install(opts ServiceOptions) error { return nil }
func Uninstall(name string) error { return nil }
