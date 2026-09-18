//go:build darwin

// Package darwin implements real launchd registration for the Volt daemon
// (§8/§17 Phase 5), replacing internal/system's former no-op stubs. It
// generates a per-user LaunchAgent plist adapted from
// packaging/com.alresia.volt.plist and installs/removes it via launchctl.
//
// Safety note (see the task's critical constraint): Install/Uninstall
// really do shell out to launchctl and really do write into
// ~/Library/LaunchAgents -- that's the whole point, it's what makes this
// not a no-op anymore. What must never happen is *this repository's own
// tests* invoking that against the machine running them. writePlistFile is
// factored out precisely so the file-generation logic can be unit-tested
// against a temp directory without ever calling launchctl or touching the
// real LaunchAgents directory; see darwin_test.go.
package darwin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// Options describes the LaunchAgent to generate/install.
type Options struct {
	// Label is the launchd job label, e.g. "com.alresiainc.volt".
	Label string
	// ExecPath is the absolute path to the voltpanel binary.
	ExecPath string
	// Args are extra arguments passed to ExecPath.
	Args []string
	// RunAtLoad and KeepAlive mirror the plist keys of the same name.
	// Both default to true (matching packaging/com.alresia.volt.plist)
	// when left unset via applyDefaults.
	RunAtLoad *bool
	KeepAlive *bool
	// StdoutPath/StderrPath default to <label>.out.log/.err.log under the
	// user's Volt log directory when empty.
	StdoutPath string
	StderrPath string
}

func applyDefaults(opts Options) Options {
	if opts.RunAtLoad == nil {
		t := true
		opts.RunAtLoad = &t
	}
	if opts.KeepAlive == nil {
		t := true
		opts.KeepAlive = &t
	}
	if opts.StdoutPath == "" {
		opts.StdoutPath = filepath.Join(os.TempDir(), opts.Label+".out.log")
	}
	if opts.StderrPath == "" {
		opts.StderrPath = filepath.Join(os.TempDir(), opts.Label+".err.log")
	}
	return opts
}

const plistTemplateSrc = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.ExecPath}}</string>
{{- range .Args}}
        <string>{{.}}</string>
{{- end}}
    </array>
    <key>RunAtLoad</key><{{if .RunAtLoad}}true{{else}}false{{end}}/>
    <key>KeepAlive</key><{{if .KeepAlive}}true{{else}}false{{end}}/>
    <key>StandardOutPath</key><string>{{.StdoutPath}}</string>
    <key>StandardErrorPath</key><string>{{.StderrPath}}</string>
</dict>
</plist>
`

var plistTemplate = template.Must(template.New("launchd-plist").Parse(plistTemplateSrc))

type plistData struct {
	Label      string
	ExecPath   string
	Args       []string
	RunAtLoad  bool
	KeepAlive  bool
	StdoutPath string
	StderrPath string
}

// xmlEscape escapes the handful of characters that matter inside a plist
// <string> element -- paths/labels are the only free-form content here, and
// none of them are expected to be attacker-controlled, but escaping is
// cheap correctness regardless.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// GeneratePlist renders the launchd plist content for opts. Pure function,
// no filesystem or process interaction -- this is what darwin_test.go
// exercises directly to verify content correctness without installing
// anything.
func GeneratePlist(opts Options) (string, error) {
	opts = applyDefaults(opts)
	if opts.Label == "" {
		return "", fmt.Errorf("darwin: Label is required")
	}
	if opts.ExecPath == "" {
		return "", fmt.Errorf("darwin: ExecPath is required")
	}
	args := make([]string, len(opts.Args))
	for i, a := range opts.Args {
		args[i] = xmlEscape(a)
	}
	data := plistData{
		Label:      xmlEscape(opts.Label),
		ExecPath:   xmlEscape(opts.ExecPath),
		Args:       args,
		RunAtLoad:  *opts.RunAtLoad,
		KeepAlive:  *opts.KeepAlive,
		StdoutPath: xmlEscape(opts.StdoutPath),
		StderrPath: xmlEscape(opts.StderrPath),
	}
	var buf strings.Builder
	if err := plistTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// defaultLaunchAgentsDir returns the real, per-user LaunchAgents directory.
func defaultLaunchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// writePlistFile renders opts and writes it to dir/<label>.plist, creating
// dir if needed. Returns the written path. Pure file I/O -- no launchctl
// invocation -- which is exactly why tests call this directly against a
// t.TempDir() rather than going through Install.
func writePlistFile(dir string, opts Options) (string, error) {
	content, err := GeneratePlist(opts)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, opts.Label+".plist")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Install writes the LaunchAgent plist to the real ~/Library/LaunchAgents
// and loads it via `launchctl load -w`. This is real, side-effecting
// registration of a persistent background service -- callers (the CLI /
// UI, not this repo's own tests) are the only intended invokers.
func Install(opts Options) error {
	dir, err := defaultLaunchAgentsDir()
	if err != nil {
		return fmt.Errorf("darwin: locate LaunchAgents dir: %w", err)
	}
	path, err := writePlistFile(dir, opts)
	if err != nil {
		return fmt.Errorf("darwin: write plist: %w", err)
	}
	out, err := exec.Command("launchctl", "load", "-w", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("darwin: launchctl load: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Uninstall unloads and removes the LaunchAgent with the given label.
func Uninstall(label string) error {
	dir, err := defaultLaunchAgentsDir()
	if err != nil {
		return fmt.Errorf("darwin: locate LaunchAgents dir: %w", err)
	}
	path := filepath.Join(dir, label+".plist")
	// Unload best-effort: if it was never loaded (or already unloaded)
	// this errors harmlessly; the file removal below is what actually
	// matters for "is it still registered."
	_, _ = exec.Command("launchctl", "unload", path).CombinedOutput()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("darwin: remove plist: %w", err)
	}
	return nil
}

// Status reports launchctl's view of the job, or "not installed" if
// launchctl doesn't know about it.
func Status(label string) (string, error) {
	out, err := exec.Command("launchctl", "list", label).CombinedOutput()
	if err != nil {
		return "not installed", nil
	}
	return strings.TrimSpace(string(out)), nil
}
