package extension

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

// repoTestdata resolves testdata/<name> at the repository root, regardless
// of the test runner's working directory -- same pattern used by
// internal/domain/project/detect/detect_test.go and
// internal/pluginhost/host_test.go.
func repoTestdata(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to resolve this test file's path")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 20; i++ {
		candidate := filepath.Join(dir, "testdata")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return filepath.Join(candidate, name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate repository testdata/ directory above %s", thisFile)
	return ""
}

func newTestRepo(t *testing.T) (*Repository, *providers.Registry, *sql.DB) {
	t.Helper()
	db, err := storage.OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	registry := providers.NewRegistry()
	return NewRepository(db, registry), registry, db
}

func TestInstallReadsManifestAndPersistsDisabled(t *testing.T) {
	repo, _, _ := newTestRepo(t)
	ctx := context.Background()

	ext, err := repo.Install(ctx, repoTestdata(t, "sample-python-provider"))
	if err != nil {
		t.Fatal(err)
	}
	if ext.Name != "python-demo" || ext.Version != "0.1.0" || ext.Kind != "runtime" {
		t.Fatalf("unexpected extension from manifest: %+v", ext)
	}
	if ext.Enabled {
		t.Fatal("expected a freshly installed extension to be disabled until explicitly enabled")
	}
	if len(ext.Permissions) != 1 || ext.Permissions[0] != "exec:python3" {
		t.Fatalf("expected permissions to be read from the manifest, got %+v", ext.Permissions)
	}

	got, ok, err := repo.Get(ctx, ext.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected to find the installed extension by id")
	}
	if got.Source != ext.Source {
		t.Fatalf("expected persisted source to match, got %q vs %q", got.Source, ext.Source)
	}
}

func TestInstallRejectsMissingManifest(t *testing.T) {
	repo, _, _ := newTestRepo(t)
	if _, err := repo.Install(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected an error installing from a directory with no manifest")
	}
}

func TestListAndGet(t *testing.T) {
	repo, _, _ := newTestRepo(t)
	ctx := context.Background()

	if list, err := repo.List(ctx); err != nil || len(list) != 0 {
		t.Fatalf("expected an empty list initially, got %+v, err=%v", list, err)
	}

	ext, err := repo.Install(ctx, repoTestdata(t, "sample-python-provider"))
	if err != nil {
		t.Fatal(err)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != ext.ID {
		t.Fatalf("expected the installed extension to appear in List, got %+v", list)
	}

	if _, ok, err := repo.Get(ctx, "does-not-exist"); err != nil || ok {
		t.Fatalf("expected ok=false, err=nil for an unknown id, got ok=%v err=%v", ok, err)
	}
}

func TestEnableRegistersProviderIntoRegistryAndDisableUnregisters(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available on this machine")
	}
	repo, registry, _ := newTestRepo(t)
	ctx := context.Background()

	ext, err := repo.Install(ctx, repoTestdata(t, "sample-python-provider"))
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := registry.Runtime("python-demo"); ok {
		t.Fatal("expected the provider not to be registered before Enable")
	}

	enabled, err := repo.Enable(ctx, ext.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled {
		t.Fatal("expected Enable to return enabled=true")
	}

	// The whole point of Phase 11: the Registry now holds this extension
	// exactly like a built-in provider, and answers real detection
	// through nothing but the providers.RuntimeProvider interface.
	provider, ok := registry.Runtime("python-demo")
	if !ok {
		t.Fatal("expected the extension's provider to be registered after Enable")
	}
	versions, err := provider.DetectInstalled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Version == "" {
		t.Fatalf("expected a real detected python3 version, got %+v", versions)
	}

	got, ok, err := repo.Get(ctx, ext.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !got.Enabled {
		t.Fatalf("expected persisted enabled=true, got %+v (ok=%v)", got, ok)
	}

	disabled, err := repo.Disable(ctx, ext.ID)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled {
		t.Fatal("expected Disable to return enabled=false")
	}
	if _, ok := registry.Runtime("python-demo"); ok {
		t.Fatal("expected the provider to be unregistered after Disable")
	}

	got2, ok, err := repo.Get(ctx, ext.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got2.Enabled {
		t.Fatalf("expected persisted enabled=false, got %+v", got2)
	}
}

func TestEnableUnknownKindReturnsError(t *testing.T) {
	repo, _, db := newTestRepo(t)
	ctx := context.Background()

	permsJSON, _ := json.Marshal([]string{})
	if _, err := db.ExecContext(ctx,
		`INSERT INTO extensions (id, name, version, kind, source, enabled, permissions) VALUES (?, ?, ?, ?, ?, 0, ?)`,
		"id-1", "some-db", "1.0", "database", "/nowhere", string(permsJSON),
	); err != nil {
		t.Fatal(err)
	}

	_, err := repo.Enable(ctx, "id-1")
	if !errors.Is(err, ErrUnsupportedKind) {
		t.Fatalf("expected ErrUnsupportedKind, got %v", err)
	}
}

func TestEnableUnknownIDReturnsErrNotFound(t *testing.T) {
	repo, _, _ := newTestRepo(t)
	_, err := repo.Enable(context.Background(), "does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRemoveDisablesFirstThenDeletes(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available on this machine")
	}
	repo, registry, _ := newTestRepo(t)
	ctx := context.Background()

	ext, err := repo.Install(ctx, repoTestdata(t, "sample-python-provider"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Enable(ctx, ext.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Runtime("python-demo"); !ok {
		t.Fatal("setup: expected the provider to be registered")
	}

	if err := repo.Remove(ctx, ext.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Runtime("python-demo"); ok {
		t.Fatal("expected Remove to unregister the provider (via Disable) before deleting")
	}
	if _, ok, err := repo.Get(ctx, ext.ID); err != nil || ok {
		t.Fatalf("expected the row to be gone after Remove, ok=%v err=%v", ok, err)
	}
}

func TestRemoveUnknownIDReturnsErrNotFound(t *testing.T) {
	repo, _, _ := newTestRepo(t)
	if err := repo.Remove(context.Background(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadEnabledRelaunchesPersistedEnabledExtensions(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available on this machine")
	}
	repo, _, db := newTestRepo(t)
	ctx := context.Background()

	ext, err := repo.Install(ctx, repoTestdata(t, "sample-python-provider"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Enable(ctx, ext.ID); err != nil {
		t.Fatal(err)
	}

	// Simulate a daemon restart: fresh Repository/Registry over the same
	// (still-enabled) database rows, with nothing loaded yet.
	freshRegistry := providers.NewRegistry()
	freshRepo := NewRepository(db, freshRegistry)
	if _, ok := freshRegistry.Runtime("python-demo"); ok {
		t.Fatal("setup: expected a fresh registry to start empty")
	}

	freshRepo.LoadEnabled(ctx)

	if _, ok := freshRegistry.Runtime("python-demo"); !ok {
		t.Fatal("expected LoadEnabled to re-register the previously-enabled extension")
	}

	// Original repo's own loaded copy is a separate subprocess; close both
	// to avoid leaking processes at the end of the test.
	repo.CloseAll()
	freshRepo.CloseAll()
}

func TestCloseAllStopsSubprocessesAndUnregisters(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available on this machine")
	}
	repo, registry, _ := newTestRepo(t)
	ctx := context.Background()

	ext, err := repo.Install(ctx, repoTestdata(t, "sample-python-provider"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Enable(ctx, ext.ID); err != nil {
		t.Fatal(err)
	}

	repo.CloseAll()

	if _, ok := registry.Runtime("python-demo"); ok {
		t.Fatal("expected CloseAll to unregister every loaded provider")
	}

	// Enabled flag in storage is untouched by CloseAll (it's a runtime
	// cleanup, not a state change) -- LoadEnabled after a real restart is
	// what's expected to bring it back.
	got, ok, err := repo.Get(ctx, ext.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !got.Enabled {
		t.Fatalf("expected persisted enabled state to remain true after CloseAll, got %+v", got)
	}
}
