//go:build windows

package service

import "os"

// terminate has no graceful equivalent to send on Windows: Go's
// os.Process.Signal only supports os.Kill there (and os.Interrupt for
// console processes, which doesn't apply to a supervised child), so there
// is nothing SIGTERM-like this can deliver generically. Per the plan's own
// guidance for this platform difference, fall back to an immediate hard
// kill rather than pretending a graceful path exists -- Stop()'s
// wait-then-hard-kill logic still runs, it just resolves immediately since
// the process is already gone.
func terminate(p *os.Process) error {
	return p.Kill()
}
