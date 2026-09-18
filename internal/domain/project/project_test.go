package project

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

func repoTestdata(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to resolve this test file's path")
	}
	// internal/domain/project -> repo root is three levels up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(root, "testdata", name)
}

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	db, err := storage.OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepository(db)
}

func TestValidatePathRejectsRelative(t *testing.T) {
	if err := ValidatePath("relative/path"); err == nil {
		t.Fatal("expected an error for a relative path")
	}
}

func TestValidatePathRejectsMissing(t *testing.T) {
	if err := ValidatePath(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a nonexistent path")
	}
}

func TestValidatePathRejectsFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "notadir")
	if err := writeFile(f, "x"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePath(f); err == nil {
		t.Fatal("expected an error when path is a file, not a directory")
	}
}

func TestValidatePathAcceptsRealDir(t *testing.T) {
	if err := ValidatePath(t.TempDir()); err != nil {
		t.Fatalf("expected a real absolute directory to validate, got %v", err)
	}
}

func TestCreateRunsDetectionAndPersists(t *testing.T) {
	repo := newTestRepo(t)
	path := repoTestdata(t, "laravel-fixture")

	p, err := repo.Create("my-laravel-app", path)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" {
		t.Fatal("expected a generated id")
	}
	if p.DetectedKind != "laravel" {
		t.Fatalf("expected detected kind laravel, got %q", p.DetectedKind)
	}
	if p.RunCommand != "php artisan serve" {
		t.Fatalf("expected default laravel run command, got %q", p.RunCommand)
	}

	got, err := repo.Get(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != path || got.Name != "my-laravel-app" {
		t.Fatalf("round-tripped project mismatch: %+v", got)
	}
}

func TestCreateRejectsBadPath(t *testing.T) {
	repo := newTestRepo(t)
	if _, err := repo.Create("bad", "not-absolute"); err == nil {
		t.Fatal("expected Create to reject a relative path")
	}
}

func TestListReturnsAllProjects(t *testing.T) {
	repo := newTestRepo(t)
	if _, err := repo.Create("a", repoTestdata(t, "node-fixture")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create("b", repoTestdata(t, "nextjs-fixture")); err != nil {
		t.Fatal(err)
	}
	list, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(list))
	}
}

func TestGetMissingReturnsErrNotFound(t *testing.T) {
	repo := newTestRepo(t)
	if _, err := repo.Get("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteRemovesProject(t *testing.T) {
	repo := newTestRepo(t)
	p, err := repo.Create("gone", repoTestdata(t, "php-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteMissingReturnsErrNotFound(t *testing.T) {
	repo := newTestRepo(t)
	if err := repo.Delete("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRedetectUpdatesStoredResult(t *testing.T) {
	repo := newTestRepo(t)
	// Start pointed at a directory with no markers.
	empty := t.TempDir()
	p, err := repo.Create("shifting", empty)
	if err != nil {
		t.Fatal(err)
	}
	if p.DetectedKind != "unknown" {
		t.Fatalf("expected unknown for an empty dir, got %q", p.DetectedKind)
	}

	// Simulate the project growing a package.json with a start script
	// between the initial add and a later re-detect.
	if err := writeFile(filepath.Join(empty, "package.json"), `{"scripts":{"start":"node index.js"}}`); err != nil {
		t.Fatal(err)
	}

	updated, err := repo.Redetect(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DetectedKind != "node" || updated.RunCommand != "npm start" {
		t.Fatalf("expected redetect to pick up node/npm start, got %+v", updated)
	}

	// And it should be persisted, not just returned.
	got, err := repo.Get(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DetectedKind != "node" || got.RunCommand != "npm start" {
		t.Fatalf("expected persisted redetect result, got %+v", got)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
