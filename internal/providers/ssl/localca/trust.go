package localca

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrTrustNotConfirmed is returned by TrustCA whenever confirmed is not
// exactly true. Per §7/§9.6/§22 of the plan, this is the one operation in
// the whole provider that would modify the real OS/browser trust store, so
// it never runs implicitly -- confirmed must come from an explicit user
// action, checked here server-side, not just a UI checkbox.
var ErrTrustNotConfirmed = errors.New("localca: TrustCA requires confirmed=true -- refusing to modify the OS/browser trust store without explicit confirmation")

// commandRunner is the seam between TrustCA and the real system: running
// the OS-specific trust-store command. Tests substitute a fake runner so
// they can assert exactly what *would* run without ever executing it --
// mirroring the installer seam in internal/providers/runtime/node.
type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type realRunner struct{}

func (realRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// newWithRunner is used by this package's own tests to inject a fake
// commandRunner, so TrustCA's real code path runs end-to-end (including
// the confirmed-gate and command-construction logic) without ever shelling
// out for real.
func newWithRunner(dir string, r commandRunner) *Provider {
	return &Provider{Dir: dir, runner: r}
}

// TrustCA installs the local CA certificate into this OS's trust store so
// browsers stop warning about certificates it issues. It refuses outright
// unless confirmed is exactly true (ErrTrustNotConfirmed) -- callers
// (internal/api/v1/ssl.go) must plumb a real, explicit user confirmation
// through to this argument, never hardcode true.
//
// This repository's own tests call TrustCA only with a fake commandRunner
// injected via newWithRunner -- never the real realRunner{} -- specifically
// so no test run ever modifies this machine's actual trust store.
func (p *Provider) TrustCA(ctx context.Context, confirmed bool) error {
	if !confirmed {
		return ErrTrustNotConfirmed
	}

	p.mu.Lock()
	if _, err := p.ensureCALocked(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("localca: ensure CA before trust: %w", err)
	}
	p.mu.Unlock()

	name, args, err := p.trustCommand(runtime.GOOS, detectLinuxCAFamily())
	if err != nil {
		return err
	}
	out, err := p.runner.Run(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("localca: trust-store command failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// Best-effort: failing to write this marker doesn't undo the real
	// trust-store change that already happened, it just means CAInfo.Trusted
	// won't reflect it until the next successful TrustCA call.
	_ = os.WriteFile(p.trustedMarkerPath(), []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
	return nil
}

// trustCommand builds the exact OS-specific command TrustCA would run,
// given goos (normally runtime.GOOS) and, for Linux, which CA-trust
// tooling family is present. It performs no I/O and executes nothing --
// that's what makes it directly unit-testable for all three OS branches
// from a single host.
func (p *Provider) trustCommand(goos, linuxFamily string) (name string, args []string, err error) {
	caCert := p.caCertPath()
	switch goos {
	case "darwin":
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", nil, fmt.Errorf("localca: locate login keychain: %w", herr)
		}
		keychain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
		// Login keychain (not System.keychain) so this never requires the
		// daemon to run elevated -- macOS still prompts the user to confirm
		// via a GUI dialog, which is the point (§8/§9.6: explicit user
		// confirmation, never silent).
		return "security", []string{"add-trusted-cert", "-d", "-r", "trustRoot", "-k", keychain, caCert}, nil

	case "linux":
		switch linuxFamily {
		case "fedora":
			dest := "/etc/pki/ca-trust/source/anchors/volt-local-ca.pem"
			cmd := fmt.Sprintf("cp %s %s && update-ca-trust extract", shellQuote(caCert), shellQuote(dest))
			return "sh", []string{"-c", cmd}, nil
		default: // debian/ubuntu, and the catch-all default
			dest := "/usr/local/share/ca-certificates/volt-local-ca.crt"
			cmd := fmt.Sprintf("cp %s %s && update-ca-certificates", shellQuote(caCert), shellQuote(dest))
			return "sh", []string{"-c", cmd}, nil
		}

	case "windows":
		return "certutil", []string{"-addstore", "-f", "ROOT", caCert}, nil

	default:
		return "", nil, fmt.Errorf("localca: TrustCA is not implemented for GOOS=%q", goos)
	}
}

// detectLinuxCAFamily probes for the Fedora/RHEL-family trust tooling
// (update-ca-trust); anything else defaults to the Debian/Ubuntu family
// (update-ca-certificates), which is by far the more common case. Only
// consulted on the real TrustCA path -- trustCommand itself takes the
// family as a plain argument so tests can exercise both branches directly
// regardless of which family (if any) the test host actually has.
func detectLinuxCAFamily() string {
	if _, err := exec.LookPath("update-ca-trust"); err == nil {
		return "fedora"
	}
	return "debian"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
