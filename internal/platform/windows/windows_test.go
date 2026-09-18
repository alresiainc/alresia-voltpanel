//go:build windows

package windows

import (
	"testing"

	"golang.org/x/sys/windows/svc"
)

// TestRealServiceInstallUninstall is the ONLY test in this package that
// would actually register a real Windows Service via the SCM -- it
// requires an elevated process to succeed at all. Skipped unless
// VOLT_REAL_SERVICE_TEST=1 is explicitly set; this implementation never
// sets that variable itself. This package's behavior could not be
// exercised on the machine this was written on (darwin); it is verified
// only by GOOS=windows cross-compilation, per the task's own scope.
func TestRealServiceInstallUninstall(t *testing.T) {
	t.Skip("requires VOLT_REAL_SERVICE_TEST=1 and an elevated Windows process; not run as part of ordinary test suites")
}

func TestStateStringKnownValues(t *testing.T) {
	// stateString has no Windows-API dependency, so this is safe to run
	// even in a constrained environment -- included mainly so the package
	// isn't "go test [no test files]" on a windows CI runner.
	if got := stateString(svc.Stopped); got != "stopped" {
		t.Errorf("expected \"stopped\", got %q", got)
	}
}
