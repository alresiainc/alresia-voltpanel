package integration

import (
	"encoding/json"
	"errors"
	"strings"
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

func TestCreateAndGet(t *testing.T) {
	r := newTestRepo(t)
	i, err := r.Create("github", "octocat", "secret-id-123", []string{"repo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if i.ID == "" {
		t.Fatal("expected a generated id")
	}

	got, err := r.Get(i.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != "github" || got.AccountRef != "octocat" {
		t.Fatalf("unexpected integration: %+v", got)
	}
	if got.SecretRef != "secret-id-123" {
		t.Fatalf("expected SecretRef to round-trip internally, got %q", got.SecretRef)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != "repo" {
		t.Fatalf("unexpected scopes: %+v", got.Scopes)
	}
}

func TestSecretRefNeverSerialized(t *testing.T) {
	r := newTestRepo(t)
	i, err := r.Create("github", "octocat", "super-sensitive-secret-id", nil)
	if err != nil {
		t.Fatal(err)
	}
	// json:"-" on SecretRef must hold even if a caller marshals the struct
	// directly (rather than the API layer's own response shape) -- belt
	// and suspenders against the secret ref ever appearing in a response.
	b, err := json.Marshal(i)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "super-sensitive-secret-id") {
		t.Fatalf("secretRef leaked into JSON: %s", b)
	}
}

func TestGetMissingReturnsErrNotFound(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.Get("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestList(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.Create("github", "a", "s1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create("github", "b", "s2", nil); err != nil {
		t.Fatal(err)
	}
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 integrations, got %d", len(list))
	}
}

func TestDelete(t *testing.T) {
	r := newTestRepo(t)
	i, err := r.Create("github", "octocat", "s1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(i.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(i.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleted integration to be gone, got %v", err)
	}
}

func TestDeleteMissingReturnsErrNotFound(t *testing.T) {
	r := newTestRepo(t)
	if err := r.Delete("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
