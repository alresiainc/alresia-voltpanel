package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestIntegrationDockerContainerLifecycle exercises a real container
// against whatever Docker daemon (if any) is reachable on this machine.
//
// Per the Phase 6 plan: this must skip cleanly (never fail) in an
// environment without Docker, since that's the expected state on most
// dev/CI boxes running this suite. When a daemon IS reachable, it creates a
// uniquely-named, clearly-labeled throwaway container from a small public
// image, exercises list/inspect/start/logs/exec/restart/stop, and removes
// the container unconditionally afterward -- it never touches anything
// pre-existing.
func TestIntegrationDockerContainerLifecycle(t *testing.T) {
	c, err := NewClient()
	if err != nil {
		t.Skipf("docker: %v (skipping live integration test)", err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Available(pingCtx); err != nil {
		t.Skipf("no Docker daemon reachable at %s, skipping live integration test: %v", c.SocketPath(), err)
	}

	longCtx, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	if err := ensureAlpineImage(longCtx, c); err != nil {
		t.Skipf("could not pull test image (likely no network in this sandbox), skipping: %v", err)
	}

	name := fmt.Sprintf("voltpanel-test-%d", time.Now().UnixNano())
	id, err := createTestContainer(longCtx, c, name, []string{"sh", "-c", "echo hello-from-voltpanel-test; sleep 300"})
	if err != nil {
		t.Fatalf("create test container: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.StopContainer(cleanupCtx, id)
		removeTestContainer(cleanupCtx, c, id)
	})

	if err := c.StartContainer(longCtx, id); err != nil {
		t.Fatalf("StartContainer: %v", err)
	}

	containers, err := c.ListContainers(longCtx)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	var found bool
	for _, ct := range containers {
		if ct.ID == id {
			found = true
			if ct.State != "running" {
				t.Errorf("expected test container to be running, got state=%q", ct.State)
			}
		}
	}
	if !found {
		t.Fatalf("test container %s not found in ListContainers", id)
	}

	detail, err := c.InspectContainer(longCtx, id)
	if err != nil {
		t.Fatalf("InspectContainer: %v", err)
	}
	if detail.State != "running" {
		t.Errorf("expected inspect to report running, got %q", detail.State)
	}

	// Give the container a moment to emit its startup line.
	time.Sleep(500 * time.Millisecond)
	logs, err := c.ContainerLogs(longCtx, id, 0)
	if err != nil {
		t.Fatalf("ContainerLogs: %v", err)
	}
	if !bytes.Contains(logs, []byte("hello-from-voltpanel-test")) {
		t.Errorf("expected logs to contain startup line, got: %q", string(logs))
	}

	execResult, err := c.Exec(longCtx, id, []string{"echo", "exec-ok"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !strings.Contains(execResult.Output, "exec-ok") {
		t.Errorf("expected exec output to contain 'exec-ok', got %q", execResult.Output)
	}
	if execResult.ExitCode != 0 {
		t.Errorf("expected exec exit code 0, got %d", execResult.ExitCode)
	}

	if err := c.RestartContainer(longCtx, id); err != nil {
		t.Fatalf("RestartContainer: %v", err)
	}

	if err := c.StopContainer(longCtx, id); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
}

// ensureAlpineImage pulls alpine:latest if it isn't already present,
// draining the pull-progress stream. Kept local to the test file since
// pulling images isn't part of Phase 6's required feature surface.
func ensureAlpineImage(ctx context.Context, c *Client) error {
	resp, err := c.do(ctx, http.MethodPost, "/images/create?fromImage=alpine&tag=latest", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull failed: status %d", resp.StatusCode)
	}
	return nil
}

func createTestContainer(ctx context.Context, c *Client, name string, cmd []string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"Image":  "alpine:latest",
		"Cmd":    cmd,
		"Labels": map[string]string{"com.voltpanel.test": "1"},
	})
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(name), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create failed: status %d: %s", resp.StatusCode, b)
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func removeTestContainer(ctx context.Context, c *Client, id string) {
	resp, err := c.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id)+"?force=true", nil)
	if err == nil {
		resp.Body.Close()
	}
}
