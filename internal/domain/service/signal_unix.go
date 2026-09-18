//go:build !windows

package service

import (
	"os"
	"syscall"
)

// terminate asks a process to shut down gracefully. On Unix (darwin/linux)
// that means SIGTERM, which most well-behaved processes trap to exit
// cleanly -- Stop() falls back to a hard Kill() only if the process ignores
// this and outlives the graceful timeout.
func terminate(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
