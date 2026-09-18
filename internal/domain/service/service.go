// Package service formalizes the Service domain entity described in §6/§7
// of the implementation plan, replacing internal/agent.Manager (which this
// package supersedes -- internal/agent is deleted as part of this change).
//
// A Service carries both the live-process fields (PID, Status, log file)
// that internal/agent.ProcInfo used to hold and the persisted lifecycle
// configuration (autostart, dependencies, restart policy) that
// storage.App never had a place for. Manager (see manager.go) is this
// package's ServiceLifecycle-shaped implementation: Start/Stop(graceful)/
// Status/Logs, loosely matching §7's ServiceLifecycle interface without
// the full multi-provider abstraction Phase 2 introduces later.
package service

import (
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

// Status is a service's current lifecycle state.
type Status string

const (
	StatusStopped Status = "stopped"
	StatusRunning Status = "running"
	// StatusFailed means the service crashed and either has no restart
	// policy or exhausted its restart budget (§ crash detection).
	StatusFailed Status = "failed"
)

// RestartPolicy controls what Manager does when a service's process exits
// on its own (not via a deliberate Stop call).
type RestartPolicy string

const (
	// RestartAlways restarts the service regardless of exit code.
	RestartAlways RestartPolicy = "always"
	// RestartOnFailure restarts only on a non-zero exit code. This is the
	// default for anything with a restart policy set at all.
	RestartOnFailure RestartPolicy = "on-failure"
	// RestartNever disables automatic restart entirely -- the default for
	// ad-hoc services started via the API without any explicit policy,
	// preserving internal/agent.Manager's old "never auto-restarts"
	// behavior unless a caller opts in.
	RestartNever RestartPolicy = "no"
)

const (
	defaultGracefulTimeout = 5 * time.Second
	defaultMaxRestarts     = 5
	defaultRestartWindow   = 60 * time.Second
)

// Service is the domain entity. See package doc.
// JSON tags match internal/agent's old ProcInfo shape (id/name/command/
// args/cwd/env/pid/status/startedAt/exitedAt/code/logFile) for the fields
// that existed there, since internal/api/v1.startService returns a Service
// directly to HTTP callers -- the new lifecycle fields are additive, never
// renames of something that used to be on the wire.
type Service struct {
	ID        string            `json:"id"`
	ProjectID string            `json:"projectId,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	Name      string            `json:"name"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Cwd       string            `json:"cwd"`
	Env       map[string]string `json:"env"`

	Autostart       bool          `json:"autostart,omitempty"`
	DependsOn       []string      `json:"dependsOn,omitempty"`
	RestartPolicy   RestartPolicy `json:"restartPolicy,omitempty"`
	GracefulTimeout time.Duration `json:"gracefulTimeoutNs,omitempty"`
	MaxRestarts     int           `json:"maxRestarts,omitempty"`
	RestartWindow   time.Duration `json:"restartWindowNs,omitempty"`

	PID       int        `json:"pid"`
	Status    Status     `json:"status"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	ExitedAt  *time.Time `json:"exitedAt,omitempty"`
	ExitCode  *int       `json:"code,omitempty"`
	LogFile   string     `json:"logFile"`
}

// StartRequest is what callers (the API layer, autostart) provide to start
// a service for the first time. Unset optional fields fall back to sane
// defaults (see applyDefaults) rather than requiring every caller to know
// about restart policy/timeouts.
type StartRequest struct {
	ID      string
	Name    string
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string

	ProjectID       string
	Kind            string
	Autostart       bool
	DependsOn       []string
	RestartPolicy   RestartPolicy
	GracefulTimeout time.Duration
	MaxRestarts     int
	RestartWindow   time.Duration
}

func (r StartRequest) toService() Service {
	return Service{
		ID: r.ID, Name: r.Name, Command: r.Command, Args: r.Args, Cwd: r.Cwd, Env: r.Env,
		ProjectID: r.ProjectID, Kind: defaultString(r.Kind, "native"),
		Autostart: r.Autostart, DependsOn: r.DependsOn,
		RestartPolicy:   defaultPolicy(r.RestartPolicy),
		GracefulTimeout: defaultDuration(r.GracefulTimeout, defaultGracefulTimeout),
		MaxRestarts:     defaultInt(r.MaxRestarts, defaultMaxRestarts),
		RestartWindow:   defaultDuration(r.RestartWindow, defaultRestartWindow),
		Status:          StatusStopped,
	}
}

func toRecord(s Service) storage.ServiceRecord {
	return storage.ServiceRecord{
		ID: s.ID, ProjectID: s.ProjectID, Kind: s.Kind, Name: s.Name, Command: s.Command,
		Args: s.Args, Cwd: s.Cwd, Env: s.Env,
		Autostart: s.Autostart, DependsOn: s.DependsOn,
		RestartPolicy:          string(s.RestartPolicy),
		GracefulTimeoutSeconds: int(s.GracefulTimeout / time.Second),
		MaxRestarts:            s.MaxRestarts,
		RestartWindowSeconds:   int(s.RestartWindow / time.Second),
		Status:                 string(s.Status),
		PID:                    s.PID,
		LogFile:                s.LogFile,
		StartedAt:              s.StartedAt,
		ExitedAt:               s.ExitedAt,
		ExitCode:               s.ExitCode,
	}
}

func fromRecord(r storage.ServiceRecord) Service {
	return Service{
		ID: r.ID, ProjectID: r.ProjectID, Kind: r.Kind, Name: r.Name, Command: r.Command,
		Args: r.Args, Cwd: r.Cwd, Env: r.Env,
		Autostart: r.Autostart, DependsOn: r.DependsOn,
		RestartPolicy:   defaultPolicy(RestartPolicy(r.RestartPolicy)),
		GracefulTimeout: defaultDuration(time.Duration(r.GracefulTimeoutSeconds)*time.Second, defaultGracefulTimeout),
		MaxRestarts:     defaultInt(r.MaxRestarts, defaultMaxRestarts),
		RestartWindow:   defaultDuration(time.Duration(r.RestartWindowSeconds)*time.Second, defaultRestartWindow),
		Status:          Status(defaultString(r.Status, string(StatusStopped))),
		PID:             r.PID,
		LogFile:         r.LogFile,
		StartedAt:       r.StartedAt,
		ExitedAt:        r.ExitedAt,
		ExitCode:        r.ExitCode,
	}
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func defaultPolicy(v RestartPolicy) RestartPolicy {
	if v == "" {
		return RestartNever
	}
	return v
}

func defaultDuration(v, fallback time.Duration) time.Duration {
	if v <= 0 {
		return fallback
	}
	return v
}

func defaultInt(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}
