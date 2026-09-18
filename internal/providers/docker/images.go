package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type rawImage struct {
	ID       string   `json:"Id"`
	RepoTags []string `json:"RepoTags"`
	Size     int64    `json:"Size"`
	Created  int64    `json:"Created"`
}

// ListImages returns every locally-present image.
func (c *Client) ListImages(ctx context.Context) ([]Image, error) {
	resp, err := c.do(ctx, http.MethodGet, "/images/json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var raws []rawImage
	if err := json.NewDecoder(resp.Body).Decode(&raws); err != nil {
		return nil, fmt.Errorf("docker: decode images: %w", err)
	}
	out := make([]Image, 0, len(raws))
	for _, ri := range raws {
		tags := ri.RepoTags
		if len(tags) == 0 {
			tags = []string{"<none>:<none>"}
		}
		out = append(out, Image{ID: ri.ID, Tags: tags, Size: ri.Size, Created: ri.Created})
	}
	return out, nil
}
