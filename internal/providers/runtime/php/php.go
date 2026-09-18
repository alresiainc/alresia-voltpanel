// Package php implements providers.RuntimeProvider for PHP.
//
// Detection is real, not mocked: it shells out to `php --version` for the
// currently active interpreter on PATH, and to `update-alternatives
// --list php` on Linux (when present) or a Homebrew Cellar/opt glob on
// macOS to discover every other installed version.
//
// Install/Remove are real too, but only via Homebrew (`brew install
// php@<version>` / `brew uninstall php@<version>`) since that's the one
// package-management path that never needs sudo/root -- matching §8's
// principle that the daemon itself never runs elevated. On systems without
// Homebrew, Install/Remove return a clear error rather than silently doing
// nothing or attempting a privileged apt/dnf call from the daemon.
// SetDefault uses `brew link --overwrite --force php@<version>` when
// Homebrew is present, or `update-alternatives --set php <path>` on Linux
// (a no-op with a clear permission error if not already permitted).
//
// All system interaction goes through the installer interface below so
// unit tests substitute a fake -- per the plan, "don't actually install
// Node/PHP in unit tests." Only real_test.go (behind the
// VOLT_REAL_INSTALL_TEST=1 gate) exercises the real sysInstaller.
package php

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// installer is the seam between Provider and the real system: running
// `php`/`brew`/`update-alternatives`. Tests substitute a fake installer
// instead of shelling out or installing anything real.
type installer interface {
	ActiveVersion(ctx context.Context) (version, path string, ok bool)
	ManagedVersions(ctx context.Context) ([]providers.RuntimeVersion, error)
	Install(ctx context.Context, version string, progress providers.ProgressFunc) error
	Remove(ctx context.Context, version string) error
	SetDefault(ctx context.Context, version string) error
}

// Provider implements providers.RuntimeProvider for PHP.
type Provider struct {
	inst installer
}

// New returns a Provider backed by real system calls. See the package doc
// for exactly what each method does.
func New() *Provider { return &Provider{inst: &sysInstaller{}} }

// newWithInstaller is used by tests to inject a fake installer.
func newWithInstaller(i installer) *Provider { return &Provider{inst: i} }

func (p *Provider) Kind() string { return "php" }

// DetectInstalled returns the active `php` (if any, marked as default)
// plus every other version this installer can find, deduplicated by
// version string. Never mutates anything.
func (p *Provider) DetectInstalled(ctx context.Context) ([]providers.RuntimeVersion, error) {
	managed, err := p.inst.ManagedVersions(ctx)
	if err != nil {
		return nil, fmt.Errorf("php: list managed versions: %w", err)
	}

	out := make([]providers.RuntimeVersion, 0, len(managed)+1)
	seen := make(map[string]bool, len(managed)+1)

	if version, path, ok := p.inst.ActiveVersion(ctx); ok {
		out = append(out, providers.RuntimeVersion{Version: version, InstallPath: path, IsDefault: true})
		seen[version] = true
	}
	for _, v := range managed {
		if seen[v.Version] {
			continue
		}
		seen[v.Version] = true
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func (p *Provider) Install(ctx context.Context, version string, progress providers.ProgressFunc) error {
	if version == "" {
		return fmt.Errorf("php: version must not be empty")
	}
	return p.inst.Install(ctx, version, progress)
}

func (p *Provider) Remove(ctx context.Context, version string) error {
	if version == "" {
		return fmt.Errorf("php: version must not be empty")
	}
	return p.inst.Remove(ctx, version)
}

func (p *Provider) SetDefault(ctx context.Context, version string) error {
	if version == "" {
		return fmt.Errorf("php: version must not be empty")
	}
	return p.inst.SetDefault(ctx, version)
}

// --- real installer -------------------------------------------------------

type sysInstaller struct{}

var phpVersionRe = regexp.MustCompile(`PHP (\d+\.\d+\.\d+)`)

func (sysInstaller) ActiveVersion(ctx context.Context) (string, string, bool) {
	path, err := exec.LookPath("php")
	if err != nil {
		return "", "", false
	}
	out, err := exec.CommandContext(ctx, "php", "--version").Output()
	if err != nil {
		return "", "", false
	}
	m := phpVersionRe.FindStringSubmatch(string(out))
	if len(m) != 2 {
		return "", "", false
	}
	return m[1], path, true
}

func (s sysInstaller) ManagedVersions(ctx context.Context) ([]providers.RuntimeVersion, error) {
	var out []providers.RuntimeVersion
	if runtime.GOOS == "linux" {
		if v, err := s.updateAlternativesVersions(ctx); err == nil {
			out = append(out, v...)
		}
	}
	if brewVersions, err := s.brewVersions(ctx); err == nil {
		out = append(out, brewVersions...)
	}
	return out, nil
}

// updateAlternativesVersions asks Debian/Ubuntu-style `update-alternatives`
// for every registered php alternative, then runs each one with
// `--version` to learn its actual version string.
func (sysInstaller) updateAlternativesVersions(ctx context.Context) ([]providers.RuntimeVersion, error) {
	if _, err := exec.LookPath("update-alternatives"); err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "update-alternatives", "--list", "php").Output()
	if err != nil {
		return nil, err
	}
	var versions []providers.RuntimeVersion
	for _, path := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		verOut, err := exec.CommandContext(ctx, path, "--version").Output()
		if err != nil {
			continue
		}
		m := phpVersionRe.FindStringSubmatch(string(verOut))
		if len(m) != 2 {
			continue
		}
		versions = append(versions, providers.RuntimeVersion{Version: m[1], InstallPath: path})
	}
	return versions, nil
}

// brewVersions globs Homebrew's opt prefix for every installed php@X.Y
// keg (both Apple Silicon and Intel prefixes) and runs each with
// `--version` to learn its actual version string.
func (sysInstaller) brewVersions(ctx context.Context) ([]providers.RuntimeVersion, error) {
	prefixes := []string{"/opt/homebrew/opt", "/usr/local/opt"}
	var versions []providers.RuntimeVersion
	for _, prefix := range prefixes {
		matches, err := filepath.Glob(filepath.Join(prefix, "php@*", "bin", "php"))
		if err != nil {
			continue
		}
		for _, path := range matches {
			out, err := exec.CommandContext(ctx, path, "--version").Output()
			if err != nil {
				continue
			}
			m := phpVersionRe.FindStringSubmatch(string(out))
			if len(m) != 2 {
				continue
			}
			versions = append(versions, providers.RuntimeVersion{Version: m[1], InstallPath: path})
		}
	}
	return versions, nil
}

func (sysInstaller) Install(ctx context.Context, version string, progress providers.ProgressFunc) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("php: no supported installer found (Homebrew required; automatic install is not attempted on package managers that need root, per §8)")
	}
	formula := "php@" + majorMinor(version)
	if progress != nil {
		progress(0, "installing "+formula+" via brew")
	}
	out, err := exec.CommandContext(ctx, "brew", "install", formula).CombinedOutput()
	if err != nil {
		return fmt.Errorf("brew install %s: %w: %s", formula, err, string(out))
	}
	if progress != nil {
		progress(100, "installed "+formula)
	}
	return nil
}

func (sysInstaller) Remove(ctx context.Context, version string) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("php: no supported installer found (Homebrew required)")
	}
	formula := "php@" + majorMinor(version)
	out, err := exec.CommandContext(ctx, "brew", "uninstall", formula).CombinedOutput()
	if err != nil {
		return fmt.Errorf("brew uninstall %s: %w: %s", formula, err, string(out))
	}
	return nil
}

func (sysInstaller) SetDefault(ctx context.Context, version string) error {
	if _, err := exec.LookPath("brew"); err == nil {
		formula := "php@" + majorMinor(version)
		out, err := exec.CommandContext(ctx, "brew", "link", "--overwrite", "--force", formula).CombinedOutput()
		if err != nil {
			return fmt.Errorf("brew link --overwrite --force %s: %w: %s", formula, err, string(out))
		}
		return nil
	}
	if _, err := exec.LookPath("update-alternatives"); err == nil {
		// update-alternatives needs the target's registered path, not the
		// bare version -- resolve it via --list first.
		out, err := exec.CommandContext(ctx, "update-alternatives", "--list", "php").Output()
		if err != nil {
			return fmt.Errorf("update-alternatives --list php: %w", err)
		}
		for _, path := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			verOut, err := exec.CommandContext(ctx, path, "--version").Output()
			if err != nil {
				continue
			}
			m := phpVersionRe.FindStringSubmatch(string(verOut))
			if len(m) == 2 && m[1] == version {
				setOut, err := exec.CommandContext(ctx, "update-alternatives", "--set", "php", path).CombinedOutput()
				if err != nil {
					return fmt.Errorf("update-alternatives --set php %s: %w: %s", path, err, string(setOut))
				}
				return nil
			}
		}
		return fmt.Errorf("php: version %s not found among update-alternatives entries", version)
	}
	return fmt.Errorf("php: no supported version manager found (Homebrew or update-alternatives required)")
}

// majorMinor trims a full semver (e.g. "8.3.1") down to "8.3", which is
// how Homebrew names its php@ formulas.
func majorMinor(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}
