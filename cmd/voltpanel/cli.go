package main

// The CLI stays deliberately small (§12 of the plan): a hand-rolled
// dispatch over a handful of subcommands, not a new dependency like cobra
// -- add one only if this list grows past ~10 entries. `volt` with no
// subcommand keeps behaving like today's default (boot the daemon), for
// continuity with existing invocations/scripts/packaging.
//
// Explicitly not added here: `volt project`/`deploy`/`server`/`pipeline`
// and friends -- the browser UI owns management for those (§12's own
// reasoning). A CLI verb gets added only when there's a proven case the
// browser can't cover, and even then as a thin wrapper over the same REST
// API the UI uses, never a second implementation of the logic.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/metrics"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

// Version is set via -ldflags "-X main.Version=..." by goreleaser; "dev"
// for a local `go build`.
var Version = "dev"

// dispatch returns true if args[0] was a recognized subcommand it fully
// handled (main should exit(0) after), or false if execution should fall
// through to the default daemon-start behavior (no subcommand, or an
// unrecognized non-flag first argument is left for `flag.Parse` to reject
// normally further down in main).
func dispatch(args []string) bool {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return false
	}
	switch args[0] {
	case "start":
		return false // falls through to the normal daemon-boot path below
	case "stop":
		cliStop()
	case "restart":
		cliRestart()
	case "status":
		cliStatus()
	case "logs":
		cliLogs(args[1:])
	case "doctor":
		cliDoctor()
	case "update":
		cliUpdate()
	case "version":
		fmt.Println("voltpanel " + Version)
	default:
		fmt.Fprintf(os.Stderr, "voltpanel: unknown subcommand %q\n\nUsage: voltpanel [start|stop|restart|status|logs|doctor|update|version]\n", args[0])
		os.Exit(1)
	}
	return true
}

func readRunningConfig() (storage.Config, string, error) {
	cfgDir, err := storage.EnsureDirs()
	if err != nil {
		return storage.Config{}, "", err
	}
	cfg, err := storage.LoadOrInitConfig()
	return cfg, cfgDir, err
}

func readPID(cfgDir string) (int, error) {
	b, err := os.ReadFile(pidPath(cfgDir))
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(string(b), "%d", &pid); err != nil {
		return 0, err
	}
	return pid, nil
}

func pidPath(cfgDir string) string { return cfgDir + "/daemon.pid" }

func cliStop() {
	_, cfgDir, err := readRunningConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: %v\n", err)
		os.Exit(1)
	}
	pid, err := readPID(cfgDir)
	if err != nil {
		fmt.Println("voltpanel: not running (no daemon.pid found)")
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: %v\n", err)
		os.Exit(1)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Printf("voltpanel: process %d not running (stale pid file, removing)\n", pid)
		_ = os.Remove(pidPath(cfgDir))
		return
	}
	fmt.Printf("voltpanel: sent stop signal to pid %d\n", pid)
}

func cliRestart() {
	_, cfgDir, err := readRunningConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: %v\n", err)
		os.Exit(1)
	}
	if pid, err := readPID(cfgDir); err == nil {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Signal(syscall.SIGTERM)
			// Best-effort wait for the old daemon to release the port/pid file.
			for i := 0; i < 50; i++ {
				if _, err := readPID(cfgDir); err != nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: %v\n", err)
		os.Exit(1)
	}
	proc, err := os.StartProcess(self, []string{self, "start"}, &os.ProcAttr{
		Files: []*os.File{nil, os.Stdout, os.Stderr},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: failed to relaunch: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("voltpanel: restarted (new pid %d)\n", proc.Pid)
}

func cliStatus() {
	cfg, _, err := readRunningConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: %v\n", err)
		os.Exit(1)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", cfg.Port)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("voltpanel: not responding on port %d (%v)\n", cfg.Port, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("voltpanel: unhealthy (HTTP %d) on port %d\n", resp.StatusCode, cfg.Port)
		os.Exit(1)
	}
	fmt.Printf("voltpanel %s: healthy on http://127.0.0.1:%d\n", Version, cfg.Port)
}

func cliLogs(args []string) {
	_, cfgDir, err := readRunningConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: %v\n", err)
		os.Exit(1)
	}
	path := cfgDir + "/logs/daemon.log"
	follow := len(args) > 0 && (args[0] == "-f" || args[0] == "--follow")

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	_, _ = io.Copy(os.Stdout, f)

	if !follow {
		return
	}
	for {
		time.Sleep(500 * time.Millisecond)
		_, _ = io.Copy(os.Stdout, f)
	}
}

func cliDoctor() {
	ok := true
	check := func(name string, err error) {
		if err != nil {
			fmt.Printf("✗ %s: %v\n", name, err)
			ok = false
			return
		}
		fmt.Printf("✓ %s\n", name)
	}

	cfgDir, err := storage.EnsureDirs()
	check("config dir writable", func() error {
		if err != nil {
			return err
		}
		probe := cfgDir + "/.doctor-probe"
		if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
			return err
		}
		return os.Remove(probe)
	}())

	check("config loads", func() error {
		_, err := storage.LoadOrInitConfig()
		return err
	}())

	check("disk space available", func() error {
		m, err := metrics.Collect()
		if err != nil {
			return err
		}
		if m.DiskTotal > 0 && m.DiskUsed >= m.DiskTotal {
			return fmt.Errorf("disk appears full")
		}
		return nil
	}())

	if pid, err := readPID(cfgDir); err == nil {
		if proc, err := os.FindProcess(pid); err == nil && proc.Signal(syscall.Signal(0)) == nil {
			fmt.Printf("i daemon appears to be running (pid %d)\n", pid)
		}
	}

	if !ok {
		os.Exit(1)
	}
}

const updateManifestURL = "https://api.github.com/repos/alresiainc/alresia-voltpanel/releases/latest"

// cliUpdate checks (never silently applies) the latest published release
// against the running binary's version. Actually replacing the binary is
// deliberately not automated here -- self-replacing a running executable
// is exactly the kind of hard-to-reverse action that warrants an explicit,
// separate, user-driven step (e.g. re-running the same install method:
// brew upgrade / package manager / re-download) rather than this command
// doing it silently. This only became meaningful after the rename
// question settled (§16.7); it stays a real network call gated entirely
// behind the user explicitly typing `volt update`, never invoked otherwise.
func cliUpdate() {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(updateManifestURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: failed to check latest release: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "voltpanel: release check returned HTTP %d\n", resp.StatusCode)
		os.Exit(1)
	}
	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fmt.Fprintf(os.Stderr, "voltpanel: %v\n", err)
		os.Exit(1)
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	if latest == Version {
		fmt.Printf("voltpanel: already on the latest version (%s)\n", Version)
		return
	}
	fmt.Printf("voltpanel: a newer version is available: %s -> %s\n", Version, latest)
	fmt.Printf("Install it via the same method you used originally (Homebrew/deb/rpm/manual download): %s\n", release.HTMLURL)
}
