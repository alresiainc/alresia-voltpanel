package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// rawContainer mirrors the shape of one item in GET /containers/json.
type rawContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	Command string            `json:"Command"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
	Ports   []rawPort         `json:"Ports"`
}

type rawPort struct {
	IP          string `json:"IP,omitempty"`
	PrivatePort uint16 `json:"PrivatePort"`
	PublicPort  uint16 `json:"PublicPort,omitempty"`
	Type        string `json:"Type"`
}

func (rc rawContainer) toContainer() Container {
	ports := make([]Port, 0, len(rc.Ports))
	for _, p := range rc.Ports {
		ports = append(ports, Port{IP: p.IP, PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type})
	}
	names := make([]string, 0, len(rc.Names))
	for _, n := range rc.Names {
		names = append(names, strings.TrimPrefix(n, "/"))
	}
	return Container{
		ID: rc.ID, Names: names, Image: rc.Image, Command: rc.Command,
		State: rc.State, Status: rc.Status, Labels: rc.Labels, Ports: ports,
		Project: rc.Labels[composeProjectLabel],
	}
}

// ListContainers returns every container (running and stopped, matching
// `docker ps -a`).
func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	resp, err := c.do(ctx, http.MethodGet, "/containers/json?all=true", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var raws []rawContainer
	if err := json.NewDecoder(resp.Body).Decode(&raws); err != nil {
		return nil, fmt.Errorf("docker: decode containers: %w", err)
	}
	out := make([]Container, 0, len(raws))
	for _, rc := range raws {
		out = append(out, rc.toContainer())
	}
	return out, nil
}

// inspectResponse mirrors the (much richer, differently-shaped) response of
// GET /containers/{id}/json.
type inspectResponse struct {
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	RestartCount int    `json:"RestartCount"`
	State        struct {
		Status string `json:"Status"`
	} `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Cmd    []string          `json:"Cmd"`
		Env    []string          `json:"Env"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
}

// InspectContainer returns detailed information about one container.
func (c *Client) InspectContainer(ctx context.Context, id string) (*ContainerDetail, error) {
	resp, err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("docker: container %q not found", id)
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var ir inspectResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		return nil, fmt.Errorf("docker: decode container: %w", err)
	}
	mounts := make([]Mount, 0, len(ir.Mounts))
	for _, m := range ir.Mounts {
		mounts = append(mounts, Mount{Type: m.Type, Source: m.Source, Destination: m.Destination, RW: m.RW})
	}
	return &ContainerDetail{
		Container: Container{
			ID:      ir.ID,
			Names:   []string{strings.TrimPrefix(ir.Name, "/")},
			Image:   ir.Config.Image,
			Command: strings.Join(ir.Config.Cmd, " "),
			State:   ir.State.Status,
			Status:  ir.State.Status,
			Labels:  ir.Config.Labels,
			Project: ir.Config.Labels[composeProjectLabel],
		},
		Env:          ir.Config.Env,
		Mounts:       mounts,
		RestartCount: ir.RestartCount,
	}, nil
}

func (c *Client) containerAction(ctx context.Context, id, action string, okCodes ...int) error {
	resp, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/"+action, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("docker: container %q not found", id)
	}
	return checkStatus(resp, okCodes...)
}

// StartContainer starts a stopped container. Starting an already-running
// container is treated as a no-op success (Docker returns 304).
func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.containerAction(ctx, id, "start", http.StatusNoContent, http.StatusNotModified)
}

// StopContainer stops a running container. Stopping an already-stopped
// container is treated as a no-op success (Docker returns 304).
func (c *Client) StopContainer(ctx context.Context, id string) error {
	return c.containerAction(ctx, id, "stop", http.StatusNoContent, http.StatusNotModified)
}

// RestartContainer restarts a container (works whether it's currently
// running or stopped).
func (c *Client) RestartContainer(ctx context.Context, id string) error {
	return c.containerAction(ctx, id, "restart", http.StatusNoContent)
}

// ContainerLogs returns up to tail lines of combined stdout+stderr for a
// container (tail <= 0 means "all"). The Engine API multiplexes stdout/
// stderr into framed chunks unless the container was created with a TTY;
// demuxStream handles both cases transparently.
func (c *Client) ContainerLogs(ctx context.Context, id string, tail int) ([]byte, error) {
	q := url.Values{}
	q.Set("stdout", "true")
	q.Set("stderr", "true")
	if tail > 0 {
		q.Set("tail", strconv.Itoa(tail))
	} else {
		q.Set("tail", "all")
	}
	resp, err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/logs?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("docker: container %q not found", id)
	}
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	out, err := demuxStream(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("docker: read logs: %w", err)
	}
	return out, nil
}
