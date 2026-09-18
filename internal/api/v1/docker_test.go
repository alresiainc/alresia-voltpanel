package v1

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/providers/docker"
	"github.com/gin-gonic/gin"
)

// fakeDockerSocket starts an HTTP server listening on a short-lived Unix
// socket (mirroring the real Docker Engine API's transport) and returns the
// socket path. Setting DOCKER_HOST=unix://<path> before calling
// docker.NewClient() points the real client at it, letting these tests
// exercise the full router -> handler -> docker.Client path without a real
// Docker daemon.
func fakeDockerSocket(t *testing.T, handler http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "voltdockerapi")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	sockPath := filepath.Join(dir, "d.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(l)
	t.Cleanup(func() {
		srv.Close()
		os.RemoveAll(dir)
	})
	return sockPath
}

// newTestRouterWithDocker builds on newTestRouter (router_test.go) but
// swaps in the given Docker client, so tests can point Deps.Docker at a
// fake socket instead of the real (usually absent) one.
func newTestRouterWithDocker(t *testing.T, token string, dockerClient *docker.Client) (*gin.Engine, Deps) {
	t.Helper()
	_, d := newTestRouter(t, token)
	d.Docker = dockerClient
	g := gin.New()
	Mount(g, d)
	return g, d
}

func TestDockerContainersUnavailableWhenNoDaemon(t *testing.T) {
	// newTestRouter never sets Deps.Docker, matching the actual state on a
	// machine without Docker installed/running -- the plan flags this as
	// the path that matters most to get right.
	g, _ := newTestRouter(t, "secret-token")
	for _, path := range []string{
		"/api/v1/docker/containers",
		"/api/v1/docker/images",
		"/api/v1/docker/volumes",
		"/api/v1/docker/networks",
		"/api/v1/docker/containers/abc",
		"/api/v1/docker/containers/abc/logs",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Volt-Token", "secret-token")
		g.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: expected 503 when docker unavailable, got %d: %s", path, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: expected JSON error body, got %q: %v", path, w.Body.String(), err)
		}
		if avail, ok := body["available"].(bool); !ok || avail {
			t.Fatalf("%s: expected available=false in body, got %v", path, body)
		}
		if _, ok := body["error"].(string); !ok {
			t.Fatalf("%s: expected an error message in body, got %v", path, body)
		}
	}
}

func TestDockerContainerActionsUnavailableWhenNoDaemon(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	for _, path := range []string{
		"/api/v1/docker/containers/abc/start",
		"/api/v1/docker/containers/abc/stop",
		"/api/v1/docker/containers/abc/restart",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("X-Volt-Token", "secret-token")
		g.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: expected 503 when docker unavailable, got %d: %s", path, w.Code, w.Body.String())
		}
	}
}

func TestDockerExecUnavailableWhenNoDaemon(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/containers/abc/exec", strings.NewReader(`{"cmd":["echo","hi"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when docker unavailable, got %d: %s", w.Code, w.Body.String())
	}
}

// writeFrame writes one Docker multiplexed-stream frame, matching what the
// real Engine API sends for logs/exec output.
func writeFrame(w http.ResponseWriter, streamType byte, payload string) {
	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	w.Write(header)
	w.Write([]byte(payload))
}

func TestDockerContainersListedAndGroupedWhenDaemonAvailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) })
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"Id": "c1", "Names": []string{"/myapp-web-1"}, "Image": "nginx:latest",
				"State": "running", "Status": "Up",
				"Labels": map[string]string{"com.docker.compose.project": "myapp"},
			},
			{
				"Id": "c2", "Names": []string{"/standalone"}, "Image": "redis:7",
				"State": "running", "Status": "Up", "Labels": map[string]string{},
			},
		})
	})
	sockPath := fakeDockerSocket(t, mux)
	t.Setenv("DOCKER_HOST", "unix://"+sockPath)
	cl, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker.NewClient: %v", err)
	}

	g, _ := newTestRouterWithDocker(t, "secret-token", cl)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/docker/containers", nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var containers []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &containers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/docker/containers?group=compose", nil)
	req2.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var groups []map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &groups); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 compose groups (myapp + standalone), got %d: %v", len(groups), groups)
	}
}

func TestDockerContainerStartWritesAuditEvent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) })
	mux.HandleFunc("/containers/abc/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	sockPath := fakeDockerSocket(t, mux)
	t.Setenv("DOCKER_HOST", "unix://"+sockPath)
	cl, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker.NewClient: %v", err)
	}

	g, d := newTestRouterWithDocker(t, "secret-token", cl)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/containers/abc/start", nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM audit_events WHERE action = 'docker.container.start'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit event for docker.container.start, got %d", count)
	}
}

func TestDockerContainerLogsDemuxed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) })
	mux.HandleFunc("/containers/abc/logs", func(w http.ResponseWriter, r *http.Request) {
		writeFrame(w, 1, "hello from container\n")
	})
	sockPath := fakeDockerSocket(t, mux)
	t.Setenv("DOCKER_HOST", "unix://"+sockPath)
	cl, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker.NewClient: %v", err)
	}

	g, _ := newTestRouterWithDocker(t, "secret-token", cl)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/docker/containers/abc/logs", nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "hello from container") {
		t.Fatalf("expected demuxed log content, got %q", w.Body.String())
	}
}

func TestDockerContainerNotFoundIsBadRequestNotUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) })
	mux.HandleFunc("/containers/missing/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "No such container: missing"})
	})
	sockPath := fakeDockerSocket(t, mux)
	t.Setenv("DOCKER_HOST", "unix://"+sockPath)
	cl, err := docker.NewClient()
	if err != nil {
		t.Fatalf("docker.NewClient: %v", err)
	}

	g, _ := newTestRouterWithDocker(t, "secret-token", cl)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/docker/containers/missing", nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	// The daemon IS reachable here -- a missing container is a normal
	// client error (400), never the "Docker not available" 503.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing container on a reachable daemon, got %d: %s", w.Code, w.Body.String())
	}
}
