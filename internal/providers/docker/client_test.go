package docker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testClient spins up a real HTTP server listening on a Unix socket in a
// temp dir (mirroring how the Docker daemon itself listens) and returns a
// Client pointed at it. This is what lets us test the transport
// (net.Dial("unix", ...) + http.Client) without a real Docker daemon.
func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	// Deliberately not t.TempDir(): that nests under a directory named after
	// the full (sub)test name, which combined with "/docker.sock" easily
	// exceeds the ~104-byte sun_path limit on Unix domain sockets (macOS in
	// particular). A short, uniquely-named dir directly under os.TempDir()
	// keeps the path short regardless of test name length.
	dir, err := os.MkdirTemp("", "voltdocker")
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
	return newClientForSocket(sockPath)
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestAvailableSucceedsAgainstRealSocket(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})
	c := testClient(t, mux)
	if err := c.Available(ctx(t)); err != nil {
		t.Fatalf("expected Available to succeed, got %v", err)
	}
}

func TestAvailableFailsCleanlyWithoutSocket(t *testing.T) {
	dir := t.TempDir()
	c := newClientForSocket(filepath.Join(dir, "nonexistent.sock"))
	err := c.Available(ctx(t))
	if err == nil {
		t.Fatal("expected an error when no daemon is listening")
	}
	if !IsUnavailable(err) {
		t.Fatalf("expected IsUnavailable(err) to be true, got err=%v", err)
	}
}

func TestNewClientRejectsWindows(t *testing.T) {
	// This test only meaningfully exercises the windows branch when run on
	// windows; on other platforms NewClient should succeed (pointed at
	// whatever socket path it guesses, even if nothing is listening there).
	c, err := NewClient()
	if runtime.GOOS == "windows" {
		if err == nil || !IsUnavailable(err) {
			t.Fatalf("expected ErrUnavailable on windows, got client=%v err=%v", c, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("expected NewClient to succeed on this platform, got %v", err)
	}
	if c == nil {
		t.Fatal("expected a non-nil client")
	}
}

func TestListContainersGroupsByComposeProject(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("all"); got != "true" {
			t.Errorf("expected all=true, got %q", got)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"Id": "c1", "Names": []string{"/myapp-web-1"}, "Image": "nginx:latest",
				"Command": "nginx -g daemon off;", "State": "running", "Status": "Up 2 hours",
				"Labels": map[string]string{"com.docker.compose.project": "myapp"},
				"Ports":  []map[string]any{{"IP": "0.0.0.0", "PrivatePort": 80, "PublicPort": 8080, "Type": "tcp"}},
			},
			{
				"Id": "c2", "Names": []string{"/myapp-db-1"}, "Image": "postgres:16",
				"Command": "postgres", "State": "running", "Status": "Up 2 hours",
				"Labels": map[string]string{"com.docker.compose.project": "myapp"},
			},
			{
				"Id": "c3", "Names": []string{"/standalone-redis"}, "Image": "redis:7",
				"Command": "redis-server", "State": "exited", "Status": "Exited (0) 1 hour ago",
				"Labels": map[string]string{},
			},
		})
	})
	c := testClient(t, mux)

	containers, err := c.ListContainers(ctx(t))
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(containers) != 3 {
		t.Fatalf("expected 3 containers, got %d", len(containers))
	}
	if containers[0].Names[0] != "myapp-web-1" {
		t.Errorf("expected leading slash stripped from container name, got %q", containers[0].Names[0])
	}
	if containers[0].Project != "myapp" {
		t.Errorf("expected project 'myapp', got %q", containers[0].Project)
	}
	if len(containers[0].Ports) != 1 || containers[0].Ports[0].PublicPort != 8080 {
		t.Errorf("expected port mapping to survive, got %+v", containers[0].Ports)
	}

	groups := GroupByComposeProject(containers)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups (myapp + standalone), got %d: %+v", len(groups), groups)
	}
	if groups[0].Name != "myapp" || len(groups[0].Containers) != 2 {
		t.Fatalf("expected myapp group with 2 containers, got %+v", groups[0])
	}
	if groups[1].Name != "" || len(groups[1].Containers) != 1 {
		t.Fatalf("expected standalone group with 1 container, got %+v", groups[1])
	}
}

func TestInspectContainerNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "No such container: bogus"})
	})
	c := testClient(t, mux)
	_, err := c.InspectContainer(ctx(t), "bogus")
	if err == nil {
		t.Fatal("expected an error for a missing container")
	}
	if IsUnavailable(err) {
		t.Fatal("a 404 from a reachable daemon should not be reported as ErrUnavailable")
	}
}

func TestInspectContainerDecodesDetail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/abc123/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"Id":           "abc123",
			"Name":         "/myapp-web-1",
			"RestartCount": 2,
			"State":        map[string]any{"Status": "running"},
			"Config": map[string]any{
				"Image":  "nginx:latest",
				"Cmd":    []string{"nginx", "-g", "daemon off;"},
				"Env":    []string{"FOO=bar"},
				"Labels": map[string]string{"com.docker.compose.project": "myapp"},
			},
			"Mounts": []map[string]any{
				{"Type": "bind", "Source": "/host/path", "Destination": "/container/path", "RW": true},
			},
		})
	})
	c := testClient(t, mux)
	detail, err := c.InspectContainer(ctx(t), "abc123")
	if err != nil {
		t.Fatalf("InspectContainer: %v", err)
	}
	if detail.Names[0] != "myapp-web-1" || detail.Project != "myapp" || detail.RestartCount != 2 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	if len(detail.Env) != 1 || detail.Env[0] != "FOO=bar" {
		t.Fatalf("unexpected env: %+v", detail.Env)
	}
	if len(detail.Mounts) != 1 || detail.Mounts[0].Destination != "/container/path" {
		t.Fatalf("unexpected mounts: %+v", detail.Mounts)
	}
}

func TestStartStopRestartContainer(t *testing.T) {
	var calls []string
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/abc/start", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "start")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/containers/abc/stop", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "stop")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/containers/abc/restart", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "restart")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/containers/missing/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c := testClient(t, mux)

	if err := c.StartContainer(ctx(t), "abc"); err != nil {
		t.Fatalf("StartContainer: %v", err)
	}
	if err := c.StopContainer(ctx(t), "abc"); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
	if err := c.RestartContainer(ctx(t), "abc"); err != nil {
		t.Fatalf("RestartContainer: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %v", calls)
	}

	err := c.StartContainer(ctx(t), "missing")
	if err == nil {
		t.Fatal("expected an error starting a missing container")
	}
	if IsUnavailable(err) {
		t.Fatal("a 404 should not be reported as ErrUnavailable")
	}
}

// writeFrame writes one Docker multiplexed-stream frame (used by
// ContainerLogs/Exec) to w.
func writeFrame(w http.ResponseWriter, streamType byte, payload string) {
	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	w.Write(header)
	w.Write([]byte(payload))
}

func TestContainerLogsDemuxesFramedStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/abc/logs", func(w http.ResponseWriter, r *http.Request) {
		if tail := r.URL.Query().Get("tail"); tail != "50" {
			t.Errorf("expected tail=50, got %q", tail)
		}
		writeFrame(w, 1, "stdout line\n")
		writeFrame(w, 2, "stderr line\n")
	})
	c := testClient(t, mux)
	out, err := c.ContainerLogs(ctx(t), "abc", 50)
	if err != nil {
		t.Fatalf("ContainerLogs: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "stdout line") || !strings.Contains(got, "stderr line") {
		t.Fatalf("expected demuxed content from both streams, got %q", got)
	}
}

func TestContainerLogsPassesThroughRawTTYStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/abc/logs", func(w http.ResponseWriter, r *http.Request) {
		// A raw TTY stream has no frame headers at all.
		w.Write([]byte("plain tty output\n"))
	})
	c := testClient(t, mux)
	out, err := c.ContainerLogs(ctx(t), "abc", 0)
	if err != nil {
		t.Fatalf("ContainerLogs: %v", err)
	}
	if string(out) != "plain tty output\n" {
		t.Fatalf("expected raw passthrough, got %q", string(out))
	}
}

func TestExecCreateStartInspect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/abc/exec", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if cmd, _ := body["Cmd"].([]any); len(cmd) == 0 {
			t.Error("expected Cmd to be forwarded")
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"Id": "exec1"})
	})
	mux.HandleFunc("/exec/exec1/start", func(w http.ResponseWriter, r *http.Request) {
		writeFrame(w, 1, "hello from exec\n")
	})
	mux.HandleFunc("/exec/exec1/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ExitCode": 0})
	})
	c := testClient(t, mux)

	result, err := c.Exec(ctx(t), "abc", []string{"echo", "hello from exec"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !strings.Contains(result.Output, "hello from exec") {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestExecRejectsEmptyCommand(t *testing.T) {
	c := testClient(t, http.NewServeMux())
	if _, err := c.Exec(ctx(t), "abc", nil); err == nil {
		t.Fatal("expected an error for an empty command")
	}
}

func TestListImagesVolumesNetworks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"Id": "sha256:abc", "RepoTags": []string{"nginx:latest"}, "Size": 1234, "Created": 111},
			{"Id": "sha256:def", "RepoTags": []string{}, "Size": 5678, "Created": 222},
		})
	})
	mux.HandleFunc("/volumes", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"Volumes": []map[string]any{
				{"Name": "myvol", "Driver": "local", "Mountpoint": "/var/lib/docker/volumes/myvol/_data", "Labels": map[string]string{}},
			},
		})
	})
	mux.HandleFunc("/networks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"Id": "net1", "Name": "bridge", "Driver": "bridge", "Scope": "local", "Labels": map[string]string{}},
		})
	})
	c := testClient(t, mux)

	images, err := c.ListImages(ctx(t))
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(images) != 2 || images[1].Tags[0] != "<none>:<none>" {
		t.Fatalf("unexpected images: %+v", images)
	}

	volumes, err := c.ListVolumes(ctx(t))
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(volumes) != 1 || volumes[0].Name != "myvol" {
		t.Fatalf("unexpected volumes: %+v", volumes)
	}

	networks, err := c.ListNetworks(ctx(t))
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(networks) != 1 || networks[0].Name != "bridge" {
		t.Fatalf("unexpected networks: %+v", networks)
	}
}

func TestEngineErrorMessageSurfaced(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "something broke"})
	})
	c := testClient(t, mux)
	_, err := c.ListImages(ctx(t))
	if err == nil || !strings.Contains(err.Error(), "something broke") {
		t.Fatalf("expected engine error message surfaced, got %v", err)
	}
}
