package security

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSandboxResolveRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	sb, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sb.Resolve("../etc/passwd"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	if _, err := sb.Resolve("sub/../../etc/passwd"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestSandboxResolveAllowsWithinRoot(t *testing.T) {
	dir := t.TempDir()
	sb, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := sb.Resolve("sub")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(dir, "sub"))
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}

	if _, err := sb.Resolve(filepath.Join(dir, "sub")); err != nil {
		t.Fatalf("expected absolute in-root path to resolve: %v", err)
	}
}

func TestSandboxResolveRejectsOutsideAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	sb, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if _, err := sb.Resolve(other); err == nil {
		t.Fatal("expected absolute path outside root to be rejected")
	}
}

func TestSandboxResolveRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows CI runners")
	}
	dir := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks not supported in this environment: %v", err)
	}

	sb, err := NewSandbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Resolve("escape"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}
