package hosts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tempHostsPath returns a path under t.TempDir() -- every test in this file
// uses NewProvider against one of these, never New() (the real OS path).
func tempHostsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "hosts")
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewNeverPointsAtTestPath(t *testing.T) {
	// Sanity check on the production constructor itself: it must resolve
	// to a real per-OS system path, never something under a temp dir. The
	// package's own tests below always use NewProvider with an explicit
	// temp path instead.
	p := New()
	if p.Path == "" {
		t.Fatal("expected a non-empty default hosts path")
	}
	if strings.Contains(p.Path, os.TempDir()) {
		t.Fatalf("default hosts path unexpectedly resolved under the temp dir: %s", p.Path)
	}
}

func TestAddOnEmptyFileCreatesManagedEntry(t *testing.T) {
	path := tempHostsPath(t)
	p := NewProvider(path)

	if err := p.Add(context.Background(), "myapp.test"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "127.0.0.1") || !strings.Contains(content, "myapp.test") {
		t.Fatalf("expected file to map myapp.test to 127.0.0.1, got:\n%s", content)
	}
	if !strings.Contains(content, managedMarker) {
		t.Fatalf("expected the volt-managed marker comment, got:\n%s", content)
	}
}

func TestAddIsIdempotent(t *testing.T) {
	path := tempHostsPath(t)
	p := NewProvider(path)

	if err := p.Add(context.Background(), "myapp.test"); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Add(context.Background(), "myapp.test"); err != nil {
		t.Fatalf("second Add: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected re-adding an already-managed hostname to be a no-op.\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestAddPreservesForeignLinesAndRefusesTakeover(t *testing.T) {
	path := tempHostsPath(t)
	mustWrite(t, path, "127.0.0.1\tlocalhost\n192.168.1.5\tsomeone-elses-app.test\n")
	p := NewProvider(path)

	if err := p.Add(context.Background(), "someone-elses-app.test"); err == nil {
		t.Fatal("expected Add to refuse taking over a foreign (non-volt) entry")
	}

	// The pre-existing foreign content must be completely untouched.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "127.0.0.1\tlocalhost") {
		t.Fatalf("expected the pre-existing localhost line to survive untouched, got:\n%s", content)
	}
	if !strings.Contains(content, "192.168.1.5\tsomeone-elses-app.test") {
		t.Fatalf("expected the foreign entry to survive untouched, got:\n%s", content)
	}
}

func TestAddRejectsEmptyHostname(t *testing.T) {
	p := NewProvider(tempHostsPath(t))
	if err := p.Add(context.Background(), ""); err == nil {
		t.Fatal("expected an error for an empty hostname")
	}
}

func TestRemoveDeletesManagedEntryOnly(t *testing.T) {
	path := tempHostsPath(t)
	mustWrite(t, path, "127.0.0.1\tlocalhost\n")
	p := NewProvider(path)

	if err := p.Add(context.Background(), "myapp.test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := p.Add(context.Background(), "other.test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := p.Remove(context.Background(), "myapp.test"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if strings.Contains(content, "myapp.test") {
		t.Fatalf("expected myapp.test to be removed, got:\n%s", content)
	}
	if !strings.Contains(content, "other.test") {
		t.Fatalf("expected other.test to survive removal of a different entry, got:\n%s", content)
	}
	if !strings.Contains(content, "127.0.0.1\tlocalhost") {
		t.Fatalf("expected the original localhost line to survive, got:\n%s", content)
	}
}

func TestRemoveUnknownHostnameReturnsErrNotFound(t *testing.T) {
	p := NewProvider(tempHostsPath(t))
	err := p.Remove(context.Background(), "never-added.test")
	if err == nil {
		t.Fatal("expected an error removing a hostname that was never added")
	}
	if !strings.Contains(err.Error(), ErrNotFound.Error()) {
		t.Fatalf("expected ErrNotFound-flavored error, got: %v", err)
	}
}

func TestRemoveNeverTouchesForeignEntryForSameHostname(t *testing.T) {
	path := tempHostsPath(t)
	mustWrite(t, path, "192.168.1.5\tforeign.test\n")
	p := NewProvider(path)

	err := p.Remove(context.Background(), "foreign.test")
	if err == nil {
		t.Fatal("expected Remove to report not-found rather than delete a foreign entry")
	}

	b, err2 := os.ReadFile(path)
	if err2 != nil {
		t.Fatal(err2)
	}
	if !strings.Contains(string(b), "192.168.1.5\tforeign.test") {
		t.Fatalf("expected the foreign entry to survive untouched, got:\n%s", b)
	}
}

func TestListReturnsOnlyManagedHostnamesSorted(t *testing.T) {
	path := tempHostsPath(t)
	mustWrite(t, path, "127.0.0.1\tlocalhost\n192.168.1.5\tforeign.test\n")
	p := NewProvider(path)

	for _, h := range []string{"zeta.test", "alpha.test"} {
		if err := p.Add(context.Background(), h); err != nil {
			t.Fatalf("Add(%s): %v", h, err)
		}
	}

	got, err := p.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha.test", "zeta.test"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestListOnMissingFileReturnsEmpty(t *testing.T) {
	p := NewProvider(tempHostsPath(t)) // file never created
	got, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("expected a missing hosts file to behave like an empty one, got error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no managed hostnames, got %v", got)
	}
}

func TestConflictsDetectsForeignEntry(t *testing.T) {
	path := tempHostsPath(t)
	mustWrite(t, path, "10.0.0.9\tapi.myapp.test\n")
	p := NewProvider(path)

	conflicts, err := p.Conflicts(context.Background(), "api.myapp.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected exactly one conflict, got %v", conflicts)
	}
	if !strings.Contains(conflicts[0], "10.0.0.9") {
		t.Fatalf("expected the conflict message to mention the foreign IP, got: %s", conflicts[0])
	}
}

func TestConflictsEmptyWhenClear(t *testing.T) {
	path := tempHostsPath(t)
	mustWrite(t, path, "127.0.0.1\tlocalhost\n")
	p := NewProvider(path)

	conflicts, err := p.Conflicts(context.Background(), "brand-new.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %v", conflicts)
	}
}

func TestConflictsEmptyForOwnManagedEntry(t *testing.T) {
	path := tempHostsPath(t)
	p := NewProvider(path)
	if err := p.Add(context.Background(), "myapp.test"); err != nil {
		t.Fatal(err)
	}

	conflicts, err := p.Conflicts(context.Background(), "myapp.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts against volt's own correctly-pointed entry, got %v", conflicts)
	}
}
