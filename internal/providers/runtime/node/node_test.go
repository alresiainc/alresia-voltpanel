package node

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
	progress      []string
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
		f.progress = append(f.progress, "start")
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
	if p.Kind() != "node" {
		t.Fatalf("expected kind 'node', got %q", p.Kind())
	}
}

func TestDetectInstalledMergesActiveAndManagedDeduped(t *testing.T) {
	fi := &fakeInstaller{
		active:     "20.11.0",
		activePath: "/usr/local/bin/node",
		activeOK:   true,
		managed: []providers.RuntimeVersion{
			{Version: "20.11.0", InstallPath: "/home/u/.nvm/versions/node/v20.11.0/bin/node"},
			{Version: "18.19.0", InstallPath: "/home/u/.nvm/versions/node/v18.19.0/bin/node"},
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
	var found20, found18 bool
	for _, v := range versions {
		if v.Version == "20.11.0" {
			found20 = true
			if !v.IsDefault {
				t.Fatal("active version 20.11.0 should be marked default")
			}
			if v.InstallPath != "/usr/local/bin/node" {
				t.Fatalf("expected active install path preserved, got %q", v.InstallPath)
			}
		}
		if v.Version == "18.19.0" {
			found18 = true
			if v.IsDefault {
				t.Fatal("non-active version 18.19.0 should not be marked default")
			}
		}
	}
	if !found20 || !found18 {
		t.Fatalf("expected both versions present, got %+v", versions)
	}
}

func TestDetectInstalledNoActiveNoManaged(t *testing.T) {
	p := newWithInstaller(&fakeInstaller{})
	versions, err := p.DetectInstalled(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("expected no versions, got %+v", versions)
	}
}

func TestDetectInstalledPropagatesManagedVersionsError(t *testing.T) {
	wantErr := errors.New("boom")
	p := newWithInstaller(&fakeInstaller{managedErr: wantErr})
	if _, err := p.DetectInstalled(context.Background()); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestInstallDelegatesToInstallerAndReportsProgress(t *testing.T) {
	fi := &fakeInstaller{}
	p := newWithInstaller(fi)
	var got []string
	err := p.Install(context.Background(), "21.0.0", func(percent int, message string) {
		got = append(got, message)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fi.installCalls) != 1 || fi.installCalls[0] != "21.0.0" {
		t.Fatalf("expected installer.Install called with 21.0.0, got %+v", fi.installCalls)
	}
	if len(got) == 0 {
		t.Fatal("expected progress callback to be invoked")
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
	if err := p.Remove(context.Background(), "18.19.0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fi.removeCalls) != 1 || fi.removeCalls[0] != "18.19.0" {
		t.Fatalf("expected installer.Remove called with 18.19.0, got %+v", fi.removeCalls)
	}
}

func TestSetDefaultDelegatesToInstaller(t *testing.T) {
	fi := &fakeInstaller{}
	p := newWithInstaller(fi)
	if err := p.SetDefault(context.Background(), "20.11.0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fi.defaultCalls) != 1 || fi.defaultCalls[0] != "20.11.0" {
		t.Fatalf("expected installer.SetDefault called with 20.11.0, got %+v", fi.defaultCalls)
	}
}

func TestSetDefaultPropagatesError(t *testing.T) {
	wantErr := errors.New("no such version")
	p := newWithInstaller(&fakeInstaller{setDefaultErr: wantErr})
	if err := p.SetDefault(context.Background(), "99.0.0"); err == nil {
		t.Fatal("expected error to propagate")
	}
}

// New (the real, non-mocked constructor) must still produce a usable
// Provider wired to the real nativeInstaller -- this doesn't execute any
// install/remove, just checks the wiring compiles and Kind() works.
func TestNewReal(t *testing.T) {
	p := New(t.TempDir())
	if p.Kind() != "node" {
		t.Fatalf("expected kind 'node', got %q", p.Kind())
	}
}
