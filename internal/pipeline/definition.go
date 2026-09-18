// Package pipeline implements Phase 10 (§17/§13): a YAML pipeline
// definition + a linear step runner. Deliberately not a general CI/CD DSL
// -- no conditionals, matrices, or reusable templates (§20 explicitly
// rules that out for this phase); a pipeline is just an ordered list of
// steps, each one of a small fixed set of kinds, run until the first
// failure.
package pipeline

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Step is one step in a Definition. Exactly one of Run/SSH/Deploy/
// HealthCheck should be set -- Kind() reports which.
type Step struct {
	Name        string           `yaml:"name"`
	Run         string           `yaml:"run,omitempty"`         // local shell command (os/exec on the daemon's own host)
	SSH         *SSHStep         `yaml:"ssh,omitempty"`         // remote command via a Server (RemoteProvider.Exec)
	Deploy      string           `yaml:"deploy,omitempty"`      // a DeploymentTarget id -- reuses internal/domain/deployment.Engine wholesale
	HealthCheck *HealthCheckStep `yaml:"healthcheck,omitempty"`
}

type SSHStep struct {
	ServerID string `yaml:"serverId"`
	Command  string `yaml:"command"`
}

type HealthCheckStep struct {
	URL            string `yaml:"url"`
	ExpectedStatus int    `yaml:"expectedStatus"`
}

// Kind reports which step type this is, for the engine's dispatch and for
// validation error messages.
func (s Step) Kind() string {
	switch {
	case s.Run != "":
		return "run"
	case s.SSH != nil:
		return "ssh"
	case s.Deploy != "":
		return "deploy"
	case s.HealthCheck != nil:
		return "healthcheck"
	default:
		return ""
	}
}

type Definition struct {
	Steps []Step `yaml:"steps"`
}

// Parse decodes and validates a pipeline definition. Validation is
// deliberately minimal: every step needs a name and exactly one kind --
// anything the step's own kind requires (e.g. a healthcheck's URL) is
// checked at run time, where a clear per-step error is more useful than a
// wall of upfront schema errors.
func Parse(yamlText string) (Definition, error) {
	var def Definition
	if err := yaml.Unmarshal([]byte(yamlText), &def); err != nil {
		return Definition{}, fmt.Errorf("parse pipeline yaml: %w", err)
	}
	if len(def.Steps) == 0 {
		return Definition{}, fmt.Errorf("pipeline has no steps")
	}
	for i, s := range def.Steps {
		if s.Name == "" {
			return Definition{}, fmt.Errorf("step %d: name is required", i)
		}
		if s.Kind() == "" {
			return Definition{}, fmt.Errorf("step %q: exactly one of run/ssh/deploy/healthcheck is required", s.Name)
		}
	}
	return def, nil
}
