package runtime

import (
	"context"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	db, err := storage.OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepository(db)
}

func TestEnsureRuntimeCreatesOnceAndReturnsSameRow(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	rt1, err := repo.EnsureRuntime(ctx, "node", "Node.js")
	if err != nil {
		t.Fatal(err)
	}
	if rt1.Kind != "node" || rt1.Name != "Node.js" || rt1.ID == "" {
		t.Fatalf("unexpected runtime: %+v", rt1)
	}

	rt2, err := repo.EnsureRuntime(ctx, "node", "Node.js (renamed arg ignored)")
	if err != nil {
		t.Fatal(err)
	}
	if rt2.ID != rt1.ID {
		t.Fatalf("expected EnsureRuntime to return the same row, got %+v vs %+v", rt1, rt2)
	}
}

func TestReplaceDetectedInsertsAndPersistsVersions(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	_, err := repo.ReplaceDetected(ctx, "node", "Node.js", []DetectedVersion{
		{Version: "20.11.0", InstallPath: "/usr/local/bin/node", IsDefault: true},
		{Version: "18.19.0", InstallPath: "/home/u/.nvm/versions/node/v18.19.0/bin/node"},
	})
	if err != nil {
		t.Fatal(err)
	}

	runtimes, err := repo.ListRuntimes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d: %+v", len(runtimes), runtimes)
	}
	if len(runtimes[0].Versions) != 2 {
		t.Fatalf("expected 2 versions, got %d: %+v", len(runtimes[0].Versions), runtimes[0].Versions)
	}

	var defaultCount int
	for _, v := range runtimes[0].Versions {
		if v.Status != "installed" {
			t.Errorf("expected status installed, got %q for %+v", v.Status, v)
		}
		if v.IsDefault {
			defaultCount++
			if v.Version != "20.11.0" {
				t.Errorf("expected 20.11.0 to be default, got %+v", v)
			}
		}
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly 1 default version, got %d", defaultCount)
	}
}

func TestReplaceDetectedMarksGoneVersionsMissingWithoutDeleting(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if _, err := repo.ReplaceDetected(ctx, "node", "Node.js", []DetectedVersion{
		{Version: "20.11.0", IsDefault: true},
		{Version: "18.19.0"},
	}); err != nil {
		t.Fatal(err)
	}

	// Second detection run no longer finds 18.19.0 (e.g. it was uninstalled
	// outside VoltPanel).
	if _, err := repo.ReplaceDetected(ctx, "node", "Node.js", []DetectedVersion{
		{Version: "20.11.0", IsDefault: true},
	}); err != nil {
		t.Fatal(err)
	}

	runtimes, err := repo.ListRuntimes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimes[0].Versions) != 2 {
		t.Fatalf("expected the missing version's row to remain (marked missing), got %+v", runtimes[0].Versions)
	}
	var sawMissing bool
	for _, v := range runtimes[0].Versions {
		if v.Version == "18.19.0" {
			sawMissing = true
			if v.Status != "missing" {
				t.Fatalf("expected 18.19.0 to be marked missing, got %q", v.Status)
			}
		}
	}
	if !sawMissing {
		t.Fatal("expected to find the now-missing 18.19.0 row")
	}
}

func TestSetDefaultVersionSwitchesDefault(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if _, err := repo.ReplaceDetected(ctx, "node", "Node.js", []DetectedVersion{
		{Version: "20.11.0", IsDefault: true},
		{Version: "18.19.0"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.SetDefaultVersion(ctx, "node", "18.19.0"); err != nil {
		t.Fatal(err)
	}

	runtimes, err := repo.ListRuntimes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var newDefault string
	var defaultCount int
	for _, v := range runtimes[0].Versions {
		if v.IsDefault {
			defaultCount++
			newDefault = v.Version
		}
	}
	if defaultCount != 1 || newDefault != "18.19.0" {
		t.Fatalf("expected exactly 18.19.0 to be default, got count=%d version=%q", defaultCount, newDefault)
	}
}

func TestSetDefaultVersionUnknownReturnsErrVersionNotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if _, err := repo.ReplaceDetected(ctx, "node", "Node.js", []DetectedVersion{{Version: "20.11.0"}}); err != nil {
		t.Fatal(err)
	}

	err := repo.SetDefaultVersion(ctx, "node", "99.99.99")
	if err != ErrVersionNotFound {
		t.Fatalf("expected ErrVersionNotFound, got %v", err)
	}

	err = repo.SetDefaultVersion(ctx, "php", "8.3.1")
	if err != ErrVersionNotFound {
		t.Fatalf("expected ErrVersionNotFound for unknown runtime kind, got %v", err)
	}
}

func TestListRuntimesEmptyWhenNothingDetectedYet(t *testing.T) {
	repo := newTestRepo(t)
	runtimes, err := repo.ListRuntimes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimes) != 0 {
		t.Fatalf("expected no runtimes yet, got %+v", runtimes)
	}
}
