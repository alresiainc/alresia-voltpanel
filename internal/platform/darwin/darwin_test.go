//go:build darwin

package darwin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratePlistContent(t *testing.T) {
	content, err := GeneratePlist(Options{
		Label:    "com.alresiainc.volt",
		ExecPath: "/usr/local/bin/voltpanel",
		Args:     []string{"-port", "7788"},
	})
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}

	for _, want := range []string{
		"<key>Label</key><string>com.alresiainc.volt</string>",
		"<string>/usr/local/bin/voltpanel</string>",
		"<string>-port</string>",
		"<string>7788</string>",
		"<key>RunAtLoad</key><true/>",
		"<key>KeepAlive</key><true/>",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("expected plist to contain %q, got:\n%s", want, content)
		}
	}
}

func TestGeneratePlistRespectsRunAtLoadKeepAliveOverrides(t *testing.T) {
	f := false
	content, err := GeneratePlist(Options{
		Label: "com.alresiainc.volt", ExecPath: "/usr/local/bin/voltpanel",
		RunAtLoad: &f, KeepAlive: &f,
	})
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}
	if !strings.Contains(content, "<key>RunAtLoad</key><false/>") {
		t.Errorf("expected RunAtLoad false, got:\n%s", content)
	}
	if !strings.Contains(content, "<key>KeepAlive</key><false/>") {
		t.Errorf("expected KeepAlive false, got:\n%s", content)
	}
}

func TestGeneratePlistRequiresLabelAndExecPath(t *testing.T) {
	if _, err := GeneratePlist(Options{ExecPath: "/x"}); err == nil {
		t.Error("expected an error for a missing Label")
	}
	if _, err := GeneratePlist(Options{Label: "x"}); err == nil {
		t.Error("expected an error for a missing ExecPath")
	}
}

func TestGeneratePlistEscapesSpecialCharacters(t *testing.T) {
	content, err := GeneratePlist(Options{
		Label:    "com.alresiainc.volt",
		ExecPath: "/usr/local/bin/volt<panel>&co",
	})
	if err != nil {
		t.Fatalf("GeneratePlist: %v", err)
	}
	if strings.Contains(content, "volt<panel>&co") {
		t.Errorf("expected special characters to be escaped, got:\n%s", content)
	}
	if !strings.Contains(content, "volt&lt;panel&gt;&amp;co") {
		t.Errorf("expected escaped form present, got:\n%s", content)
	}
}

// TestWritePlistFileWritesExpectedContent exercises the file-generation
// logic (writePlistFile) against a temp directory ONLY -- it must never
// call launchctl or touch the real ~/Library/LaunchAgents. This is the
// test that satisfies "unit-test the file-generation logic against temp
// files/dirs" without invoking a real service registration.
func TestWritePlistFileWritesExpectedContent(t *testing.T) {
	dir := t.TempDir()
	path, err := writePlistFile(dir, Options{
		Label:    "com.alresiainc.volt-test",
		ExecPath: "/usr/local/bin/voltpanel",
		Args:     []string{"-dev"},
	})
	if err != nil {
		t.Fatalf("writePlistFile: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("expected the plist to be written under %s, got %s", dir, path)
	}
	if filepath.Base(path) != "com.alresiainc.volt-test.plist" {
		t.Fatalf("unexpected plist filename: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written plist: %v", err)
	}
	if !strings.Contains(string(b), "com.alresiainc.volt-test") {
		t.Errorf("written plist missing label, got:\n%s", b)
	}
	if !strings.Contains(string(b), "-dev") {
		t.Errorf("written plist missing arg, got:\n%s", b)
	}
}

func TestWritePlistFileCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "LaunchAgents")
	path, err := writePlistFile(dir, Options{Label: "com.alresiainc.volt-test2", ExecPath: "/bin/true"})
	if err != nil {
		t.Fatalf("writePlistFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected plist to exist: %v", err)
	}
}

// TestRealLaunchctlInstallUninstall is the ONLY test in this package that
// would actually shell out to launchctl and touch the real
// ~/Library/LaunchAgents directory. It is skipped unless
// VOLT_REAL_SERVICE_TEST=1 is explicitly set, and must stay that way --
// per the plan's own guidance, real trust-store/service operations belong
// in a dedicated, deliberately-invoked matrix job, never assumed safe to
// run as part of an ordinary `go test ./...`. This implementation never
// sets that env var itself.
func TestRealLaunchctlInstallUninstall(t *testing.T) {
	if os.Getenv("VOLT_REAL_SERVICE_TEST") != "1" {
		t.Skip("skipped by default -- set VOLT_REAL_SERVICE_TEST=1 to actually register a real launchd job")
	}
	label := "com.alresiainc.volt-real-service-test"
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if err := Install(Options{Label: label, ExecPath: execPath, Args: []string{"-version"}}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	t.Cleanup(func() {
		if err := Uninstall(label); err != nil {
			t.Errorf("Uninstall cleanup: %v", err)
		}
	})
	if status, err := Status(label); err != nil || status == "not installed" {
		t.Fatalf("expected the job to be installed, status=%q err=%v", status, err)
	}
}
