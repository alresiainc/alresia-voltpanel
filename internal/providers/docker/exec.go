package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// ExecResult is the captured output of a one-shot exec. This is the "basic
// terminal" capability the plan calls for (§17 Phase 6) -- not a full
// interactive PTY, just create+start+collect, which covers "run a command
// in a running container and see what it printed" without a terminal
// protocol on top of the WS hub.
type ExecResult struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
}

// Exec runs cmd inside the given running container and returns its combined
// stdout+stderr plus exit code, once the command finishes. There is no
// timeout applied here beyond ctx -- callers (the HTTP handler) are expected
// to bound it via the request context.
func (c *Client) Exec(ctx context.Context, containerID string, cmd []string) (*ExecResult, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("docker: exec requires a non-empty command")
	}

	createBody, err := json.Marshal(map[string]any{
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          false,
		"Cmd":          cmd,
	})
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(containerID)+"/exec", bytes.NewReader(createBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("docker: container %q not found", containerID)
	}
	if err := checkStatus(resp, http.StatusCreated); err != nil {
		return nil, err
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("docker: decode exec-create response: %w", err)
	}

	startBody, err := json.Marshal(map[string]any{"Detach": false, "Tty": false})
	if err != nil {
		return nil, err
	}
	startResp, err := c.do(ctx, http.MethodPost, "/exec/"+created.ID+"/start", bytes.NewReader(startBody))
	if err != nil {
		return nil, err
	}
	defer startResp.Body.Close()
	if err := checkStatus(startResp, http.StatusOK); err != nil {
		return nil, err
	}
	out, err := demuxStream(startResp.Body)
	if err != nil {
		return nil, fmt.Errorf("docker: read exec output: %w", err)
	}

	result := &ExecResult{Output: string(out)}

	inspectResp, err := c.do(ctx, http.MethodGet, "/exec/"+created.ID+"/json", nil)
	if err != nil {
		// The command already ran and we have its output; exit code is a
		// nice-to-have on top, not worth failing the whole call for.
		return result, nil
	}
	defer inspectResp.Body.Close()
	var execInspect struct {
		ExitCode int `json:"ExitCode"`
	}
	if err := json.NewDecoder(inspectResp.Body).Decode(&execInspect); err == nil {
		result.ExitCode = execInspect.ExitCode
	}
	return result, nil
}
