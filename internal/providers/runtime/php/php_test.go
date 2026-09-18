package php

import (
	"context"
	"errors"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// fakeInstaller substitutes real system calls in unit tests -- per the
// plan, "don't actually install Node/PHP in unit tests -- mock the
// installer call."
type fakeInstaller struct {
	active        string
	activePath    string
	activeOK      bool
	managed       []providers.RuntimeVersion
	managedErr    error
	installCalls  []string
	removeCalls   []string
	defaultCalls  []string
	installErr    error
	removeErr     error
	setDefaultErr error
}

func (f *fakeInstaller) ActiveVersion(ctx context.Context) (string, string, bool) {
	return f.active, f.activePath, f.activeOK
}

func (f *fakeInstaller) ManagedVersions(ctx context.Context) ([]providers.RuntimeVersion, error) {
	return f.managed, f.managedErr
}

func (f *fakeInstaller) Install(ctx context.Context, version string, progress providers.ProgressFunc) error {
	f.installCalls = append(f.installCalls, version)
	if progress != nil {
		progress(0, "start")
	}
	return f.installErr
}

func (f *fakeInstaller) Remove(ctx context.Context, version string) error {
	f.removeCalls = append(f.removeCalls, version)
	return f.removeErr
}

func (f *fakeInstaller) SetDefault(ctx context.Context, version string) error {
	f.defaultCalls = append(f.defaultCalls, version)
	return f.setDefaultErr
}

func TestKind(t *testing.T) {
	p := newWithInstaller(&fakeInstaller{})
	if p.Kind() != "php" {
		t.Fatalf("expected kind 'php', got %q", p.Kind())
	}
}

func TestDetectInstalledMergesActiveAndManagedDeduped(t *testing.T) {
	fi := &fakeInstaller{
		active:     "8.3.1",
		activePath: "/opt/homebrew/opt/php/bin/php",
		activeOK:   true,
		managed: []providers.RuntimeVersion{
			{Version: "8.3.1", InstallPath: "/opt/homebrew/opt/php@8.3/bin/php"},
			{Version: "8.1.27", InstallPath: "/opt/homebrew/opt/php@8.1/bin/php"},
		},
	}
	p := newWithInstaller(fi)
	versions, err := p.DetectInstalled(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 deduped versions, got %d: %+v", len(versions), versions)
	}
	var found83, found81 bool
	for _, v := range versions {
		if v.Version == "8.3.1" {
			found83 = true
			if !v.IsDefault {
				t.Fatal("active version 8.3.1 should be marked default")
			}
		}
		if v.Version == "8.1.27" {
			found81 = true
			if v.IsDefault {
				t.Fatal("non-active version 8.1.27 should not be marked default")
			}
		}
	}
	if !found83 || !found81 {
		t.Fatalf("expected both versions present, got %+v", versions)
	}
}

func TestDetectInstalledPropagatesManagedVersionsError(t *testing.T) {
	wantErr := errors.New("boom")
	p := newWithInstaller(&fakeInstaller{managedErr: wantErr})
	if _, err := p.DetectInstalled(context.Background()); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestInstallDelegatesToInstaller(t *testing.T) {
	fi := &fakeInstaller{}
	p := newWithInstaller(fi)
	if err := p.Install(context.Background(), "8.3.1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fi.installCalls) != 1 || fi.installCalls[0] != "8.3.1" {
		t.Fatalf("expected installer.Install called with 8.3.1, got %+v", fi.installCalls)
	}
}

func TestInstallRejectsEmptyVersion(t *testing.T) {
	p := newWithInstaller(&fakeInstaller{})
	if err := p.Install(context.Background(), "", nil); err == nil {
		t.Fatal("expected error for empty version")
	}
}

func TestRemoveDelegatesToInstaller(t *testing.T) {
	fi := &fakeInstaller{}
	p := newWithInstaller(fi)
	if err := p.Remove(context.Background(), "8.1.27"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fi.removeCalls) != 1 || fi.removeCalls[0] != "8.1.27" {
		t.Fatalf("expected installer.Remove called with 8.1.27, got %+v", fi.removeCalls)
	}
}

func TestSetDefaultDelegatesToInstaller(t *testing.T) {
	fi := &fakeInstaller{}
	p := newWithInstaller(fi)
	if err := p.SetDefault(context.Background(), "8.3.1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fi.defaultCalls) != 1 || fi.defaultCalls[0] != "8.3.1" {
		t.Fatalf("expected installer.SetDefault called with 8.3.1, got %+v", fi.defaultCalls)
	}
}

func TestSetDefaultPropagatesError(t *testing.T) {
	wantErr := errors.New("no such version")
	p := newWithInstaller(&fakeInstaller{setDefaultErr: wantErr})
	if err := p.SetDefault(context.Background(), "99.0.0"); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestMajorMinor(t *testing.T) {
	cases := map[string]string{
		"8.3.1":  "8.3",
		"8.1.27": "8.1",
		"8":      "8",
	}
	for in, want := range cases {
		if got := majorMinor(in); got != want {
			t.Errorf("majorMinor(%q) = %q, want %q", in, got, want)
		}
	}
}

// New (the real, non-mocked constructor) must still produce a usable
// Provider wired to the real sysInstaller -- this doesn't execute any
// install/remove, just checks the wiring compiles and Kind() works.
func TestNewReal(t *testing.T) {
	p := New()
	if p.Kind() != "php" {
		t.Fatalf("expected kind 'php', got %q", p.Kind())
	}
}
