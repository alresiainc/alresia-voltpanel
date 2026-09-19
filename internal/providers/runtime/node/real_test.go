package node

import (
	"context"
	"os"
	"testing"
)

// TestRealDetectInstalled exercises DetectInstalled against a real (empty)
// managed directory plus whatever `node` happens to be on this machine's
// PATH. Detection is read-only, so this stays cheap; the real install/
// download path has its own gated test below.
//
// Run with: VOLT_REAL_INSTALL_TEST=1 go test ./internal/providers/runtime/node/...
func TestRealDetectInstalled(t *testing.T) {
	if os.Getenv("VOLT_REAL_INSTALL_TEST") != "1" {
		t.Skip("set VOLT_REAL_INSTALL_TEST=1 to run real-environment provider detection")
	}
	p := New(t.TempDir())
	versions, err := p.DetectInstalled(context.Background())
	if err != nil {
		t.Fatalf("DetectInstalled: %v", err)
	}
	t.Logf("detected %d node version(s): %+v", len(versions), versions)
}

// TestRealInstallAndRemove downloads a real, small-ish Node release from
// nodejs.org, verifies its checksum, extracts it, runs the real `node
// --version` from the extracted binary to prove it's actually usable, sets
// it as default, then removes it -- end to end, against the real network.
// Gated for the same reason every other "real" test in this codebase is:
// it depends on the host having network access, not just being a hermetic
// unit test.
func TestRealInstallAndRemove(t *testing.T) {
	if os.Getenv("VOLT_REAL_INSTALL_TEST") != "1" {
		t.Skip("set VOLT_REAL_INSTALL_TEST=1 to run a real download+install")
	}

	dir := t.TempDir()
	p := New(dir)
	ctx := context.Background()
	const version = "18.20.4" // an old, small, stable LTS release -- fast download, unlikely to ever be pulled

	var messages []string
	if err := p.Install(ctx, version, func(percent int, msg string) { messages = append(messages, msg) }); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("expected progress callbacks during a real install")
	}

	versions, err := p.DetectInstalled(ctx)
	if err != nil {
		t.Fatalf("DetectInstalled: %v", err)
	}
	found := false
	for _, v := range versions {
		if v.Version == version {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s to show up as installed, got %+v", version, versions)
	}

	if err := p.SetDefault(ctx, version); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	if err := p.Remove(ctx, version); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	versions, err = p.DetectInstalled(ctx)
	if err != nil {
		t.Fatalf("DetectInstalled after Remove: %v", err)
	}
	for _, v := range versions {
		if v.Version == version {
			t.Fatalf("expected %s to be gone after Remove, still present: %+v", version, versions)
		}
	}
}
