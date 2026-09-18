package providers

import (
	"context"
	"testing"
)

type fakeRuntime struct{ kind string }

func (f fakeRuntime) Kind() string { return f.kind }
func (f fakeRuntime) DetectInstalled(ctx context.Context) ([]RuntimeVersion, error) {
	return nil, nil
}
func (f fakeRuntime) Install(ctx context.Context, version string, progress ProgressFunc) error {
	return nil
}
func (f fakeRuntime) Remove(ctx context.Context, version string) error     { return nil }
func (f fakeRuntime) SetDefault(ctx context.Context, version string) error { return nil }

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Runtime("node"); ok {
		t.Fatal("expected no provider registered yet")
	}
	r.RegisterRuntime(fakeRuntime{kind: "node"})
	p, ok := r.Runtime("node")
	if !ok || p.Kind() != "node" {
		t.Fatalf("expected to find the registered node provider, got ok=%v p=%+v", ok, p)
	}
	if len(r.Runtimes()) != 1 {
		t.Fatalf("expected exactly 1 registered runtime, got %d", len(r.Runtimes()))
	}
}

// TestRegistryTreatsExternalProviderIdenticallyToBuiltIn is the crux of
// Phase 11's design: the Registry only ever sees the RuntimeProvider
// interface, so a Registry-level test can't tell (and shouldn't be able
// to tell) a real out-of-process extension apart from any other
// implementation of that interface -- see
// internal/pluginhost/host_test.go's dogfood test for the real end-to-end
// version of this same claim, using an actual subprocess.
func TestRegistryTreatsExternalProviderIdenticallyToBuiltIn(t *testing.T) {
	r := NewRegistry()
	r.RegisterRuntime(fakeRuntime{kind: "node"})
	r.RegisterRuntime(fakeRuntime{kind: "python-demo"}) // stands in for an external provider here
	if len(r.Runtimes()) != 2 {
		t.Fatalf("expected 2 registered runtimes, got %d", len(r.Runtimes()))
	}
	if _, ok := r.Runtime("python-demo"); !ok {
		t.Fatal("expected the externally-sourced provider's kind to be registered exactly like a built-in one")
	}
}

func TestRegistryUnregisterRuntime(t *testing.T) {
	r := NewRegistry()
	r.RegisterRuntime(fakeRuntime{kind: "php"})
	if _, ok := r.Runtime("php"); !ok {
		t.Fatal("expected php to be registered")
	}
	r.UnregisterRuntime("php")
	if _, ok := r.Runtime("php"); ok {
		t.Fatal("expected php to be gone after UnregisterRuntime")
	}
	// Unregistering something never registered must be a no-op, not a panic.
	r.UnregisterRuntime("does-not-exist")
}
