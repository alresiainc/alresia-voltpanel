package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/deployment"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/server"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// StepResult captures one executed step's outcome -- the whole point of a
// pipeline run's stored `steps` JSON (§6) is being able to see exactly
// what happened, per step, after the fact.
type StepResult struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"` // "success" | "failed"
	Output     string `json:"output"`
	DurationMs int64  `json:"durationMs"`
}

// Engine runs a Definition's steps in order, stopping at the first
// failure (§20: a linear step runner, not a general DAG/DSL). Each step's
// log capture reuses the exact pattern proven in
// internal/agent.Manager.pipeLogs / internal/domain/deployment.Engine --
// this file doesn't reinvent that, it just calls into
// internal/domain/deployment.Engine for the one step kind ("deploy") that
// needs it.
type Engine struct {
	Remote       providers.RemoteProvider
	Servers      *server.Repository
	DeployEngine *deployment.Engine
	HTTPClient   *http.Client
}

func (e *Engine) httpClient() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Run executes every step in def in order. It always returns the full
// StepResult slice for whatever ran (including the failing step), even
// when it also returns an error -- callers persist both.
func (e *Engine) Run(ctx context.Context, def Definition) ([]StepResult, error) {
	results := make([]StepResult, 0, len(def.Steps))
	for _, step := range def.Steps {
		start := time.Now()
		output, err := e.runStep(ctx, step)
		res := StepResult{Name: step.Name, Kind: step.Kind(), Output: output, DurationMs: time.Since(start).Milliseconds()}
		if err != nil {
			res.Status = "failed"
			res.Output += "\nERROR: " + err.Error()
			results = append(results, res)
			return results, fmt.Errorf("step %q: %w", step.Name, err)
		}
		res.Status = "success"
		results = append(results, res)
	}
	return results, nil
}

func (e *Engine) runStep(ctx context.Context, step Step) (string, error) {
	switch step.Kind() {
	case "run":
		return e.runLocal(ctx, step.Run)
	case "ssh":
		return e.runSSH(ctx, step.SSH)
	case "deploy":
		return e.runDeploy(ctx, step.Deploy)
	case "healthcheck":
		return e.runHealthCheck(ctx, step.HealthCheck)
	default:
		return "", fmt.Errorf("unknown step kind")
	}
}

// runLocal executes a build/test command on the daemon's own host (e.g.
// `npm test`) -- distinct from an "ssh" step, which runs on a configured
// remote Server.
func (e *Engine) runLocal(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *Engine) runSSH(ctx context.Context, s *SSHStep) (string, error) {
	if s == nil || s.ServerID == "" || s.Command == "" {
		return "", fmt.Errorf("ssh step requires serverId and command")
	}
	srv, err := e.Servers.Get(s.ServerID)
	if err != nil {
		return "", err
	}
	sess, err := e.Remote.Connect(ctx, srv.ToProviderServer())
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer sess.Close()
	stdout, stderr, err := sess.Exec(ctx, s.Command)
	out := string(stdout) + string(stderr)
	if err != nil {
		return out, err
	}
	return out, nil
}

// runDeploy delegates entirely to internal/domain/deployment.Engine --
// checkout, install, restart, and health-check are all its job already
// (Phase 9); a pipeline "deploy" step is just "run that."
func (e *Engine) runDeploy(ctx context.Context, targetID string) (string, error) {
	if e.DeployEngine == nil {
		return "", fmt.Errorf("deploy step requires a configured DeployEngine")
	}
	d, err := e.DeployEngine.Deploy(ctx, targetID)
	log, _ := e.DeployEngine.Deployments.Log(d.ID)
	if err != nil {
		return log, err
	}
	return log, nil
}

func (e *Engine) runHealthCheck(ctx context.Context, h *HealthCheckStep) (string, error) {
	if h == nil || h.URL == "" {
		return "", fmt.Errorf("healthcheck step requires a url")
	}
	expected := h.ExpectedStatus
	if expected == 0 {
		expected = 200
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := e.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	out := fmt.Sprintf("GET %s -> HTTP %d (expected %d)", h.URL, resp.StatusCode, expected)
	if resp.StatusCode != expected {
		return out, fmt.Errorf("unexpected status %d, want %d", resp.StatusCode, expected)
	}
	return out, nil
}
