package security

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// testSecretsDB opens a throwaway SQLite DB with just the `secrets` table
// (mirroring internal/storage/migrations/0001_init.sql) -- kept minimal and
// local to this package rather than importing internal/storage, which
// already imports internal/security and would make that an import cycle.
func testSecretsDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE secrets (
		id TEXT PRIMARY KEY,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		storage_backend TEXT NOT NULL,
		encrypted_blob BLOB,
		created_at TEXT NOT NULL,
		rotated_at TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSecretStorePutGetRoundTrip(t *testing.T) {
	db := testSecretsDB(t)
	cfgDir := t.TempDir()
	store, err := NewSecretStore(db, cfgDir)
	if err != nil {
		t.Fatal(err)
	}

	const plaintext = "ghp_thisIsATestTokenNotReal"
	id, err := store.Put("integration", "octocat", "github-pat", []byte(plaintext))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if id == "" {
		t.Fatal("expected a non-empty secret id")
	}

	// The DB row itself must never hold the plaintext -- only an encrypted
	// blob distinct from it.
	var blob []byte
	if err := db.QueryRow(`SELECT encrypted_blob FROM secrets WHERE id = ?`, id).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if string(blob) == plaintext {
		t.Fatal("encrypted_blob must not equal the plaintext")
	}
	for i := 0; i+len(plaintext) <= len(blob); i++ {
		if string(blob[i:i+len(plaintext)]) == plaintext {
			t.Fatal("plaintext token must not appear anywhere in the stored blob")
		}
	}

	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != plaintext {
		t.Fatalf("expected round-tripped plaintext %q, got %q", plaintext, got)
	}
}

func TestSecretStoreGetMissingReturnsErrNotFound(t *testing.T) {
	db := testSecretsDB(t)
	store, err := NewSecretStore(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("does-not-exist"); err != ErrSecretNotFound {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestSecretStoreDelete(t *testing.T) {
	db := testSecretsDB(t)
	store, err := NewSecretStore(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Put("integration", "octocat", "github-pat", []byte("secret-value"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(id); err != ErrSecretNotFound {
		t.Fatalf("expected deleted secret to be gone, got %v", err)
	}
}

func TestSecretStoreKeyFilePermissions(t *testing.T) {
	db := testSecretsDB(t)
	cfgDir := t.TempDir()
	if _, err := NewSecretStore(db, cfgDir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(cfgDir, secretKeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected key file to be 0600, got %o", perm)
	}
}

func TestSecretStoreReusesExistingKey(t *testing.T) {
	db := testSecretsDB(t)
	cfgDir := t.TempDir()
	store1, err := NewSecretStore(db, cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	id, err := store1.Put("integration", "octocat", "github-pat", []byte("persisted-secret"))
	if err != nil {
		t.Fatal(err)
	}

	// A second store instance backed by the same cfgDir/key file must be
	// able to decrypt what the first one wrote.
	store2, err := NewSecretStore(db, cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store2.Get(id)
	if err != nil {
		t.Fatalf("Get with reloaded key: %v", err)
	}
	if string(got) != "persisted-secret" {
		t.Fatalf("unexpected decrypted value: %q", got)
	}
}
