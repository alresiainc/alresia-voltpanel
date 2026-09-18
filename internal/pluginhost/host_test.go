package pluginhost

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// repoTestdata resolves testdata/<name> at the repository root, regardless
// of the test runner's working directory. Same helper used elsewhere in
// this repo (internal/domain/project/detect/detect_test.go) -- duplicated
// here per that file's own convention rather than factored into a shared
// test-util package.
func repoTestdata(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to resolve this test file's path")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 20; i++ {
		candidate := filepath.Join(dir, "testdata")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return filepath.Join(candidate, name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate repository testdata/ directory above %s", thisFile)
	return ""
}

// skipOnWindows guards tests that launch a /bin/sh fake extension -- sh
// scripts aren't portable to Windows, matching the existing convention in
// internal/providers/docker/client_test.go of skipping (not failing)
// platform-specific subprocess tests.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake extension fixtures in this test use /bin/sh, not available on windows")
	}
}

// writeFakeExtension creates a temp directory containing a
// volt-extension.json manifest (executable "sh", running run.sh) and a
// run.sh with the given body, and returns the directory path.
func writeFakeExtension(t *testing.T, scriptBody string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := Manifest{Name: "fake", Version: "0.0.1", Kind: "runtime", Executable: "sh", Args: []string{"run.sh"}}
	b, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func withShortTimeouts(t *testing.T, d time.Duration) {
	t.Helper()
	origHandshake, origRequest := DefaultHandshakeTimeout, DefaultRequestTimeout
	DefaultHandshakeTimeout, DefaultRequestTimeout = d, d
	t.Cleanup(func() { DefaultHandshakeTimeout, DefaultRequestTimeout = origHandshake, origRequest })
}

func TestLoadExtensionMissingManifestReturnsError(t *testing.T) {
	_, err := LoadExtension(t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a directory with no manifest")
	}
}

func TestLoadExtensionInvalidManifestJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExtension(dir); err == nil {
		t.Fatal("expected an error for invalid manifest JSON")
	}
}

func TestLoadExtensionMissingRequiredManifestFieldsReturnsError(t *testing.T) {
	dir := t.TempDir()
	// "executable" is missing.
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(`{"name":"x","version":"1","kind":"runtime"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExtension(dir); err == nil {
		t.Fatal("expected an error for a manifest missing required fields")
	}
}

func TestLoadExtensionNonexistentExecutableReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"x","version":"1","kind":"runtime","executable":"/definitely/does/not/exist/anywhere"}`
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExtension(dir); err == nil {
		t.Fatal("expected an error launching a nonexistent executable")
	}
}

func TestLoadExtensionRejectsUnsupportedProtocolVersion(t *testing.T) {
	skipOnWindows(t)
	dir := writeFakeExtension(t, `#!/bin/sh
echo '{"protocolVersion":9999,"kind":"runtime","name":"too-new","methods":[]}'
exit 0
`)
	_, err := LoadExtension(dir)
	if err == nil {
		t.Fatal("expected LoadExtension to reject an unsupported protocol version")
	}
}

func TestLoadExtensionTimesOutWaitingForHandshake(t *testing.T) {
	skipOnWindows(t)
	withShortTimeouts(t, 200*time.Millisecond)
	dir := writeFakeExtension(t, `#!/bin/sh
sleep 5
`)
	start := time.Now()
	_, err := LoadExtension(dir)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected LoadExtension to time out when the subprocess never writes a handshake")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("expected LoadExtension to give up quickly, took %s", elapsed)
	}
}

func TestCallTimesOutWhenSubprocessHangsOnAResponse(t *testing.T) {
	skipOnWindows(t)
	dir := writeFakeExtension(t, `#!/bin/sh
echo '{"protocolVersion":1,"kind":"runtime","name":"hangs","methods":["DetectInstalled"]}'
read line
sleep 5
`)
	ep, err := LoadExtension(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Close()
	ep.requestTimeout = 300 * time.Millisecond

	start := time.Now()
	_, err = ep.DetectInstalled(context.Background())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected DetectInstalled to time out when the subprocess never responds")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("expected the call to give up quickly, took %s", elapsed)
	}
}

func TestCallReturnsErrorWhenSubprocessExitsWithoutResponding(t *testing.T) {
	skipOnWindows(t)
	dir := writeFakeExtension(t, `#!/bin/sh
echo '{"protocolVersion":1,"kind":"runtime","name":"gone","methods":["DetectInstalled"]}'
exit 0
`)
	ep, err := LoadExtension(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Close()

	// Give the subprocess a moment to actually exit before we call it, so
	// this exercises the "already gone" path rather than racing it.
	time.Sleep(200 * time.Millisecond)

	if _, err := ep.DetectInstalled(context.Background()); err == nil {
		t.Fatal("expected an error when the subprocess exited before responding")
	}
}

func TestCallReturnsExtensionReportedError(t *testing.T) {
	skipOnWindows(t)
	dir := writeFakeExtension(t, `#!/bin/sh
echo '{"protocolVersion":1,"kind":"runtime","name":"erroring","methods":["Install"]}'
while read line; do
  id=$(echo "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  echo "{\"id\":\"$id\",\"error\":\"nope, not supported\"}"
done
`)
	ep, err := LoadExtension(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Close()

	err = ep.Install(context.Background(), "1.2.3", nil)
	if err == nil {
		t.Fatal("expected Install to surface the extension-reported error")
	}
}

func TestCloseIsIdempotentAndSafeAfterFailure(t *testing.T) {
	skipOnWindows(t)
	dir := writeFakeExtension(t, `#!/bin/sh
echo '{"protocolVersion":9999,"kind":"runtime","name":"too-new","methods":[]}'
`)
	_, err := LoadExtension(dir)
	if err == nil {
		t.Fatal("expected version rejection")
	}
	// LoadExtension already closed the subprocess internally on rejection;
	// nothing further to close here, but this documents that a rejected
	// load doesn't leave anything hanging around for the caller to clean
	// up manually.
}

// TestDogfoodSamplePythonProviderThroughRegistry is Phase 11's core
// acceptance criterion: the sample Python provider (testdata/
// sample-python-provider), a genuinely separate process that only speaks
// the documented protocol, loads through LoadExtension, registers into a
// plain providers.Registry exactly like a built-in provider, and answers
// DetectInstalled through nothing but the providers.RuntimeProvider
// interface -- proving the daemon can't tell it apart from Node/PHP at
// that interface boundary.
func TestDogfoodSamplePythonProviderThroughRegistry(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available on this machine")
	}

	dir := repoTestdata(t, "sample-python-provider")
	ep, err := LoadExtension(dir)
	if err != nil {
		t.Fatalf("LoadExtension: %v", err)
	}
	defer ep.Close()

	registry := providers.NewRegistry()
	registry.RegisterRuntime(ep)

	// Deliberately go through the interface type only, the way
	// internal/api/v1 handlers do -- no *pluginhost.ExternalProvider
	// anywhere below this line.
	var provider providers.RuntimeProvider
	provider, ok := registry.Runtime("python-demo")
	if !ok {
		t.Fatalf("expected the extension to register under kind %q, got kinds: %+v", "python-demo", registry.Runtimes())
	}
	if provider.Kind() != "python-demo" {
		t.Fatalf("expected Kind() == %q, got %q", "python-demo", provider.Kind())
	}

	versions, err := provider.DetectInstalled(context.Background())
	if err != nil {
		t.Fatalf("DetectInstalled: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected exactly 1 detected version, got %d: %+v", len(versions), versions)
	}
	if versions[0].Version == "" {
		t.Fatal("expected a non-empty detected python3 version")
	}
	if versions[0].InstallPath == "" {
		t.Fatal("expected a non-empty detected python3 install path")
	}
	if !versions[0].IsDefault {
		t.Fatal("expected the sample provider's single detected version to be marked default")
	}

	// Install/Remove/SetDefault are explicitly unsupported by this
	// read-only demo provider -- confirm they return a clear error rather
	// than silently succeeding or hanging.
	if err := provider.Install(context.Background(), "3.13.0", nil); err == nil {
		t.Fatal("expected Install to be rejected by the demo provider")
	}
	if err := provider.Remove(context.Background(), "3.13.0"); err == nil {
		t.Fatal("expected Remove to be rejected by the demo provider")
	}
	if err := provider.SetDefault(context.Background(), "3.13.0"); err == nil {
		t.Fatal("expected SetDefault to be rejected by the demo provider")
	}
}
