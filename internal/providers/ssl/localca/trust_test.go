package localca

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records every command it would have run and never actually
// executes anything -- this is how every test in this file verifies
// TrustCA's behavior without ever touching this machine's real trust
// store, per the task's explicit constraint.
type fakeRunner struct {
	calls []call
	err   error
}

type call struct {
	name string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name: name, args: args})
	if f.err != nil {
		return []byte("simulated failure output"), f.err
	}
	return []byte("ok"), nil
}

func TestTrustCARefusesWithoutConfirmation(t *testing.T) {
	runner := &fakeRunner{}
	p := newWithRunner(filepath.Join(t.TempDir(), "ssl"), runner)

	err := p.TrustCA(context.Background(), false)
	if err != ErrTrustNotConfirmed {
		t.Fatalf("expected ErrTrustNotConfirmed, got %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected TrustCA(confirmed=false) to never invoke a command, got %+v", runner.calls)
	}
}

func TestTrustCAWithConfirmationConstructsAndRunsRealOSCommandOnFakeRunner(t *testing.T) {
	// This exercises TrustCA's real production code path (confirmed=true)
	// end-to-end -- including on this actual darwin/linux/windows test
	// machine's runtime.GOOS -- but the injected fakeRunner intercepts the
	// command instead of executing it, so nothing on this machine's real
	// trust store is ever touched.
	runner := &fakeRunner{}
	p := newWithRunner(filepath.Join(t.TempDir(), "ssl"), runner)

	if err := p.TrustCA(context.Background(), true); err != nil {
		t.Fatalf("TrustCA(confirmed=true): %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected exactly one command to be constructed and run, got %+v", runner.calls)
	}
	got := runner.calls[0]
	if got.name == "" {
		t.Fatal("expected a non-empty command name")
	}
	// Whatever OS this test runs on, the constructed command must
	// reference the CA cert file this Provider actually generated.
	found := false
	for _, a := range got.args {
		if strings.Contains(a, "ca-cert.pem") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the constructed command to reference the CA cert file, got %+v", got)
	}
}

func TestTrustCAMarksTrustedOnSuccess(t *testing.T) {
	p := newWithRunner(filepath.Join(t.TempDir(), "ssl"), &fakeRunner{})

	info, err := p.EnsureCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Trusted {
		t.Fatal("expected Trusted=false before TrustCA runs")
	}

	if err := p.TrustCA(context.Background(), true); err != nil {
		t.Fatal(err)
	}

	info2, err := p.EnsureCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info2.Trusted {
		t.Fatal("expected Trusted=true after a successful TrustCA(confirmed=true)")
	}
}

func TestTrustCAPropagatesCommandFailureAndDoesNotMarkTrusted(t *testing.T) {
	runner := &fakeRunner{err: errStub{"security: user declined"}}
	p := newWithRunner(filepath.Join(t.TempDir(), "ssl"), runner)

	if err := p.TrustCA(context.Background(), true); err == nil {
		t.Fatal("expected the simulated command failure to propagate as an error")
	}

	info, err := p.EnsureCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Trusted {
		t.Fatal("expected Trusted=false after a failed TrustCA")
	}
}

type errStub struct{ msg string }

func (e errStub) Error() string { return e.msg }

func TestTrustCommandConstructionPerOS(t *testing.T) {
	p := newWithRunner(filepath.Join(t.TempDir(), "ssl"), &fakeRunner{})
	caCert := p.caCertPath()

	cases := []struct {
		goos        string
		linuxFamily string
		wantName    string
		wantContain []string
	}{
		{goos: "darwin", wantName: "security", wantContain: []string{"add-trusted-cert", "trustRoot", caCert}},
		{goos: "linux", linuxFamily: "debian", wantName: "sh", wantContain: []string{"update-ca-certificates", caCert}},
		{goos: "linux", linuxFamily: "fedora", wantName: "sh", wantContain: []string{"update-ca-trust extract", caCert}},
		{goos: "windows", wantName: "certutil", wantContain: []string{"-addstore", "ROOT", caCert}},
	}

	for _, c := range cases {
		name, args, err := p.trustCommand(c.goos, c.linuxFamily)
		if err != nil {
			t.Fatalf("%s/%s: %v", c.goos, c.linuxFamily, err)
		}
		if name != c.wantName {
			t.Fatalf("%s/%s: expected command %q, got %q", c.goos, c.linuxFamily, c.wantName, name)
		}
		joined := name + " " + strings.Join(args, " ")
		for _, want := range c.wantContain {
			if !strings.Contains(joined, want) {
				t.Fatalf("%s/%s: expected constructed command to contain %q, got: %s", c.goos, c.linuxFamily, want, joined)
			}
		}
	}
}

func TestTrustCommandRejectsUnsupportedOS(t *testing.T) {
	p := newWithRunner(filepath.Join(t.TempDir(), "ssl"), &fakeRunner{})
	if _, _, err := p.trustCommand("plan9", ""); err == nil {
		t.Fatal("expected an error for an unsupported GOOS")
	}
}
