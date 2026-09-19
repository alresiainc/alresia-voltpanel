package node

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseArchiveName(t *testing.T) {
	cases := []struct {
		goos, goarch, wantName string
		wantZip                bool
	}{
		{"darwin", "amd64", "node-v20.11.0-darwin-x64.tar.gz", false},
		{"darwin", "arm64", "node-v20.11.0-darwin-arm64.tar.gz", false},
		{"linux", "amd64", "node-v20.11.0-linux-x64.tar.gz", false},
		{"linux", "arm64", "node-v20.11.0-linux-arm64.tar.gz", false},
		{"windows", "amd64", "node-v20.11.0-win-x64.zip", true},
		{"windows", "arm64", "node-v20.11.0-win-arm64.zip", true},
	}
	for _, c := range cases {
		name, isZip, err := releaseArchiveName("20.11.0", c.goos, c.goarch)
		if err != nil {
			t.Errorf("%s/%s: unexpected error: %v", c.goos, c.goarch, err)
			continue
		}
		if name != c.wantName || isZip != c.wantZip {
			t.Errorf("%s/%s: got (%q, %v), want (%q, %v)", c.goos, c.goarch, name, isZip, c.wantName, c.wantZip)
		}
	}

	if _, _, err := releaseArchiveName("20.11.0", "plan9", "amd64"); err == nil {
		t.Error("expected an unsupported OS to error")
	}
	if _, _, err := releaseArchiveName("20.11.0", "linux", "riscv64"); err == nil {
		t.Error("expected an unsupported architecture to error")
	}
}

func TestParseChecksum(t *testing.T) {
	body := "aaaaaaaa  node-v20.11.0-darwin-arm64.tar.gz\nbbbbbbbb  node-v20.11.0-linux-x64.tar.gz\n"
	got, err := parseChecksum(body, "node-v20.11.0-linux-x64.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "bbbbbbbb" {
		t.Fatalf("expected bbbbbbbb, got %q", got)
	}

	if _, err := parseChecksum(body, "node-v99.0.0-linux-x64.tar.gz"); err == nil {
		t.Fatal("expected an error for a filename not present in the checksum file")
	}
}

func TestStripTopLevel(t *testing.T) {
	cases := map[string]string{
		"node-v20.11.0-darwin-arm64/bin/node": "bin/node",
		"node-v20.11.0-darwin-arm64/":         "",
		"node-v20.11.0-darwin-arm64":          "",
		"node-v20.11.0-win-x64\\node.exe":     "node.exe",
	}
	for in, want := range cases {
		if got := stripTopLevel(in); got != want {
			t.Errorf("stripTopLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeJoinRejectsEscape(t *testing.T) {
	dst := t.TempDir()
	if _, err := safeJoin(dst, "../../etc/passwd"); err == nil {
		t.Fatal("expected a path escaping dstDir to be rejected")
	}
	if _, err := safeJoin(dst, "bin/node"); err != nil {
		t.Fatalf("expected a normal relative path to be accepted, got: %v", err)
	}
}

func TestExtractTarGzStripsTopLevelDir(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"node-v20.11.0-darwin-arm64/bin/node":  "fake binary",
		"node-v20.11.0-darwin-arm64/README.md": "readme",
	}
	for name, content := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))})
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()
	_ = gz.Close()

	archivePath := filepath.Join(t.TempDir(), "node.tar.gz")
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "extracted")
	if err := extractTarGz(archivePath, dst); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "bin", "node"))
	if err != nil {
		t.Fatalf("expected bin/node to exist after extraction: %v", err)
	}
	if string(got) != "fake binary" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestExtractZipStripsTopLevelDir(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("node-v20.11.0-win-x64/node.exe")
	_, _ = w.Write([]byte("fake exe"))
	_ = zw.Close()

	archivePath := filepath.Join(t.TempDir(), "node.zip")
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "extracted")
	if err := extractZip(archivePath, dst); err != nil {
		t.Fatalf("extractZip: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "node.exe"))
	if err != nil {
		t.Fatalf("expected node.exe to exist after extraction: %v", err)
	}
	if string(got) != "fake exe" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestSetDefaultRequiresInstalledVersion(t *testing.T) {
	n := &nativeInstaller{baseDir: t.TempDir()}
	if err := n.SetDefault(context.Background(), "99.0.0"); err == nil {
		t.Fatal("expected an error when setting default to a version that isn't installed")
	}
}

func TestRemoveRequiresInstalledVersion(t *testing.T) {
	n := &nativeInstaller{baseDir: t.TempDir()}
	if err := n.Remove(context.Background(), "99.0.0"); err == nil {
		t.Fatal("expected an error when removing a version that isn't installed")
	}
}
