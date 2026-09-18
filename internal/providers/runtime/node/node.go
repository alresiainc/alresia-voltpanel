// Package node implements providers.RuntimeProvider for Node.js.
//
// Detection is real, not mocked: it shells out to `node --version` for the
// currently active interpreter on PATH, and reads nvm's version directory
// (~/.nvm/versions/node, or $NVM_DIR if set) for every nvm-managed
// install, since nvm itself is a shell function rather than a binary that
// could be exec'd directly.
//
// Install/Remove/SetDefault are real too (they drive `nvm install` /
// `nvm uninstall` / `nvm alias default` through a login shell, since that's
// the only way to reach a shell-function-based version manager from a
// non-interactive process) -- but per the plan, unit tests must never
// actually install/remove a real Node version. All system interaction goes
// through the installer interface below so tests substitute a fake; only
// node_real_test.go (behind the VOLT_REAL_INSTALL_TEST=1 gate) exercises
// the real nvmInstaller.
package node

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// installer is the seam between Provider and the real system: running
// `node`/`nvm` and reading the nvm versions directory. Tests substitute a
// fake installer instead of shelling out or touching a real install.
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

// New returns a Provider backed by real system calls (nvm + `node
// --version`). See the package doc for exactly what each method does.
func New() *Provider { return &Provider{inst: &nvmInstaller{}} }

// newWithInstaller is used by tests to inject a fake installer.
func newWithInstaller(i installer) *Provider { return &Provider{inst: i} }

func (p *Provider) Kind() string { return "node" }

// DetectInstalled returns the active `node` (if any, marked as default)
// plus every nvm-managed version, deduplicated by version string. Never
// mutates anything.
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

// --- real installer -------------------------------------------------------

type nvmInstaller struct{}

var semverRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

func (nvmInstaller) ActiveVersion(ctx context.Context) (string, string, bool) {
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

var nodeVersionDirRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

func (nvmInstaller) ManagedVersions(ctx context.Context) ([]providers.RuntimeVersion, error) {
	dir := nvmVersionsDir()
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []providers.RuntimeVersion
	for _, e := range entries {
		if !e.IsDir() || !nodeVersionDirRe.MatchString(e.Name()) {
			continue
		}
		out = append(out, providers.RuntimeVersion{
			Version:     strings.TrimPrefix(e.Name(), "v"),
			InstallPath: filepath.Join(dir, e.Name(), "bin", "node"),
		})
	}
	return out, nil
}

func nvmVersionsDir() string {
	if d := os.Getenv("NVM_DIR"); d != "" {
		return filepath.Join(d, "versions", "node")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".nvm", "versions", "node")
}

func (nvmInstaller) Install(ctx context.Context, version string, progress providers.ProgressFunc) error {
	if _, err := exec.LookPath("bash"); err != nil {
		return fmt.Errorf("node: install requires bash + nvm, bash not found: %w", err)
	}
	if progress != nil {
		progress(0, "installing node "+version+" via nvm")
	}
	out, err := exec.CommandContext(ctx, "bash", "-lc", "nvm install "+shellQuote(version)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nvm install %s: %w: %s", version, err, string(out))
	}
	if progress != nil {
		progress(100, "installed node "+version)
	}
	return nil
}

func (nvmInstaller) Remove(ctx context.Context, version string) error {
	out, err := exec.CommandContext(ctx, "bash", "-lc", "nvm uninstall "+shellQuote(version)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nvm uninstall %s: %w: %s", version, err, string(out))
	}
	return nil
}

func (nvmInstaller) SetDefault(ctx context.Context, version string) error {
	out, err := exec.CommandContext(ctx, "bash", "-lc", "nvm alias default "+shellQuote(version)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nvm alias default %s: %w: %s", version, err, string(out))
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
