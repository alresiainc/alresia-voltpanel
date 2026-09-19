// Package brew drives Homebrew as VoltPanel's package-management backend
// (§ "install fresh PHP/MySQL/PostgreSQL/Redis, manage multiple versions,
// upgrade Apache/Nginx" -- the cPanel-style ask this exists to answer).
// Homebrew is the pragmatic choice on macOS specifically because it never
// needs root: every operation here runs as the same unprivileged user the
// daemon already runs as, matching VoltPanel's existing "never elevate"
// posture (see internal/providers/domainprovider/hosts's own note on the
// same tradeoff).
//
// Install/upgrade/uninstall can take minutes (Homebrew building or
// downloading a bottle), so those three run as an async internal/domain/job
// with output streamed line-by-line over the same WS "log" event every
// service/deployment log already uses, keyed by the job's ID. Search,
// listing installed formulae, and service start/stop/restart are fast
// enough to stay synchronous.
//
// Every formula name this package is handed -- from a curated tile, a
// search result, or free-typed "install anything" text -- is validated
// against nameRe before it ever reaches exec.Command. exec.Command never
// invokes a shell (no injection vector there), but a stray formula name
// could still reference an arbitrary tap; nameRe keeps it to Homebrew's
// own name grammar (letters/digits/./_/+/@/- and a single-level tap
// "owner/repo/formula" path) rather than accepting anything at all.
package brew

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/job"
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+@-]*(/[A-Za-z0-9][A-Za-z0-9_.+@-]*){0,2}$`)

// ValidateName reports whether name is safe to pass to `brew` as a formula
// argument -- exported so API handlers can reject a bad name with a 400
// before ever calling into this package.
func ValidateName(name string) error {
	if name == "" || len(name) > 128 || !nameRe.MatchString(name) {
		return fmt.Errorf("brew: %q doesn't look like a valid formula name", name)
	}
	return nil
}

// Package is one `brew search` result.
type Package struct {
	Name string `json:"name"`
}

// InstalledPackage is one row of `brew list --versions`, enriched with
// whatever `brew services list` knows about it.
type InstalledPackage struct {
	Name          string   `json:"name"`
	Versions      []string `json:"versions"`
	ServiceStatus string   `json:"serviceStatus,omitempty"` // "" if it has no service; started|stopped|error otherwise
}

// Provider drives the real `brew` binary on PATH.
type Provider struct {
	Hub      *ws.Hub
	Jobs     *job.Repository
	LogDir   string // e.g. <cfgDir>/logs/jobs
	lookPath func(string) (string, error)

	mu      sync.Mutex
	active  map[string]*exec.Cmd // jobID -> running process, for Cancel
	targets map[string]string    // "<kind>\x00<target>" -> jobID, so a second Install of the same formula while one's already running is rejected instead of silently piling up
}

func New(hub *ws.Hub, jobs *job.Repository, logDir string) *Provider {
	return &Provider{
		Hub: hub, Jobs: jobs, LogDir: logDir, lookPath: exec.LookPath,
		active: map[string]*exec.Cmd{}, targets: map[string]string{},
	}
}

// Available reports whether a `brew` binary is on PATH at all -- checked
// per-request by handlers (matching internal/providers/docker's pattern)
// rather than once at startup, since nothing stops Homebrew from being
// installed or uninstalled while the daemon is running.
func (p *Provider) Available() bool {
	lookPath := p.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	_, err := lookPath("brew")
	return err == nil
}

// Search runs `brew search --formula <query>`, returning matching formula
// names. query is validated the same as an install target -- search terms
// that don't look like a formula name are rejected rather than silently
// passed through, since Homebrew's own search grammar doesn't need
// anything nameRe wouldn't already allow for a real formula/tap lookup.
func (p *Provider) Search(ctx context.Context, query string) ([]Package, error) {
	if err := ValidateName(query); err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "brew", "search", "--formula", query).Output()
	if err != nil {
		return nil, fmt.Errorf("brew search: %w", err)
	}
	var pkgs []Package
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "==>") {
			continue
		}
		pkgs = append(pkgs, Package{Name: line})
	}
	return pkgs, nil
}

// ListInstalled merges `brew list --formula --versions` with
// `brew services list` so the UI can show, in one call, what's installed
// and (for the formulae that run as a background service) whether it's
// currently running. The two run concurrently -- each is a real `brew`
// process with Ruby's own startup cost (multiple seconds, not
// milliseconds), and running them one after another was double-paying
// that cost for no reason.
func (p *Provider) ListInstalled(ctx context.Context) ([]InstalledPackage, error) {
	type listResult struct {
		out []byte
		err error
	}
	listCh := make(chan listResult, 1)
	go func() {
		out, err := exec.CommandContext(ctx, "brew", "list", "--formula", "--versions").Output()
		listCh <- listResult{out, err}
	}()

	svcs, svcErr := p.serviceStatuses(ctx)
	lr := <-listCh
	if lr.err != nil {
		return nil, fmt.Errorf("brew list: %w", lr.err)
	}

	byName := map[string]*InstalledPackage{}
	var order []string
	for _, line := range strings.Split(string(lr.out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		ip := &InstalledPackage{Name: fields[0], Versions: append([]string{}, fields[1:]...)}
		byName[ip.Name] = ip
		order = append(order, ip.Name)
	}

	if svcErr == nil {
		for name, status := range svcs {
			if ip, ok := byName[name]; ok {
				ip.ServiceStatus = status
			}
		}
	}

	sort.Strings(order)
	out2 := make([]InstalledPackage, 0, len(order))
	for _, n := range order {
		out2 = append(out2, *byName[n])
	}
	return out2, nil
}

type brewServiceEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (p *Provider) serviceStatuses(ctx context.Context) (map[string]string, error) {
	out, err := exec.CommandContext(ctx, "brew", "services", "list", "--json").Output()
	if err != nil {
		return nil, err
	}
	var entries []brewServiceEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, err
	}
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		m[e.Name] = e.Status
	}
	return m, nil
}

// ServiceAction runs `brew services <action> <name>` -- fast (just a
// launchd load/unload), so unlike Install/Upgrade/Uninstall this stays
// synchronous rather than becoming a job.
func (p *Provider) ServiceAction(ctx context.Context, name, action string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	switch action {
	case "start", "stop", "restart":
	default:
		return fmt.Errorf("brew: unknown service action %q", action)
	}
	out, err := exec.CommandContext(ctx, "brew", "services", action, name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("brew services %s %s: %w: %s", action, name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Install starts `brew install <name>` as a background job.
func (p *Provider) Install(name string) (job.Job, error) {
	return p.runJob("install", name, "install", name)
}

// Upgrade starts `brew upgrade <name>` as a background job.
func (p *Provider) Upgrade(name string) (job.Job, error) {
	return p.runJob("upgrade", name, "upgrade", name)
}

// Uninstall starts `brew uninstall <name>` as a background job.
func (p *Provider) Uninstall(name string) (job.Job, error) {
	return p.runJob("uninstall", name, "uninstall", name)
}

// SetDefaultVersion switches which installed version of a versioned
// formula family (e.g. target "php@8.3" belongs to family "php") is
// linked into PATH -- Homebrew's own mechanism for side-by-side versions,
// the same one phpbrew/nvm-equivalents use under the hood. It looks up
// every other installed formula in the same family and unlinks them
// first, avoiding brew's "already linked" conflict when switching.
// Synchronous: linking is just symlink bookkeeping, not a download/build.
func (p *Provider) SetDefaultVersion(ctx context.Context, target string) error {
	if err := ValidateName(target); err != nil {
		return err
	}
	family := target
	if i := strings.IndexByte(target, '@'); i >= 0 {
		family = target[:i]
	}

	installed, err := p.ListInstalled(ctx)
	if err == nil {
		for _, ip := range installed {
			if ip.Name == target {
				continue
			}
			if ip.Name == family || strings.HasPrefix(ip.Name, family+"@") {
				_ = exec.CommandContext(ctx, "brew", "unlink", ip.Name).Run()
			}
		}
	}

	out, cmdErr := exec.CommandContext(ctx, "brew", "link", "--overwrite", "--force", target).CombinedOutput()
	if cmdErr != nil {
		return fmt.Errorf("brew link %s: %w: %s", target, cmdErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// runJob execs `brew <args...>`, creating a job.Job immediately and
// running the command in a goroutine -- callers get the Job back (with an
// ID to subscribe to over WS) before the command has necessarily even
// started, let alone finished. Refuses to start a second install/upgrade/
// uninstall for the same (kind, target) while one is already running --
// on a machine where a formula has no prebuilt bottle for this OS version,
// `brew install` can mean compiling from source for a very long time, and
// two of those piling up for the same target was exactly the confusing
// "nothing is happening" state this guards against.
func (p *Provider) runJob(kind, target string, args ...string) (job.Job, error) {
	if err := ValidateName(target); err != nil {
		return job.Job{}, err
	}

	key := kind + "\x00" + target
	p.mu.Lock()
	if existing, ok := p.targets[key]; ok {
		p.mu.Unlock()
		return job.Job{}, fmt.Errorf("brew: %s %s is already running (job %s) -- wait for it to finish or cancel it first", kind, target, existing)
	}
	p.mu.Unlock()

	if err := os.MkdirAll(p.LogDir, 0o755); err != nil {
		return job.Job{}, fmt.Errorf("brew: create log dir: %w", err)
	}

	j, err := p.Jobs.Create(kind, target, "")
	if err != nil {
		return job.Job{}, err
	}
	logFile := filepath.Join(p.LogDir, j.ID+".log")
	j.LogFile = logFile
	_ = p.Jobs.SetLogFile(j.ID, logFile)

	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return job.Job{}, fmt.Errorf("brew: open log file: %w", err)
	}

	cmd := exec.Command("brew", args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		_ = lf.Close()
		p.finish(j.ID, job.StatusFailed, -1, err.Error())
		return job.Job{}, err
	}

	p.mu.Lock()
	p.active[j.ID] = cmd
	p.targets[key] = j.ID
	p.mu.Unlock()

	done := make(chan struct{}, 2)
	go func() { p.pipeLogs(j.ID, stdout, lf); done <- struct{}{} }()
	go func() { p.pipeLogs(j.ID, stderr, lf); done <- struct{}{} }()

	go func() {
		<-done
		<-done
		waitErr := cmd.Wait()
		_ = lf.Close()

		p.mu.Lock()
		delete(p.active, j.ID)
		delete(p.targets, key)
		p.mu.Unlock()

		if waitErr == nil {
			p.finish(j.ID, job.StatusSuccess, 0, "")
			return
		}
		exitCode := -1
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		status := job.StatusFailed
		if cmd.ProcessState != nil && cmd.ProcessState.Sys() != nil {
			// A killed-via-Cancel process still surfaces here as a wait
			// error; Cancel already knows it canceled and doesn't need
			// this path, but a signal-killed process (rather than a clean
			// non-zero exit) is the one case worth its own status instead
			// of just "failed".
			if ws, ok := cmd.ProcessState.Sys().(interface{ Signaled() bool }); ok && ws.Signaled() {
				status = job.StatusCanceled
			}
		}
		p.finish(j.ID, status, exitCode, waitErr.Error())
	}()

	return j, nil
}

// finish calls Jobs.Finish and, unlike the code this replaced, actually
// does something if that write fails instead of discarding the error:
// logs it and retries once after a short delay. A job that never
// transitions out of "running" because of a transient SQLite contention
// error (very plausible here -- concurrent installs each have their own
// log-writing goroutines hammering this same database) is indistinguishable
// from a genuinely stuck job in the UI, which is exactly the confusing
// state being fixed.
func (p *Provider) finish(jobID string, status job.Status, exitCode int, errMsg string) {
	if err := p.Jobs.Finish(jobID, status, exitCode, errMsg); err != nil {
		log.Printf("volt: brew: failed to record job %s finishing (status=%s): %v -- retrying once", jobID, status, err)
		time.Sleep(500 * time.Millisecond)
		if err := p.Jobs.Finish(jobID, status, exitCode, errMsg); err != nil {
			log.Printf("volt: brew: job %s will stay stuck at \"running\" -- retry also failed: %v", jobID, err)
		}
	}
}

// Cancel kills the process backing a still-running job, if any. The
// existing completion goroutine in runJob picks up the resulting wait
// error and records the job as canceled -- Cancel doesn't itself touch
// job state, only the process.
func (p *Provider) Cancel(jobID string) error {
	p.mu.Lock()
	cmd, ok := p.active[jobID]
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("brew: job %s is not currently running", jobID)
	}
	if cmd.Process == nil {
		return fmt.Errorf("brew: job %s has no process to cancel", jobID)
	}
	return cmd.Process.Kill()
}

func (p *Provider) pipeLogs(id string, r io.Reader, lf *os.File) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text() + "\n"
		_, _ = lf.WriteString(line)
		if p.Hub == nil {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"type": "log", "id": id, "data": line, "ts": time.Now().UnixMilli()})
		p.Hub.Emit(payload)
	}
}
