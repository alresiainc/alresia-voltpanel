package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ListNetworks returns every locally-present network.
func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	resp, err := c.do(ctx, http.MethodGet, "/networks", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var raws []struct {
		ID     string            `json:"Id"`
		Name   string            `json:"Name"`
		Driver string            `json:"Driver"`
		Scope  string            `json:"Scope"`
		Labels map[string]string `json:"Labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raws); err != nil {
		return nil, fmt.Errorf("docker: decode networks: %w", err)
	}
	out := make([]Network, 0, len(raws))
	for _, rn := range raws {
		out = append(out, Network{ID: rn.ID, Name: rn.Name, Driver: rn.Driver, Scope: rn.Scope, Labels: rn.Labels})
	}
	return out, nil
}
