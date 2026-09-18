package providers

import "sync"

// Registry holds registered RuntimeProviders by Kind() ("node", "php",
// ...). API handlers and domain services depend only on the Registry plus
// the RuntimeProvider interface, never on a concrete provider package
// (github.com/alresiainc/alresia-voltpanel/internal/providers/runtime/node
// etc.) -- that's what lets a later, externally-discovered provider
// (Phase 11) register itself identically to a built-in one.
//
// Kept deliberately simple per the plan (§7): a map + mutex is enough for
// Phase 2's two providers; nothing here needs to grow until an actual
// second provider kind (database, webserver, ...) needs registering.
type Registry struct {
	mu       sync.RWMutex
	runtimes map[string]RuntimeProvider
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{runtimes: make(map[string]RuntimeProvider)}
}

// RegisterRuntime adds (or replaces) a RuntimeProvider under its Kind().
func (r *Registry) RegisterRuntime(p RuntimeProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimes[p.Kind()] = p
}

// Runtime looks up a registered RuntimeProvider by kind. ok is false when
// nothing is registered for that kind.
func (r *Registry) Runtime(kind string) (p RuntimeProvider, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok = r.runtimes[kind]
	return p, ok
}

// Runtimes returns every registered RuntimeProvider. Order is
// unspecified; callers needing a stable order should sort by Kind().
func (r *Registry) Runtimes() []RuntimeProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RuntimeProvider, 0, len(r.runtimes))
	for _, p := range r.runtimes {
		out = append(out, p)
	}
	return out
}
