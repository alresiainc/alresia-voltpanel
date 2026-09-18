package security

import (
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	_ "modernc.org/sqlite"
)

// openTestSecretsDB opens a throwaway SQLite DB with just the `secrets`
// table (matching internal/storage/migrations/0001_init.sql) -- internal/
// security can't import internal/storage (storage already imports
// security, for the Sandbox and SessionAuth), so tests build their own
// minimal schema rather than sharing Store's migration runner.
func openTestSecretsDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
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
		t.Fatalf("create secrets table: %v", err)
	}
	return db
}

// forceEncryptedFallback monkey-patches the keychain functions so every
// Store call in this test takes the encrypted-blob path, deterministically
// and portably (§9.7 says the encrypted-blob fallback must be tested fully,
// independent of whether this machine's real OS keychain happens to be
// available).
func forceEncryptedFallback(t *testing.T) {
	t.Helper()
	origSet, origGet, origDel := keychainSet, keychainGet, keychainDelete
	keychainSet = func(account string, value []byte) error { return errors.New("forced unavailable for test") }
	keychainGet = func(account string) ([]byte, error) { return nil, errors.New("forced unavailable for test") }
	keychainDelete = func(account string) error { return nil }
	t.Cleanup(func() {
		keychainSet, keychainGet, keychainDelete = origSet, origGet, origDel
	})
}

func TestSecretStore_EncryptedBlobFallback_RoundTrip(t *testing.T) {
	forceEncryptedFallback(t)
	db := openTestSecretsDB(t)
	store, err := NewSecretStore(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	secret := []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nfake-key-material\n-----END OPENSSH PRIVATE KEY-----\n")
	ref, err := store.Put("server", "srv-1", "ssh_key", secret)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if ref == "" {
		t.Fatal("Store returned empty secret ref")
	}

	// The DB row must carry the ciphertext, never the plaintext.
	var backend string
	var blob []byte
	if err := db.QueryRow(`SELECT storage_backend, encrypted_blob FROM secrets WHERE id = ?`, ref).Scan(&backend, &blob); err != nil {
		t.Fatalf("query secret row: %v", err)
	}
	if backend != backendEncryptedBlob {
		t.Fatalf("storage_backend = %q, want %q", backend, backendEncryptedBlob)
	}
	if len(blob) == 0 {
		t.Fatal("encrypted_blob is empty")
	}
	if string(blob) == string(secret) {
		t.Fatal("encrypted_blob contains the plaintext secret verbatim")
	}

	got, err := store.Get(ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got) != string(secret) {
		t.Fatalf("Resolve returned %q, want %q", got, secret)
	}

	if err := store.Delete(ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ref); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Resolve after Delete: err = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretStore_EncryptedBlobFallback_WrongKeyFails(t *testing.T) {
	forceEncryptedFallback(t)
	db := openTestSecretsDB(t)
	dir1, dir2 := t.TempDir(), t.TempDir()
	store1, err := NewSecretStore(db, dir1)
	if err != nil {
		t.Fatal(err)
	}
	store2, err := NewSecretStore(db, dir2) // different machine-local key file
	if err != nil {
		t.Fatal(err)
	}

	ref, err := store1.Put("server", "srv-1", "ssh_key", []byte("secret"))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, err := store2.Get(ref); err == nil {
		t.Fatal("Resolve with a different machine-local key unexpectedly succeeded")
	}
}

func TestSecretStore_Resolve_NotFound(t *testing.T) {
	db := openTestSecretsDB(t)
	store, err := NewSecretStore(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("does-not-exist"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Resolve: err = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretStore_Delete_NotFoundIsNoOp(t *testing.T) {
	db := openTestSecretsDB(t)
	store, err := NewSecretStore(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("does-not-exist"); err != nil {
		t.Fatalf("Delete of a missing secret should be a no-op, got: %v", err)
	}
}

// TestSecretStore_OSKeychainRoundTrip is the best-effort *real* keychain
// test §9.7 asks for -- it uses the actual platform backend (no monkey-
// patching) and skips cleanly if the keychain is unavailable or access is
// denied (headless CI, a locked keychain, a non-darwin box with no keychain
// backend implemented yet, etc.) rather than failing the suite.
func TestSecretStore_OSKeychainRoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("no OS keychain backend implemented on this platform yet (see keychain_other.go); encrypted-blob fallback is covered by other tests")
	}
	db := openTestSecretsDB(t)
	store, err := NewSecretStore(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	secret := []byte("real-keychain-round-trip-test-value")
	ref, err := store.Put("server", "srv-keychain-test", "ssh_key", secret)
	if err != nil {
		t.Skipf("os keychain unavailable in this environment, skipping: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(ref) })

	var backend string
	if err := db.QueryRow(`SELECT storage_backend FROM secrets WHERE id = ?`, ref).Scan(&backend); err != nil {
		t.Fatalf("query secret row: %v", err)
	}
	if backend != backendOSKeychain {
		t.Skipf("environment fell back to encrypted blob instead of the real keychain (backend=%q); nothing more to verify here", backend)
	}

	got, err := store.Get(ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got) != string(secret) {
		t.Fatalf("Resolve returned %q, want %q", got, secret)
	}
}
