//go:build !darwin

package security

import "errors"

// ErrKeychainUnavailable is returned by the keychain* functions when the OS
// keychain can't be used. No OS-keychain backend is implemented on this
// platform yet (Linux Secret Service / Windows Credential Manager are
// candidates for a future phase per §9.7) -- SecretStore transparently
// falls back to the encrypted-blob backend whenever this is returned, so
// callers on Linux/Windows still get a real secret store today, just always
// via the encrypted-blob tier.
var ErrKeychainUnavailable = errors.New("os keychain unavailable on this platform")

func osKeychainSet(account string, value []byte) error { return ErrKeychainUnavailable }

func osKeychainGet(account string) ([]byte, error) { return nil, ErrKeychainUnavailable }

func osKeychainDelete(account string) error { return nil }
