// Package security also implements the two-tier Secret abstraction from §9.7
// of the implementation plan: SSH keys and integration tokens are never
// stored as plaintext DB rows. SecretStore prefers the OS keychain (macOS
// Keychain via the `security` CLI today -- see keychain_darwin.go/
// keychain_other.go) and falls back to an AES-GCM-encrypted blob keyed by a
// machine-local key file (0600) only when no OS keychain is available (e.g.
// headless Linux, or Windows/Linux until a keychain backend is added there).
// The `secrets` SQLite table (internal/storage/migrations/0001_init.sql)
// only ever holds metadata plus the storage_backend discriminator -- never
// the plaintext.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// ErrSecretNotFound is returned by SecretStore.Resolve/Delete when no secret
// row matches the given ref.
var ErrSecretNotFound = errors.New("secret not found")

const (
	backendOSKeychain    = "os_keychain"
	backendEncryptedBlob = "encrypted_sqlite_blob"
)

// keychainSet/keychainGet/keychainDelete are package-level function
// variables (rather than plain calls to the platform-specific
// implementation) so tests can force the encrypted-blob fallback path
// deterministically, on any OS, without needing a real keychain to be
// unavailable.
var (
	keychainSet    = osKeychainSet
	keychainGet    = osKeychainGet
	keychainDelete = osKeychainDelete
)

// SecretStore is the SQLite-metadata-plus-real-backend implementation of the
// Secret entity (§6/§9.7). Callers store credential material (e.g. an SSH
// private key's PEM bytes) and get back an opaque secret_ref to persist on
// the owning row (e.g. servers.secret_ref) -- the plaintext itself never
// touches that row or any log/audit line.
type SecretStore struct {
	db      *sql.DB
	keyPath string
}

// NewSecretStore builds a SecretStore over an already-migrated *sql.DB
// (internal/storage.Store.DB()) and cfgDir (the Volt config directory,
// ~/.volt by default) for the encrypted-blob fallback's machine-local key
// file.
func NewSecretStore(db *sql.DB, cfgDir string) (*SecretStore, error) {
	return &SecretStore{db: db, keyPath: filepath.Join(cfgDir, "secret.key")}, nil
}

// Store persists value (plaintext credential material) for the given owner
// (e.g. ownerType="server", ownerID=<server id>) and kind (e.g.
// "ssh_key"), choosing the OS keychain first and falling back to the
// encrypted blob only when the keychain write fails. It returns the new
// secret's id -- the secret_ref other tables should hold -- never the
// plaintext.
func (s *SecretStore) Put(ownerType, ownerID, kind string, value []byte) (string, error) {
	id := uuid.NewString()
	account := keychainAccount(ownerType, ownerID, kind, id)

	backend := backendOSKeychain
	var blob []byte
	if err := keychainSet(account, value); err != nil {
		backend = backendEncryptedBlob
		enc, encErr := s.encrypt(value)
		if encErr != nil {
			return "", fmt.Errorf("store secret: os keychain unavailable (%v) and encrypted fallback failed: %w", err, encErr)
		}
		blob = enc
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO secrets (id, owner_type, owner_id, kind, storage_backend, encrypted_blob, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, ownerType, ownerID, kind, backend, blob, now,
	)
	if err != nil {
		if backend == backendOSKeychain {
			_ = keychainDelete(account) // best-effort cleanup, don't mask the real error
		}
		return "", err
	}
	return id, nil
}

// Resolve returns the plaintext value for a secret previously stored via
// Store.
func (s *SecretStore) Get(secretRef string) ([]byte, error) {
	ownerType, ownerID, kind, backend, blob, err := s.lookup(secretRef)
	if err != nil {
		return nil, err
	}
	switch backend {
	case backendOSKeychain:
		return keychainGet(keychainAccount(ownerType, ownerID, kind, secretRef))
	case backendEncryptedBlob:
		return s.decrypt(blob)
	default:
		return nil, fmt.Errorf("secret %s: unknown storage_backend %q", secretRef, backend)
	}
}

// Delete removes a secret from both its real backend and the metadata row.
// Deleting an already-absent ref is a no-op, not an error, matching the
// idempotent-delete convention used elsewhere in this codebase.
func (s *SecretStore) Delete(secretRef string) error {
	ownerType, ownerID, kind, backend, _, err := s.lookup(secretRef)
	if errors.Is(err, ErrSecretNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if backend == backendOSKeychain {
		_ = keychainDelete(keychainAccount(ownerType, ownerID, kind, secretRef))
	}
	_, err = s.db.Exec(`DELETE FROM secrets WHERE id = ?`, secretRef)
	return err
}

func (s *SecretStore) lookup(secretRef string) (ownerType, ownerID, kind, backend string, blob []byte, err error) {
	row := s.db.QueryRow(`SELECT owner_type, owner_id, kind, storage_backend, encrypted_blob FROM secrets WHERE id = ?`, secretRef)
	if scanErr := row.Scan(&ownerType, &ownerID, &kind, &backend, &blob); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return "", "", "", "", nil, ErrSecretNotFound
		}
		return "", "", "", "", nil, scanErr
	}
	return ownerType, ownerID, kind, backend, blob, nil
}

func keychainAccount(ownerType, ownerID, kind, id string) string {
	return fmt.Sprintf("volt:%s:%s:%s:%s", ownerType, ownerID, kind, id)
}

// loadOrCreateKey returns the machine-local AES-256 key used by the
// encrypted-blob fallback, generating and persisting one (0600) on first
// use.
func (s *SecretStore) loadOrCreateKey() ([]byte, error) {
	if b, err := os.ReadFile(s.keyPath); err == nil {
		if key, decErr := base64.StdEncoding.DecodeString(string(b)); decErr == nil && len(key) == 32 {
			return key, nil
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(s.keyPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.keyPath, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *SecretStore) encrypt(plaintext []byte) ([]byte, error) {
	gcm, err := s.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *SecretStore) decrypt(ciphertext []byte) ([]byte, error) {
	gcm, err := s.gcm()
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("encrypted secret blob is corrupt (too short)")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

func (s *SecretStore) gcm() (cipher.AEAD, error) {
	key, err := s.loadOrCreateKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
