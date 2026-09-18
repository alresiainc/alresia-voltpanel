package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceCRUDRoundTrip(t *testing.T) {
	cfgDir := t.TempDir()
	db, err := OpenSQLite(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a := App{ID: "svc1", Name: "web", Command: "node", Args: []string{"server.js"}, Cwd: "/tmp", Env: map[string]string{"PORT": "3000"}, PID: 1234, Status: "running", LogFile: "/tmp/svc1.log"}
	if err := upsertService(db, a); err != nil {
		t.Fatal(err)
	}

	got, ok, err := getService(db, "svc1")
	if err != nil || !ok {
		t.Fatalf("expected to find svc1: ok=%v err=%v", ok, err)
	}
	if got.Name != "web" || got.PID != 1234 || len(got.Args) != 1 || got.Args[0] != "server.js" || got.Env["PORT"] != "3000" {
		t.Fatalf("round-tripped service mismatch: %+v", got)
	}

	list, err := listServices(db)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 service, got %d (err=%v)", len(list), err)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	cfgDir := t.TempDir()
	db, err := OpenSQLite(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Re-opening (and thus re-running the migration runner) against the
	// same on-disk file must not fail or duplicate schema objects.
	db2, err := OpenSQLite(cfgDir)
	if err != nil {
		t.Fatalf("second open failed: %v", err)
	}
	defer db2.Close()

	var count int
	if err := db2.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 applied migration, got %d", count)
	}
}

func TestImportLegacyAppsIsIdempotentAndNonDestructive(t *testing.T) {
	cfgDir := t.TempDir()
	legacyJSON := `{"apps":[{"id":"legacy1","name":"old-app","command":"echo","args":["hi"],"status":"exited","logFile":"/tmp/legacy1.log"}]}`
	if err := os.WriteFile(filepath.Join(cfgDir, "apps.json"), []byte(legacyJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := OpenSQLite(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := importLegacyApps(db, cfgDir); err != nil {
		t.Fatal(err)
	}
	got, ok, err := getService(db, "legacy1")
	if err != nil || !ok {
		t.Fatalf("expected legacy1 imported: ok=%v err=%v", ok, err)
	}
	if got.Name != "old-app" {
		t.Fatalf("unexpected imported service: %+v", got)
	}

	// Mutate the SQLite row, then re-import: a real import must never
	// clobber data that already exists in SQLite.
	got.Name = "renamed-in-sqlite"
	if err := upsertService(db, got); err != nil {
		t.Fatal(err)
	}
	if err := importLegacyApps(db, cfgDir); err != nil {
		t.Fatal(err)
	}
	after, _, _ := getService(db, "legacy1")
	if after.Name != "renamed-in-sqlite" {
		t.Fatalf("re-import clobbered existing row: %+v", after)
	}

	// The legacy file itself must be left untouched.
	if _, err := os.Stat(filepath.Join(cfgDir, "apps.json")); err != nil {
		t.Fatalf("expected apps.json to remain: %v", err)
	}
}
