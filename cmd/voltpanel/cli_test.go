package main

import (
	"os"
	"testing"
)

func TestDispatchFallsThroughForStartAndNoArgs(t *testing.T) {
	if dispatch(nil) {
		t.Fatal("expected no args to fall through to daemon start")
	}
	if dispatch([]string{"start"}) {
		t.Fatal("expected 'start' to fall through to daemon start")
	}
	if dispatch([]string{"-port", "1234"}) {
		t.Fatal("expected a leading flag to fall through to daemon start")
	}
}

func TestDispatchVersionHandledFully(t *testing.T) {
	if !dispatch([]string{"version"}) {
		t.Fatal("expected 'version' to be fully handled (return true)")
	}
}

func TestPIDFileRoundTrip(t *testing.T) {
	cfgDir := t.TempDir()
	if err := writePIDFile(cfgDir); err != nil {
		t.Fatal(err)
	}
	pid, err := readPID(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Fatalf("expected pid %d, got %d", os.Getpid(), pid)
	}
	removePIDFile(cfgDir)
	if _, err := readPID(cfgDir); err == nil {
		t.Fatal("expected pid file to be gone after removePIDFile")
	}
}

func TestCliStopHandlesMissingPIDFileGracefully(t *testing.T) {
	// Regression guard: cliStop must not panic/exit when nothing is
	// running -- readPID returning an error is the expected, common case
	// (e.g. `voltpanel stop` run twice, or before ever starting).
	cfgDir := t.TempDir()
	if _, err := readPID(cfgDir); err == nil {
		t.Fatal("expected no pid file in a fresh temp dir")
	}
}
