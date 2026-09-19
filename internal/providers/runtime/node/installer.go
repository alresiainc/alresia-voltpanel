package node

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// nativeInstaller downloads real nodejs.org release binaries and manages
// them entirely under baseDir -- see the package doc for why this needs
// no nvm/Homebrew/shell dependency at all, unlike PHP.
type nativeInstaller struct {
	baseDir string
	client  *http.Client
}

func (n *nativeInstaller) httpClient() *http.Client {
	if n.client != nil {
		return n.client
	}
	return &http.Client{Timeout: 3 * time.Minute}
}

// defaultFile records which managed version is "current" -- a plain text
// file rather than a symlink specifically so this works identically on
// Windows, where creating a symlink needs Developer Mode or admin rights
// (neither of which this daemon ever requires elsewhere).
func (n *nativeInstaller) defaultFile() string { return filepath.Join(n.baseDir, "default.txt") }

func (n *nativeInstaller) versionDir(version string) string { return filepath.Join(n.baseDir, version) }

// binPath returns where the node executable lives inside an extracted
// version directory -- Unix tarballs nest it under bin/, the Windows zip
// puts node.exe at the top level.
func binPath(versionDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(versionDir, "node.exe")
	}
	return filepath.Join(versionDir, "bin", "node")
}

func (n *nativeInstaller) ActiveVersion(ctx context.Context) (string, string, bool) {
	if v, ok := n.readDefault(); ok {
		if _, err := os.Stat(binPath(n.versionDir(v))); err == nil {
			return v, binPath(n.versionDir(v)), true
		}
	}
	return activeVersionOnPath(ctx)
}

func (n *nativeInstaller) readDefault() (string, bool) {
	b, err := os.ReadFile(n.defaultFile())
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(b))
	return v, v != ""
}

func (n *nativeInstaller) ManagedVersions(ctx context.Context) ([]providers.RuntimeVersion, error) {
	entries, err := os.ReadDir(n.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	def, _ := n.readDefault()
	var out []providers.RuntimeVersion
	for _, e := range entries {
		if !e.IsDir() || !semverRe.MatchString(e.Name()) {
			continue
		}
		path := binPath(n.versionDir(e.Name()))
		if _, err := os.Stat(path); err != nil {
			continue // a partial/failed install dir, not a real version
		}
		out = append(out, providers.RuntimeVersion{Version: e.Name(), InstallPath: path, IsDefault: e.Name() == def})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Install downloads, verifies, and extracts one Node.js release. It
// stages the extraction in "<version>.tmp" and only renames it into
// place on full success, so a failed/interrupted install never leaves a
// half-extracted directory that ManagedVersions would mistake for a real
// one (ManagedVersions already guards on the binary actually existing,
// but this keeps a crashed install from leaving debris behind at all).
func (n *nativeInstaller) Install(ctx context.Context, version string, progress providers.ProgressFunc) error {
	version = strings.TrimPrefix(version, "v")
	if !semverRe.MatchString(version) {
		return fmt.Errorf("node: %q doesn't look like a version (expected e.g. \"20.11.0\")", version)
	}
	report := func(pct int, msg string) {
		if progress != nil {
			progress(pct, msg)
		}
	}

	archiveName, isZip, err := releaseArchiveName(version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	baseURL := fmt.Sprintf("https://nodejs.org/dist/v%s", version)

	if err := os.MkdirAll(n.baseDir, 0o755); err != nil {
		return fmt.Errorf("node: create runtimes dir: %w", err)
	}
	tmpFile, err := os.CreateTemp("", "volt-node-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	report(5, "downloading "+archiveName)
	if err := n.download(ctx, baseURL+"/"+archiveName, tmpFile); err != nil {
		tmpFile.Close()
		return fmt.Errorf("node: download %s: %w", archiveName, err)
	}
	tmpFile.Close()

	report(60, "verifying checksum")
	if err := n.verifyChecksum(ctx, baseURL+"/SHASUMS256.txt", archiveName, tmpPath); err != nil {
		return fmt.Errorf("node: %w", err)
	}

	report(75, "extracting")
	stagingDir := n.versionDir(version) + ".tmp"
	_ = os.RemoveAll(stagingDir)
	if isZip {
		err = extractZip(tmpPath, stagingDir)
	} else {
		err = extractTarGz(tmpPath, stagingDir)
	}
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return fmt.Errorf("node: extract: %w", err)
	}

	finalDir := n.versionDir(version)
	_ = os.RemoveAll(finalDir)
	if err := os.Rename(stagingDir, finalDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return fmt.Errorf("node: finalize install: %w", err)
	}

	report(100, "installed node "+version)
	return nil
}

func (n *nativeInstaller) download(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := n.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s (is %q a real Node.js version?)", resp.Status, url)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// verifyChecksum downloads nodejs.org's own published SHASUMS256.txt for
// this release and confirms the file we just downloaded matches --
// installing and running an unverified binary defeats the point of
// fetching straight from the vendor in the first place.
func (n *nativeInstaller) verifyChecksum(ctx context.Context, checksumsURL, archiveName, filePath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumsURL, nil)
	if err != nil {
		return err
	}
	resp, err := n.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch checksums: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	expected, err := parseChecksum(string(body), archiveName)
	if err != nil {
		return err
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", archiveName, expected, actual)
	}
	return nil
}

// parseChecksum finds "<sha256>  <filename>" in a SHASUMS256.txt body.
func parseChecksum(body, filename string) (string, error) {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == filename {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum entry found for %s", filename)
}

func (n *nativeInstaller) Remove(ctx context.Context, version string) error {
	version = strings.TrimPrefix(version, "v")
	dir := n.versionDir(version)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("node: version %s is not installed", version)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if def, ok := n.readDefault(); ok && def == version {
		_ = os.Remove(n.defaultFile())
	}
	return nil
}

func (n *nativeInstaller) SetDefault(ctx context.Context, version string) error {
	version = strings.TrimPrefix(version, "v")
	if _, err := os.Stat(binPath(n.versionDir(version))); err != nil {
		return fmt.Errorf("node: version %s is not installed -- install it first", version)
	}
	if err := os.MkdirAll(n.baseDir, 0o755); err != nil {
		return err
	}
	tmp := n.defaultFile() + ".tmp"
	if err := os.WriteFile(tmp, []byte(version), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, n.defaultFile())
}

// releaseArchiveName maps a Go OS/arch pair to nodejs.org's own release
// naming convention. goos/goarch are always runtime.GOOS/runtime.GOARCH
// in production; passed as arguments so tests can exercise every branch
// without needing to cross-compile.
func releaseArchiveName(version, goos, goarch string) (name string, isZip bool, err error) {
	var osPart string
	switch goos {
	case "darwin":
		osPart = "darwin"
	case "linux":
		osPart = "linux"
	case "windows":
		osPart = "win"
	default:
		return "", false, fmt.Errorf("node: unsupported OS %q", goos)
	}

	var archPart string
	switch goarch {
	case "amd64":
		archPart = "x64"
	case "arm64":
		archPart = "arm64"
	default:
		return "", false, fmt.Errorf("node: unsupported architecture %q", goarch)
	}

	if goos == "windows" {
		return fmt.Sprintf("node-v%s-%s-%s.zip", version, osPart, archPart), true, nil
	}
	return fmt.Sprintf("node-v%s-%s-%s.tar.gz", version, osPart, archPart), false, nil
}

// extractTarGz extracts a .tar.gz into dstDir, stripping the archive's
// single top-level directory (node-vX.Y.Z-os-arch/...) so dstDir itself
// ends up holding bin/, lib/, etc. directly.
func extractTarGz(archivePath, dstDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel := stripTopLevel(hdr.Name)
		if rel == "" {
			continue
		}
		target, err := safeJoin(dstDir, rel)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			// Node's tarballs don't ship symlinks in practice; skip
			// rather than risk writing one outside dstDir.
			continue
		}
	}
}

// extractZip extracts a .zip into dstDir, stripping the archive's single
// top-level directory the same way extractTarGz does.
func extractZip(archivePath, dstDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		rel := stripTopLevel(f.Name)
		if rel == "" {
			continue
		}
		target, err := safeJoin(dstDir, rel)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(out, src)
		src.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// stripTopLevel removes an archive entry's first path segment (the
// "node-vX.Y.Z-os-arch" directory every official release archive is
// wrapped in) and normalizes slashes. Returns "" for the top-level entry
// itself (nothing to extract).
func stripTopLevel(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	i := strings.IndexByte(name, '/')
	if i < 0 {
		return ""
	}
	return name[i+1:]
}

// safeJoin joins dstDir with an archive-provided relative path,
// rejecting anything that would escape dstDir (a zip-slip guard) --
// defensive even though these archives come straight from nodejs.org,
// since nothing about extracting a remote archive should ever trust its
// internal paths blindly.
func safeJoin(dstDir, rel string) (string, error) {
	clean := filepath.Join(dstDir, rel)
	if !strings.HasPrefix(clean, filepath.Clean(dstDir)+string(filepath.Separator)) && clean != filepath.Clean(dstDir) {
		return "", fmt.Errorf("archive entry %q escapes destination directory", rel)
	}
	return clean, nil
}
