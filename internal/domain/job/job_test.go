package job

import (
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

func TestCreateGetFinish(t *testing.T) {
	r := newTestRepo(t)

	j, err := r.Create("install", "php@8.3", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if j.Status != StatusRunning {
		t.Fatalf("expected a fresh job to start Running, got %q", j.Status)
	}

	if err := r.SetLogFile(j.ID, "/tmp/some.log"); err != nil {
		t.Fatalf("SetLogFile: %v", err)
	}

	got, err := r.Get(j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LogFile != "/tmp/some.log" {
		t.Fatalf("expected log file to be set, got %q", got.LogFile)
	}

	if err := r.Finish(j.ID, StatusSuccess, 0, ""); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got, err = r.Get(j.ID)
	if err != nil {
		t.Fatalf("Get after Finish: %v", err)
	}
	if got.Status != StatusSuccess {
		t.Fatalf("expected StatusSuccess, got %q", got.Status)
	}
	if got.FinishedAt == nil {
		t.Fatal("expected FinishedAt to be set after Finish")
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %v", got.ExitCode)
	}
}

func TestGetNotFound(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.Get("nope"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListOrdersMostRecentFirst(t *testing.T) {
	r := newTestRepo(t)
	first, _ := r.Create("install", "redis", "")
	second, _ := r.Create("install", "mysql", "")

	list, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(list))
	}
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("expected most-recently-started job first")
	}
}
