package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoTestdata resolves testdata/<name> at the repository root, regardless
// of the test runner's working directory -- it walks up from this source
// file's own location rather than relying on a fixed number of relative
// ".." segments from the caller's cwd.
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

func TestDetectLaravelFixture(t *testing.T) {
	dir := repoTestdata(t, "laravel-fixture")
	got, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindLaravel {
		t.Fatalf("expected KindLaravel, got %q", got.Kind)
	}
	if got.RunCommand != "php artisan serve" {
		t.Fatalf("expected default Laravel run command, got %q", got.RunCommand)
	}
}

func TestDetectNextJSFixture(t *testing.T) {
	dir := repoTestdata(t, "nextjs-fixture")
	got, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindNextJS {
		t.Fatalf("expected KindNextJS, got %q", got.Kind)
	}
	if got.RunCommand != "npm run dev" {
		t.Fatalf("expected npm run dev (fixture declares a dev script), got %q", got.RunCommand)
	}
}

func TestDetectNodeFixture(t *testing.T) {
	dir := repoTestdata(t, "node-fixture")
	got, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindNode {
		t.Fatalf("expected KindNode, got %q", got.Kind)
	}
	if got.RunCommand != "npm start" {
		t.Fatalf("expected npm start (fixture declares a start script), got %q", got.RunCommand)
	}
}

func TestDetectPHPFixture(t *testing.T) {
	dir := repoTestdata(t, "php-fixture")
	got, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindPHP {
		t.Fatalf("expected KindPHP, got %q", got.Kind)
	}
	if got.RunCommand != "" {
		t.Fatalf("expected no definitive run command for generic PHP, got %q", got.RunCommand)
	}
	if got.Detail == "" {
		t.Fatal("expected a Detail explaining the unknown/generic php result")
	}
}

func TestDetectUnknownForEmptyDir(t *testing.T) {
	dir := t.TempDir()
	got, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindUnknown {
		t.Fatalf("expected KindUnknown for a directory with no markers, got %q", got.Kind)
	}
}

func TestDetectNodeWithoutStartScriptHasNoDefaultCommand(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"name":"no-start","dependencies":{"lodash":"^4.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindNode {
		t.Fatalf("expected KindNode, got %q", got.Kind)
	}
	if got.RunCommand != "" {
		t.Fatalf("expected empty run command when no start script exists, got %q", got.RunCommand)
	}
}

func TestDetectNextJSWithoutDevScriptFallsBackToNextDev(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"name":"bare-next","dependencies":{"next":"^14.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindNextJS {
		t.Fatalf("expected KindNextJS, got %q", got.Kind)
	}
	if got.RunCommand != "next dev" {
		t.Fatalf("expected bare 'next dev' fallback, got %q", got.RunCommand)
	}
}

func TestDetectLaravelByComposerRequireAlone(t *testing.T) {
	dir := t.TempDir()
	composer := `{"require":{"laravel/framework":"^10.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindLaravel {
		t.Fatalf("expected KindLaravel via composer.json require alone (no artisan file), got %q", got.Kind)
	}
}

// registerable is a Detector implemented purely for TestRegisterExtendsRegistry
// -- it proves adding a new framework later is additive (Register + a new
// Detector), not a change to any existing detector.
type registerable struct {
	marker string
	result Result
}

func (r registerable) Detect(dir string) (*Result, error) {
	if !fileExists(dir, r.marker) {
		return nil, nil
	}
	res := r.result
	return &res, nil
}

func TestRegisterExtendsRegistry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".rails-marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	custom := registerable{marker: ".rails-marker", result: Result{Kind: "rails", RunCommand: "rails server"}}

	// DetectWith exercises the extension mechanism without mutating the
	// package-level default registry (keeps this test isolated from the
	// others in this file, which all rely on the built-in defaults).
	got, err := DetectWith(append(append([]Detector{}, defaultDetectors...), custom), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "rails" || got.RunCommand != "rails server" {
		t.Fatalf("expected the appended custom detector to match, got %+v", got)
	}
}
