//go:build !windows

package hosts

// defaultHostsPath returns the real hosts file location on macOS and Linux
// (§8 of the plan). Writing here as a normal user will fail with a
// permission error -- that's expected; per §8's cross-platform strategy the
// daemon itself never runs elevated, and the privileged-helper escalation
// flow that would let a real write succeed is out of scope for this
// package, which only owns the file-format read/write logic.
const defaultPathConst = "/etc/hosts"

func defaultHostsPath() string {
	return defaultPathConst
}
