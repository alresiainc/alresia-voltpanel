//go:build windows

package hosts

import (
	"os"
	"path/filepath"
)

// defaultHostsPath returns the real hosts file location on Windows (§8 of
// the plan): C:\Windows\System32\drivers\etc\hosts, resolved relative to
// %SystemRoot% (falling back to C:\Windows if that env var is somehow
// unset). Writing here without an elevated/UAC-approved process will fail
// -- expected, per §8 the daemon itself never runs elevated.
func defaultHostsPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "drivers", "etc", "hosts")
}
