//go:build linux

// Package linux implements real systemd (--user) unit registration for the
// Volt daemon (§8/§17 Phase 5), replacing internal/system's former no-op
// stubs. Adapted from packaging/voltpanel.service, but installed as a
// per-user unit (systemctl --user) rather than a system-wide one, so no
// root/sudo is required to register the daemon's own autostart -- matching
// §8's "the daemon itself never runs elevated" principle.
//
// Safety note: see internal/platform/darwin's package doc for the same
// caution, mirrored here for systemctl -- writeUnitFile is the pure,
// filesystem-only half that tests exercise directly; Install/Uninstall are
// the real, side-effecting operations this repo's own tests must never
// invoke.
package linux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// Options describes the systemd unit to generate/install.
type Options struct {
	// Name is the unit name without the .service suffix, e.g. "volt".
	Name string
	// Description is the [Unit] Description= value.
	Description string
	// ExecPath is the absolute path to the voltpanel binary.
	ExecPath string
	Args     []string
	// Restart mirrors systemd's Restart= directive; defaults to "always".
	Restart string
}

func applyDefaults(opts Options) Options {
	if opts.Description == "" {
		opts.Description = "Alresia Volt Daemon"
	}
	if opts.Restart == "" {
		opts.Restart = "always"
	}
	return opts
}

const unitTemplateSrc = `[Unit]
Description={{.Description}}
After=network.target

[Service]
Type=simple
ExecStart={{.ExecStart}}
Restart={{.Restart}}

[Install]
WantedBy=default.target
`

var unitTemplate = template.Must(template.New("systemd-unit").Parse(unitTemplateSrc))

type unitData struct {
	Description string
	ExecStart   string
	Restart     string
}

// quoteArg quotes an argument for systemd's ExecStart= line if it contains
// whitespace, matching systemd's own unit-file quoting rules closely
// enough for our purposes (paths/flags, not arbitrary shell content).
func quoteArg(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// GenerateUnit renders the systemd unit file content for opts. Pure
// function, no filesystem or process interaction.
func GenerateUnit(opts Options) (string, error) {
	opts = applyDefaults(opts)
	if opts.Name == "" {
		return "", fmt.Errorf("linux: Name is required")
	}
	if opts.ExecPath == "" {
		return "", fmt.Errorf("linux: ExecPath is required")
	}
	parts := make([]string, 0, len(opts.Args)+1)
	parts = append(parts, quoteArg(opts.ExecPath))
	for _, a := range opts.Args {
		parts = append(parts, quoteArg(a))
	}
	data := unitData{
		Description: opts.Description,
		ExecStart:   strings.Join(parts, " "),
		Restart:     opts.Restart,
	}
	var buf strings.Builder
	if err := unitTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// defaultUnitDir returns the real, per-user systemd unit directory.
func defaultUnitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// writeUnitFile renders opts and writes it to dir/<name>.service, creating
// dir if needed. Pure file I/O -- no systemctl invocation.
func writeUnitFile(dir string, opts Options) (string, error) {
	content, err := GenerateUnit(opts)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, opts.Name+".service")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Install writes the unit file to the real ~/.config/systemd/user and
// enables+starts it via `systemctl --user`. Real, side-effecting
// registration of a persistent background service.
func Install(opts Options) error {
	dir, err := defaultUnitDir()
	if err != nil {
		return fmt.Errorf("linux: locate systemd user unit dir: %w", err)
	}
	if _, err := writeUnitFile(dir, opts); err != nil {
		return fmt.Errorf("linux: write unit file: %w", err)
	}
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("linux: systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	unit := opts.Name + ".service"
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", unit).CombinedOutput(); err != nil {
		return fmt.Errorf("linux: systemctl enable --now: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Uninstall disables and removes the systemd user unit with the given
// name.
func Uninstall(name string) error {
	unit := name + ".service"
	_, _ = exec.Command("systemctl", "--user", "disable", "--now", unit).CombinedOutput()

	dir, err := defaultUnitDir()
	if err != nil {
		return fmt.Errorf("linux: locate systemd user unit dir: %w", err)
	}
	path := filepath.Join(dir, unit)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("linux: remove unit file: %w", err)
	}
	_, _ = exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput()
	return nil
}

// Status reports systemctl's view of the unit.
func Status(name string) (string, error) {
	out, err := exec.Command("systemctl", "--user", "status", name+".service").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "not installed", nil
	}
	return strings.TrimSpace(string(out)), nil
}
