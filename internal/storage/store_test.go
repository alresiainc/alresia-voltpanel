package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyDir(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".alresia-voltpanel")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config.json"), []byte(`{"token":"abc"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	newDir := filepath.Join(home, ".volt")
	if err := migrateLegacyDir(home, newDir); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(newDir, "config.json"))
	if err != nil {
		t.Fatalf("expected migrated config.json: %v", err)
	}
	if string(b) != `{"token":"abc"}` {
		t.Fatalf("unexpected migrated content: %s", b)
	}

	if _, err := os.Stat(filepath.Join(legacy, "config.json")); err != nil {
		t.Fatalf("expected legacy file to remain untouched: %v", err)
	}
}

func TestMigrateLegacyDirNoopWhenNewDirExists(t *testing.T) {
	home := t.TempDir()
	newDir := filepath.Join(home, ".volt")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "config.json"), []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}

	legacy := filepath.Join(home, ".alresia-voltpanel")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config.json"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyDir(home, newDir); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(filepath.Join(newDir, "config.json"))
	if string(b) != "keep-me" {
		t.Fatalf("expected existing new dir contents preserved, got %s", b)
	}
}

func TestStoreFileSandboxRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	cfgDir := t.TempDir()
	s, err := NewStoreWithRoot(cfgDir, root)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.WriteFile("../outside.txt", []byte("x")); err == nil {
		t.Fatal("expected traversal write to be rejected")
	}
	if _, err := s.ListPath("../"); err == nil {
		t.Fatal("expected traversal list to be rejected")
	}
}

func TestStoreDeletePathRefusesRoot(t *testing.T) {
	root := t.TempDir()
	cfgDir := t.TempDir()
	s, err := NewStoreWithRoot(cfgDir, root)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeletePath(""); err == nil {
		t.Fatal("expected deleting the sandbox root to be rejected")
	}
	if err := s.DeletePath("."); err == nil {
		t.Fatal("expected deleting the sandbox root to be rejected")
	}
}

func TestStoreFileRoundTrip(t *testing.T) {
	root := t.TempDir()
	cfgDir := t.TempDir()
	s, err := NewStoreWithRoot(cfgDir, root)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.WriteFile("hello.txt", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListPath(".")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Name == "hello.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected hello.txt to be listed")
	}

	if err := s.DeletePath("hello.txt"); err != nil {
		t.Fatal(err)
	}
}
