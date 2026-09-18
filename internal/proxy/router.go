// Package proxy implements the piece Domains & SSL was missing: mapping a
// hostname to 127.0.0.1 via the hosts file only gets DNS resolution right
// (see internal/providers/domainprovider/hosts) -- something still has to
// forward the actual HTTP request that arrives for that hostname to
// whichever local port a project's dev server is really listening on.
// Router is that something: an in-memory hostname -> "127.0.0.1:port"
// table, served over a plain reverse proxy.
//
// It listens on its own fixed, unprivileged port (not 80/443) rather than
// the port a browser defaults to -- binding a privileged port needs root,
// which the daemon deliberately never runs as (see path_unix.go's note on
// the same tradeoff for the hosts file). Visiting a bound domain today
// means including that port, e.g. http://myapp.test:7080.
package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

// Router is safe for concurrent use: HTTP handlers read it on every
// request while project start/stop and domain create/delete mutate it.
type Router struct {
	mu      sync.RWMutex
	targets map[string]string // lowercase hostname -> "127.0.0.1:port"
	port    int
}

func NewRouter() *Router {
	return &Router{targets: map[string]string{}}
}

// Set registers (or replaces) the live target address for hostname.
func (r *Router) Set(hostname, addr string) {
	if hostname == "" || addr == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targets[strings.ToLower(hostname)] = addr
}

// Remove un-registers hostname; a no-op if it wasn't registered.
func (r *Router) Remove(hostname string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.targets, strings.ToLower(hostname))
}

// Lookup returns the live target address for hostname, if any.
func (r *Router) Lookup(hostname string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	addr, ok := r.targets[strings.ToLower(hostname)]
	return addr, ok
}

// SetPort records the port the proxy's own HTTP listener ended up on
// (internal/server.Server.Run probes it, same as the main API port) so
// handlers/UI can report where a bound domain is actually reachable.
func (r *Router) SetPort(port int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.port = port
}

// Port returns the port set via SetPort, or 0 before Run has started it.
func (r *Router) Port() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.port
}

// Routes returns a snapshot of every currently-registered hostname ->
// target mapping, for status/debugging display.
func (r *Router) Routes() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.targets))
	for k, v := range r.targets {
		out[k] = v
	}
	return out
}

// Handler returns the http.Handler that does the actual proxying, keyed
// per-request by the incoming Host header.
func (r *Router) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		host := hostOnly(req.Host)
		addr, ok := r.Lookup(host)
		if !ok {
			http.Error(w, "volt proxy: no running project is bound to "+host, http.StatusNotFound)
			return
		}
		httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: addr}).ServeHTTP(w, req)
	})
}

func hostOnly(h string) string {
	if i := strings.IndexByte(h, ':'); i >= 0 {
		return h[:i]
	}
	return h
}
