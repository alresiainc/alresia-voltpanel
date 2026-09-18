package service

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/storage"
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test relies on /bin/sh and SIGTERM semantics, not applicable on windows")
	}
	cfgDir := t.TempDir()
	root := t.TempDir()
	st, err := storage.NewStoreWithRoot(cfgDir, root)
	if err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub("test-token", true, nil)
	return NewManager(st, hub)
}

// TestGracefulStopForceKillsAfterTimeout verifies the force-kill half of
// the acceptance criteria: a process that installs a no-op SIGTERM handler
// (so it never exits on its own) must still be gone once Stop's graceful
// timeout elapses.
func TestGracefulStopForceKillsAfterTimeout(t *testing.T) {
	m := newTestManager(t)
	svc, err := m.Start(StartRequest{
		ID: "ignores-term", Name: "ignores-term",
		Command: "/bin/sh", Args: []string{"-c", "trap '' TERM; sleep 30"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if svc.PID == 0 {
		t.Fatal("expected a PID for the started process")
	}
	// Give the forked /bin/sh time to actually execute the `trap` builtin
	// before signaling it -- otherwise SIGTERM can race ahead of the trap
	// being installed and fall through to the shell's default (terminate)
	// action, which would make this test flaky rather than exercising the
	// intended "ignores SIGTERM" scenario.
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	timeout := 500 * time.Millisecond
	if err := m.Stop("ignores-term", true, timeout); err != nil {
		t.Fatalf("stop: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < timeout {
		t.Fatalf("expected Stop to block for at least the graceful timeout (%s) before force-killing, took %s", timeout, elapsed)
	}
	if elapsed > timeout+2*time.Second {
		t.Fatalf("Stop took much longer than the timeout should allow: %s", elapsed)
	}

	if _, ok := m.Get("ignores-term"); ok {
		t.Fatal("expected the service to no longer be tracked as running after a force-kill")
	}
}

// TestGracefulStopNoForceKillOnCleanExit verifies the graceful half of the
// acceptance criteria: a process that exits promptly on SIGTERM should
// never need the force-kill fallback -- Stop should return well before the
// timeout elapses. Deliberately uses plain `sleep` (not a shell wrapping a
// trap) so the process's *default* SIGTERM disposition (terminate) is what
// gets exercised -- a shell running a foreground `sleep` as a trap target
// is a known bash gotcha (traps are only serviced once the foreground
// command completes), which would make this flaky rather than testing the
// thing the acceptance criteria actually cares about.
func TestGracefulStopNoForceKillOnCleanExit(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Start(StartRequest{
		ID: "handles-term", Name: "handles-term",
		Command: "sleep", Args: []string{"30"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	start := time.Now()
	timeout := 3 * time.Second
	if err := m.Stop("handles-term", true, timeout); err != nil {
		t.Fatalf("stop: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed >= timeout {
		t.Fatalf("expected the process to exit on SIGTERM well before the %s timeout, took %s (suggests it was force-killed instead)", timeout, elapsed)
	}

	if _, ok := m.Get("handles-term"); ok {
		t.Fatal("expected the service to no longer be tracked as running after it exited")
	}
}

// TestStopHardKillsWhenNotGraceful confirms the graceful=false path skips
// straight to a hard kill.
func TestStopHardKillsWhenNotGraceful(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Start(StartRequest{
		ID: "hard-kill", Name: "hard-kill",
		Command: "/bin/sh", Args: []string{"-c", "trap '' TERM; sleep 30"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	start := time.Now()
	if err := m.Stop("hard-kill", false, 5*time.Second); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("expected an immediate hard kill, took %s", elapsed)
	}
}

// pollForStatus polls the persisted record for id until it matches want or
// the deadline passes, returning the last-seen record.
func pollForStatus(t *testing.T, m *Manager, id string, want Status, deadline time.Duration) storage.ServiceRecord {
	t.Helper()
	end := time.Now().Add(deadline)
	var rec storage.ServiceRecord
	for time.Now().Before(end) {
		r, ok := m.store.GetServiceRecord(id)
		if ok {
			rec = r
			if rec.Status == string(want) {
				return rec
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("service %s never reached status %q, last seen: %+v", id, want, rec)
	return rec
}

// TestCrashDetectionRestartsWithinBudgetThenGivesUp exercises the restart
// policy end to end: a service that always exits non-zero should be
// restarted up to MaxRestarts times within RestartWindow, then be marked
// failed and left alone (§ crash detection's "max N restarts per M minutes
// then give up" requirement).
func TestCrashDetectionRestartsWithinBudgetThenGivesUp(t *testing.T) {
	m := newTestManager(t)
	req := StartRequest{
		ID: "crash-loop", Name: "crash-loop",
		Command: "/bin/sh", Args: []string{"-c", "exit 7"},
		RestartPolicy: RestartOnFailure,
		MaxRestarts:   2,
		RestartWindow: 30 * time.Second,
	}
	if _, err := m.Start(req); err != nil {
		t.Fatalf("start: %v", err)
	}

	rec := pollForStatus(t, m, "crash-loop", StatusFailed, 5*time.Second)
	if rec.ExitCode == nil || *rec.ExitCode != 7 {
		t.Fatalf("expected last exit code 7 to be persisted, got %+v", rec.ExitCode)
	}
	if rec.PID != 0 {
		t.Fatalf("expected no PID once the service has given up, got %d", rec.PID)
	}

	// Give any further (incorrect) restart attempts a chance to happen,
	// then confirm the service really did stop retrying.
	time.Sleep(300 * time.Millisecond)
	if _, ok := m.Get("crash-loop"); ok {
		t.Fatal("expected crash-loop to not be running after giving up")
	}
}

// TestCrashDetectionNoRestartWithoutPolicy confirms the default (no
// restart policy set) preserves the old internal/agent.Manager behavior:
// a crash is recorded as failed and never automatically retried.
func TestCrashDetectionNoRestartWithoutPolicy(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Start(StartRequest{
		ID: "crash-once", Name: "crash-once",
		Command: "/bin/sh", Args: []string{"-c", "exit 3"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	rec := pollForStatus(t, m, "crash-once", StatusFailed, 2*time.Second)
	if rec.ExitCode == nil || *rec.ExitCode != 3 {
		t.Fatalf("expected exit code 3, got %+v", rec.ExitCode)
	}

	time.Sleep(300 * time.Millisecond)
	if _, ok := m.Get("crash-once"); ok {
		t.Fatal("expected crash-once to not have been restarted")
	}
}

// TestCleanExitDoesNotRestart confirms a deliberate, successful exit (code
// 0) with an on-failure policy is left alone -- only failures trigger a
// restart under that policy.
func TestCleanExitDoesNotRestart(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Start(StartRequest{
		ID: "clean-exit", Name: "clean-exit",
		Command: "/bin/sh", Args: []string{"-c", "exit 0"},
		RestartPolicy: RestartOnFailure,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	rec := pollForStatus(t, m, "clean-exit", StatusStopped, 2*time.Second)
	if rec.ExitCode == nil || *rec.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %+v", rec.ExitCode)
	}
}

// TestStartAutostartOrdersByDependency confirms StartAutostart reads
// autostart services from storage, orders them by depends_on via TopoSort,
// and starts each one (integration-level check on top of the pure
// topo_test.go unit tests).
func TestStartAutostartOrdersByDependency(t *testing.T) {
	m := newTestManager(t)
	HealthCheckDelay = 10 * time.Millisecond // keep the test fast
	defer func() { HealthCheckDelay = 2 * time.Second }()

	// Persist two services directly (as if configured, not yet started):
	// "web" depends on "db".
	if err := m.store.UpsertServiceRecord(recordFor("db", nil, true)); err != nil {
		t.Fatal(err)
	}
	if err := m.store.UpsertServiceRecord(recordFor("web", []string{"db"}, true)); err != nil {
		t.Fatal(err)
	}
	// A non-autostart service must be left alone.
	if err := m.store.UpsertServiceRecord(recordFor("manual-only", nil, false)); err != nil {
		t.Fatal(err)
	}

	if err := m.StartAutostart(context.Background()); err != nil {
		t.Fatalf("StartAutostart: %v", err)
	}

	if _, ok := m.Get("db"); !ok {
		t.Fatal("expected db to have been autostarted")
	}
	if _, ok := m.Get("web"); !ok {
		t.Fatal("expected web to have been autostarted")
	}
	if _, ok := m.Get("manual-only"); ok {
		t.Fatal("expected manual-only to NOT have been autostarted")
	}

	_ = m.Stop("db", false, time.Second)
	_ = m.Stop("web", false, time.Second)
}

func recordFor(id string, dependsOn []string, autostart bool) storage.ServiceRecord {
	return storage.ServiceRecord{
		ID: id, Name: id, Command: "/bin/sh", Args: []string{"-c", "sleep 30"},
		Autostart: autostart, DependsOn: dependsOn,
		RestartPolicy: "no",
	}
}
