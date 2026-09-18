//go:build darwin

package security

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

// ErrKeychainUnavailable is returned by the keychain* functions when the OS
// keychain can't be used (denied, locked, or -- on non-darwin platforms
// today -- simply not implemented yet).
var ErrKeychainUnavailable = errors.New("os keychain unavailable")

// keychainServiceName groups every secret Volt stores in the macOS Keychain
// under one "service" name, distinguished from each other by account (see
// keychainAccount in secrets.go).
const keychainServiceName = "volt"

// osKeychainSet stores value in the macOS login keychain via the `security`
// CLI (§9.7: shellout, no cgo dependency) under (keychainServiceName,
// account). Any existing item for the same account is replaced.
func osKeychainSet(account string, value []byte) error {
	_ = exec.Command("security", "delete-generic-password", "-s", keychainServiceName, "-a", account).Run()
	cmd := exec.Command("security", "add-generic-password", "-s", keychainServiceName, "-a", account, "-w", string(value))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: security add-generic-password: %v: %s", ErrKeychainUnavailable, err, stderr.String())
	}
	return nil
}

func osKeychainGet(account string) ([]byte, error) {
	cmd := exec.Command("security", "find-generic-password", "-s", keychainServiceName, "-a", account, "-w")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: security find-generic-password: %v: %s", ErrKeychainUnavailable, err, stderr.String())
	}
	return bytes.TrimRight(out, "\n"), nil
}

func osKeychainDelete(account string) error {
	return exec.Command("security", "delete-generic-password", "-s", keychainServiceName, "-a", account).Run()
}
