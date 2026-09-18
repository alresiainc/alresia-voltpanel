//go:build linux

package linux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateUnitContent(t *testing.T) {
	content, err := GenerateUnit(Options{
		Name:     "volt",
		ExecPath: "/usr/bin/voltpanel",
		Args:     []string{"-port", "7788"},
	})
	if err != nil {
		t.Fatalf("GenerateUnit: %v", err)
	}
	for _, want := range []string{
		"Description=Alresia Volt Daemon",
		"ExecStart=/usr/bin/voltpanel -port 7788",
		"Restart=always",
		"WantedBy=default.target",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("expected unit to contain %q, got:\n%s", want, content)
		}
	}
}

func TestGenerateUnitQuotesArgsWithSpaces(t *testing.T) {
	content, err := GenerateUnit(Options{Name: "volt", ExecPath: "/usr/bin/voltpanel", Args: []string{"--label", "hello world"}})
	if err != nil {
		t.Fatalf("GenerateUnit: %v", err)
	}
	if !strings.Contains(content, `"hello world"`) {
		t.Errorf("expected the spaced arg to be quoted, got:\n%s", content)
	}
}

func TestGenerateUnitRequiresNameAndExecPath(t *testing.T) {
	if _, err := GenerateUnit(Options{ExecPath: "/x"}); err == nil {
		t.Error("expected an error for a missing Name")
	}
	if _, err := GenerateUnit(Options{Name: "x"}); err == nil {
		t.Error("expected an error for a missing ExecPath")
	}
}

func TestGenerateUnitRespectsRestartOverride(t *testing.T) {
	content, err := GenerateUnit(Options{Name: "volt", ExecPath: "/usr/bin/voltpanel", Restart: "on-failure"})
	if err != nil {
		t.Fatalf("GenerateUnit: %v", err)
	}
	if !strings.Contains(content, "Restart=on-failure") {
		t.Errorf("expected Restart=on-failure, got:\n%s", content)
	}
}

// TestWriteUnitFileWritesExpectedContent exercises the file-generation
// logic only, against a temp directory -- never systemctl, never the real
// ~/.config/systemd/user.
func TestWriteUnitFileWritesExpectedContent(t *testing.T) {
	dir := t.TempDir()
	path, err := writeUnitFile(dir, Options{Name: "volt-test", ExecPath: "/usr/bin/voltpanel", Args: []string{"-dev"}})
	if err != nil {
		t.Fatalf("writeUnitFile: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("expected the unit to be written under %s, got %s", dir, path)
	}
	if filepath.Base(path) != "volt-test.service" {
		t.Fatalf("unexpected unit filename: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written unit: %v", err)
	}
	if !strings.Contains(string(b), "-dev") {
		t.Errorf("written unit missing arg, got:\n%s", b)
	}
}

// TestRealSystemctlInstallUninstall is the ONLY test in this package that
// would actually shell out to systemctl and register a real unit. Skipped
// unless VOLT_REAL_SERVICE_TEST=1 is explicitly set; this implementation
// never sets that variable itself, per the plan's guidance that real
// service-registration operations belong in a dedicated matrix job, not an
// ordinary `go test ./...` run.
func TestRealSystemctlInstallUninstall(t *testing.T) {
	if os.Getenv("VOLT_REAL_SERVICE_TEST") != "1" {
		t.Skip("skipped by default -- set VOLT_REAL_SERVICE_TEST=1 to actually register a real systemd user unit")
	}
	name := "volt-real-service-test"
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if err := Install(Options{Name: name, ExecPath: execPath, Args: []string{"-version"}}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	t.Cleanup(func() {
		if err := Uninstall(name); err != nil {
			t.Errorf("Uninstall cleanup: %v", err)
		}
	})
	if status, err := Status(name); err != nil || status == "not installed" {
		t.Fatalf("expected the unit to be installed, status=%q err=%v", status, err)
	}
}
