package dbconn

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

func TestCreateGetList(t *testing.T) {
	r := newTestRepo(t)

	c, err := r.Create(CreateRequest{Name: "local mysql", Kind: KindMySQL, Host: "127.0.0.1", Port: 3306, Username: "root"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID == "" {
		t.Fatal("expected a generated id")
	}

	got, err := r.Get(c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "local mysql" || got.Kind != KindMySQL || got.Port != 3306 {
		t.Fatalf("unexpected round-tripped connection: %+v", got)
	}

	list, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(list))
	}
}

func TestCreateRejectsUnknownKind(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.Create(CreateRequest{Name: "x", Kind: "oracle", Host: "h", Username: "u"}); err == nil {
		t.Fatal("expected an error for an unsupported kind")
	}
}

func TestCreateRejectsMissingFields(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.Create(CreateRequest{Kind: KindMySQL, Host: "h", Username: "u"}); err == nil {
		t.Fatal("expected an error when name is missing")
	}
}

func TestDeleteAndNotFound(t *testing.T) {
	r := newTestRepo(t)
	c, _ := r.Create(CreateRequest{Name: "x", Kind: KindPostgres, Host: "h", Username: "u"})

	if err := r.Delete(c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(c.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := r.Delete(c.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound deleting again, got %v", err)
	}
}

func TestSecretRefRoundTrips(t *testing.T) {
	r := newTestRepo(t)
	id := NewID()
	c, err := r.Create(CreateRequest{ID: id, Name: "x", Kind: KindMySQL, Host: "h", Username: "u", SecretRef: "some-secret-id"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID != id {
		t.Fatalf("expected the caller-supplied ID to be used, got %q", c.ID)
	}
	got, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SecretRef != "some-secret-id" {
		t.Fatalf("expected secret_ref to round-trip, got %q", got.SecretRef)
	}
}
