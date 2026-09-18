package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ListVolumes returns every locally-present volume.
func (c *Client) ListVolumes(ctx context.Context) ([]Volume, error) {
	resp, err := c.do(ctx, http.MethodGet, "/volumes", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var payload struct {
		Volumes []struct {
			Name       string            `json:"Name"`
			Driver     string            `json:"Driver"`
			Mountpoint string            `json:"Mountpoint"`
			Labels     map[string]string `json:"Labels"`
		} `json:"Volumes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("docker: decode volumes: %w", err)
	}
	out := make([]Volume, 0, len(payload.Volumes))
	for _, v := range payload.Volumes {
		out = append(out, Volume{Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint, Labels: v.Labels})
	}
	return out, nil
}
