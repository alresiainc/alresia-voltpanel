package php

import (
	"context"
	"os"
	"testing"
)

// TestRealDetectInstalled exercises the real sysInstaller against whatever
// PHP setup exists on the machine running the test. It never installs or
// removes anything -- detection is read-only -- but per the plan
// ("integration-test the real install path only in a platform-tagged,
// opt-in test") it stays opt-in since it depends on the host environment
// rather than being a hermetic unit test.
//
// Run with: VOLT_REAL_INSTALL_TEST=1 go test ./internal/providers/runtime/php/...
func TestRealDetectInstalled(t *testing.T) {
	if os.Getenv("VOLT_REAL_INSTALL_TEST") != "1" {
		t.Skip("set VOLT_REAL_INSTALL_TEST=1 to run real-environment provider detection")
	}
	p := New()
	versions, err := p.DetectInstalled(context.Background())
	if err != nil {
		t.Fatalf("DetectInstalled: %v", err)
	}
	t.Logf("detected %d php version(s): %+v", len(versions), versions)
}
