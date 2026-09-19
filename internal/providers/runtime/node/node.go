// Package node implements providers.RuntimeProvider for Node.js.
//
// Detection, install, remove, and set-default are all real, and all
// native: this package downloads the exact same official prebuilt
// binaries nodejs.org publishes for every release (the same ones nvm/fnm/
// volta fetch under the hood) directly -- no Homebrew, no nvm, no shell
// dependency of any kind. That's a deliberate choice: Node is one of the
// few pieces of software in VoltPanel's "install fresh software" story
// that vendor-ships real prebuilt binaries for macOS, Linux, and Windows
// alike, which is exactly what makes a genuinely native, cross-platform
// version manager possible for it (see the package doc for why PHP can't
// do the same thing).
//
// Downloads are verified against nodejs.org's own published SHA256
// checksums before anything is extracted. Installed versions live under
// <VoltPanel config dir>/runtimes/node/<version>, entirely separate from
// any system Node install or nvm -- switching VoltPanel's "default"
// version never touches anything outside that directory (no symlink into
// /usr/local/bin, which would need permissions this daemon deliberately
// never takes). ActiveVersion still reports whatever `node` happens to be
// on PATH, informationally, but SetDefault only ever affects VoltPanel's
// own record of which managed version is "current".
//
// Per the plan, unit tests never touch the network or a real install --
// see installer_test.go for the fake-installer-backed unit tests and
// real_test.go (behind VOLT_REAL_INSTALL_TEST=1) for the one gated test
// that exercises a real download.
package node

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// installer is the seam between Provider and the real system. Tests
// substitute a fake installer instead of touching the network or a real
// install.
type installer interface {
	ActiveVersion(ctx context.Context) (version, path string, ok bool)
	ManagedVersions(ctx context.Context) ([]providers.RuntimeVersion, error)
	Install(ctx context.Context, version string, progress providers.ProgressFunc) error
	Remove(ctx context.Context, version string) error
	SetDefault(ctx context.Context, version string) error
}

// Provider implements providers.RuntimeProvider for Node.js.
type Provider struct {
	inst installer
}

// New returns a Provider that downloads real nodejs.org binaries into
// baseDir (typically <cfgDir>/runtimes/node).
func New(baseDir string) *Provider { return &Provider{inst: &nativeInstaller{baseDir: baseDir}} }

// newWithInstaller is used by tests to inject a fake installer.
func newWithInstaller(i installer) *Provider { return &Provider{inst: i} }

func (p *Provider) Kind() string { return "node" }

// DetectInstalled returns the active `node` (if any, marked as default)
// plus every VoltPanel-managed version, deduplicated by version string.
// Never mutates anything.
func (p *Provider) DetectInstalled(ctx context.Context) ([]providers.RuntimeVersion, error) {
	managed, err := p.inst.ManagedVersions(ctx)
	if err != nil {
		return nil, fmt.Errorf("node: list managed versions: %w", err)
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
		return fmt.Errorf("node: version must not be empty")
	}
	return p.inst.Install(ctx, version, progress)
}

func (p *Provider) Remove(ctx context.Context, version string) error {
	if version == "" {
		return fmt.Errorf("node: version must not be empty")
	}
	return p.inst.Remove(ctx, version)
}

func (p *Provider) SetDefault(ctx context.Context, version string) error {
	if version == "" {
		return fmt.Errorf("node: version must not be empty")
	}
	return p.inst.SetDefault(ctx, version)
}

var semverRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// activeVersionOnPath reports whatever `node` is on PATH right now,
// regardless of who installed it (system package, nvm, VoltPanel, ...).
// Shared by nativeInstaller as the fallback "detected but not
// VoltPanel-managed" signal when no managed default is set yet.
func activeVersionOnPath(ctx context.Context) (string, string, bool) {
	path, err := exec.LookPath("node")
	if err != nil {
		return "", "", false
	}
	out, err := exec.CommandContext(ctx, "node", "--version").Output()
	if err != nil {
		return "", "", false
	}
	v := semverRe.FindString(string(out))
	if v == "" {
		return "", "", false
	}
	return v, path, true
}
