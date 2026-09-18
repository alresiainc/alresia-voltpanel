// Secret storage (§9.7 of the implementation plan): integration tokens
// (GitHub PATs today, SSH keys/other credentials later) are never stored
// as plaintext DB rows. The full plan design prefers the OS keychain first
// and falls back to an AES-GCM-encrypted local blob only when no keychain
// is available (headless Linux); no keychain integration exists yet in
// this codebase, so SecretStore below implements just the fallback path,
// scoped to this need. It follows the same shape (opaque secretID in,
// plaintext out, by id) so a future OS-keychain-first implementation can
// slot in underneath without changing any caller.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// ErrSecretNotFound is returned by SecretStore.Get/Delete when no secret
// with the given id exists.
var ErrSecretNotFound = errors.New("secret not found")

// secretKeyFileName is the machine-local AES-256 key file's name, created
// 0600 under the daemon's config directory on first use. Losing this file
// makes every previously stored secret unrecoverable by design -- it is
// never written anywhere else (never logged, never in the DB).
const secretKeyFileName = "secret.key"

// SecretStore persists encrypted secret blobs in the `secrets` table
// (internal/storage/migrations/0001_init.sql: owner_type, owner_id, kind,
// storage_backend, encrypted_blob), keyed by a machine-local key file.
// Zero value is not usable; construct with NewSecretStore.
type SecretStore struct {
	db  *sql.DB
	key []byte
}

// NewSecretStore loads (or creates) the machine-local key file under
// cfgDir and returns a SecretStore backed by db's `secrets` table.
func NewSecretStore(db *sql.DB, cfgDir string) (*SecretStore, error) {
	key, err := loadOrCreateSecretKey(filepath.Join(cfgDir, secretKeyFileName))
	if err != nil {
		return nil, fmt.Errorf("secret store key: %w", err)
	}
	return &SecretStore{db: db, key: key}, nil
}

func loadOrCreateSecretKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
		return b, nil
	}
	key := make([]byte, 32) // AES-256
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// Put encrypts plaintext and inserts a new `secrets` row, returning its id.
// Callers store only this id (as `secret_ref`) -- the plaintext never
// touches any other table.
func (s *SecretStore) Put(ownerType, ownerID, kind string, plaintext []byte) (string, error) {
	blob, err := s.encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt secret: %w", err)
	}
	id := uuid.NewString()
	_, err = s.db.Exec(
		`INSERT INTO secrets (id, owner_type, owner_id, kind, storage_backend, encrypted_blob, created_at) VALUES (?, ?, ?, ?, 'local-aesgcm', ?, ?)`,
		id, ownerType, ownerID, kind, blob, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// Get decrypts and returns the plaintext for secret id.
func (s *SecretStore) Get(id string) ([]byte, error) {
	var blob []byte
	err := s.db.QueryRow(`SELECT encrypted_blob FROM secrets WHERE id = ?`, id).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecretNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.decrypt(blob)
}

// Delete removes a secret row. It is not an error to delete an id that
// doesn't exist (idempotent, matching integration/server delete flows that
// call this best-effort alongside their own row delete).
func (s *SecretStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM secrets WHERE id = ?`, id)
	return err
}

func (s *SecretStore) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *SecretStore) decrypt(blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, errors.New("secret blob too short")
	}
	nonce, ciphertext := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
