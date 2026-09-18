// Package docker talks to the local Docker Engine API directly over its
// Unix domain socket, using only the standard library (net.Dial("unix", ...)
// plus http.Client with a custom Transport.DialContext). This is a deliberate
// choice over importing github.com/docker/docker (the full moby/docker SDK):
// that dependency tree is large, notoriously fiddly to vendor/build, and far
// more than listing/starting/stopping containers and streaming logs needs.
//
// Windows: Docker Desktop on Windows exposes the Engine API over a named
// pipe (\\.\pipe\docker_engine) whose path and availability vary a lot more
// across installs than the Unix socket does. Rather than guess at it, this
// package returns ErrUnavailable on Windows today; real named-pipe support
// is a documented TODO for whoever picks up Windows support properly.
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrUnavailable is returned (wrapped with fmt.Errorf("%w: ...", ErrUnavailable))
// whenever the Docker daemon can't be reached: socket missing, permission
// denied, daemon not running, or (today) any attempt on Windows. Callers --
// notably internal/api/v1/docker.go -- check for it with errors.Is/IsUnavailable
// to turn it into a clean, structured "Docker not available" response instead
// of a 500.
var ErrUnavailable = errors.New("docker: daemon not available")

// IsUnavailable reports whether err (or anything it wraps) is ErrUnavailable.
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

// Client is a minimal Docker Engine API client that talks over a Unix
// socket. Zero value is not usable; construct with NewClient.
type Client struct {
	httpClient *http.Client
	socketPath string
}

// candidateSockets lists Unix socket paths, in likelihood order, checked
// when DOCKER_HOST isn't set to a usable unix:// URL. Covers the classic
// root-daemon path plus the common local-dev alternatives on macOS/Linux
// (Docker Desktop's per-user socket, Colima, Rancher Desktop).
func candidateSockets() []string {
	paths := []string{"/var/run/docker.sock"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths,
			filepath.Join(home, ".docker", "run", "docker.sock"),
			filepath.Join(home, ".colima", "default", "docker.sock"),
			filepath.Join(home, ".rd", "docker.sock"),
		)
	}
	return paths
}

// NewClient builds a Client pointed at the local Docker Engine API. It does
// not dial anything itself -- construction is cheap and side-effect free, so
// callers can hold a *Client even when Docker isn't installed or running.
// Call Available (or any other method) to find out whether a daemon is
// actually reachable; every method returns a clean, wrapped ErrUnavailable
// when it isn't, rather than panicking or returning an opaque transport
// error.
//
// The only hard failure here is "unsupported platform" (Windows, today).
func NewClient() (*Client, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("%w: named-pipe transport not implemented on windows (TODO)", ErrUnavailable)
	}

	var preferred string
	if h := os.Getenv("DOCKER_HOST"); strings.HasPrefix(h, "unix://") {
		preferred = strings.TrimPrefix(h, "unix://")
	}

	candidates := candidateSockets()
	if preferred != "" {
		candidates = append([]string{preferred}, candidates...)
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.Mode()&os.ModeSocket != 0 {
			return newClientForSocket(p), nil
		}
	}
	// Nothing found by stat -- still return a client pointed at the first
	// candidate. Every method call will surface a clean ErrUnavailable when
	// actually used; this avoids a check-then-use race and lets a daemon
	// that starts up later just start working without recreating the client.
	path := candidates[0]
	return newClientForSocket(path), nil
}

func newClientForSocket(path string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", path)
		},
	}
	// No blanket http.Client.Timeout: log/exec calls stream and shouldn't be
	// cut off by a fixed deadline. Callers pass a context with whatever
	// deadline is appropriate for that call (short for list/inspect/start/
	// stop, longer or none for logs/exec).
	return &Client{socketPath: path, httpClient: &http.Client{Transport: transport}}
}

// SocketPath returns the Unix socket this client is configured to dial.
// Exposed for diagnostics/logging only.
func (c *Client) SocketPath() string { return c.socketPath }

// do issues an HTTP request against the Docker Engine API. The host in the
// URL is a placeholder ("docker") -- the actual connection always goes over
// the Unix socket via Transport.DialContext, so it's never resolved as DNS.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return resp, nil
}

type engineErrorBody struct {
	Message string `json:"message"`
}

// checkStatus reads and closes resp.Body if resp.StatusCode isn't one of
// okCodes, returning a descriptive error built from the Engine API's JSON
// error body when present. Callers that need the body on success handle
// closing it themselves.
func checkStatus(resp *http.Response, okCodes ...int) error {
	for _, code := range okCodes {
		if resp.StatusCode == code {
			return nil
		}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var eb engineErrorBody
	if json.Unmarshal(b, &eb) == nil && eb.Message != "" {
		return fmt.Errorf("docker: %s (status %d)", eb.Message, resp.StatusCode)
	}
	return fmt.Errorf("docker: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
}

// Available performs a lightweight round-trip (/_ping) to confirm the
// daemon is actually reachable. Handlers call this up front so a single
// check produces the "Docker not available" response, instead of every
// individual list/start/stop call having to guess from a transport error.
func (c *Client) Available(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/_ping", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: ping returned status %d", ErrUnavailable, resp.StatusCode)
	}
	return nil
}
