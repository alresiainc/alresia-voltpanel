// Package providers defines VoltPanel's provider contract (§7 of the
// implementation plan): a small set of Go interfaces so runtimes,
// databases, web servers, local domains/SSL, remote servers, git hosts and
// deployment targets are pluggable rather than hardcoded. API handlers and
// domain services depend only on these interfaces plus the Registry
// (registry.go), never on a concrete provider package -- that's what lets
// an out-of-process provider (Phase 11) slot in identically to a built-in
// one.
//
// Phase 2 implements only RuntimeProvider (internal/providers/runtime/node,
// .../php). Every other interface below is defined now, with its minimal
// supporting types, so the shape is stable before later phases (3/4/6/7/
// 8/9) build real implementations against it -- re-designing the contract
// after several providers exist against it is exactly what the plan's risk
// list (§34) warns against.
package providers

import (
	"context"
	"io"
	"time"
)

// ProgressFunc reports incremental progress during a long-running provider
// operation (e.g. installing a runtime version). percent is 0-100; message
// is a short human-readable status line. Callers may pass nil when they
// don't care about progress.
type ProgressFunc func(percent int, message string)

// RuntimeVersion is what RuntimeProvider.DetectInstalled reports back per
// installed version of a runtime (node, php, ...). It mirrors the shape of
// the runtime_versions table (internal/storage/migrations/0001_init.sql)
// but stays provider-shaped rather than storage-shaped -- internal/domain/
// runtime is what maps between the two.
type RuntimeVersion struct {
	Version     string
	InstallPath string
	IsDefault   bool
}

// RuntimeProvider manages installed versions of one language runtime
// (node, php, python, go, ...). This is the only provider interface
// implemented in Phase 2 (internal/providers/runtime/{node,php}); it
// validates the abstraction before more providers are built against it.
type RuntimeProvider interface {
	// Kind identifies the runtime this provider manages, e.g. "node", "php".
	Kind() string
	// DetectInstalled inspects the local machine for installed versions of
	// this runtime and returns what it finds. Never mutates anything.
	DetectInstalled(ctx context.Context) ([]RuntimeVersion, error)
	// Install downloads/installs the given version. progress may be nil.
	Install(ctx context.Context, version string, progress ProgressFunc) error
	// Remove uninstalls the given version.
	Remove(ctx context.Context, version string) error
	// SetDefault marks the given (already-installed) version as the
	// system/user default, e.g. via a version manager's "use"/"global"
	// command.
	SetDefault(ctx context.Context, version string) error
}

// --------------------------------------------------------------------------
// Everything below defines the stable interface shape for provider kinds
// later phases implement (§7). Nothing below is implemented in Phase 2 --
// these types are intentionally minimal placeholders; the phase that
// implements each interface for real (noted per type) is expected to grow
// or relocate the type into its own internal/domain/* package without
// changing the interface's method signatures.

// Service is the minimal shape ServiceLifecycle operates on until Phase 5
// formalizes internal/domain/service (replacing internal/agent.Manager).
type Service struct {
	ID      string
	Name    string
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string
}

// ServiceStatus reports a provider's live-state view of a Service.
type ServiceStatus struct {
	Running bool
	PID     int
	Detail  string
}

// ServiceLifecycle is implemented by anything that can start/stop/inspect a
// managed service (a native process, a Docker container, a managed
// database engine). Phase 5 implements this for native processes; Phase 6
// for Docker; database/webserver providers embed it.
type ServiceLifecycle interface {
	Start(ctx context.Context, svc Service) error
	Stop(ctx context.Context, svc Service, graceful bool) error
	Status(ctx context.Context, svc Service) (ServiceStatus, error)
	Logs(ctx context.Context, svc Service, tail bool) (io.ReadCloser, error)
}

// DatabaseProvider manages a database engine's lifecycle, plus the
// version-vs-data-dir separation called out in §9/§6 of the plan. Phase 2
// (per the gap analysis table, §3) targets this for MySQL/Postgres/Redis.
type DatabaseProvider interface {
	ServiceLifecycle
	CreateDatabase(ctx context.Context, name string) error
	// Versions lists installable/installed engine versions, kept separate
	// from DataDir so switching engine version never implies wiping data.
	Versions(ctx context.Context) ([]string, error)
	DataDir(ctx context.Context) (string, error)
}

// Domain is the minimal shape DomainProvider/WebServerProvider/SSLProvider
// operate on until Phase 4 formalizes internal/domain/domainname.
type Domain struct {
	ID       string
	Hostname string
	Port     int
}

// Project is the minimal shape WebServerProvider operates on until Phase 3
// formalizes internal/domain/project.
type Project struct {
	ID   string
	Name string
	Path string
}

// ConfigDiff describes a vhost/config change a WebServerProvider is about
// to apply (or has applied), so the UI can show it before/after committing.
type ConfigDiff struct {
	Path    string
	Before  string
	After   string
	Applied bool
}

// WebServerProvider manages a reverse-proxy/web-server's vhost config for
// project domains (nginx, apache). Implemented in Phase 4.
type WebServerProvider interface {
	ServiceLifecycle
	ApplyVHost(ctx context.Context, domain Domain, project Project) (ConfigDiff, error)
	RemoveVHost(ctx context.Context, domain Domain) error
	Reload(ctx context.Context) error
	// ManagedConfigs distinguishes Volt-owned vhost files from foreign
	// (hand-edited) ones so Volt never silently overwrites the latter.
	ManagedConfigs(ctx context.Context) ([]string, error)
}

// DomainProvider manages local hostname resolution (hosts file, or a local
// DNS server) for project domains like myapp.test. Implemented in Phase 4.
type DomainProvider interface {
	Add(ctx context.Context, hostname string) error
	Remove(ctx context.Context, hostname string) error
	List(ctx context.Context) ([]string, error)
	// Conflicts checks both the hosts file and other registered domains,
	// so two projects never silently fight over the same hostname.
	Conflicts(ctx context.Context, hostname string) ([]string, error)
}

// CAInfo describes the local certificate authority an SSLProvider issues
// certificates from.
type CAInfo struct {
	CommonName string
	NotAfter   string
	Trusted    bool
}

// Certificate is the minimal shape SSLProvider operates on until Phase 4
// formalizes internal/domain/certificate.
type Certificate struct {
	ID        string
	DomainID  string
	NotBefore string
	NotAfter  string
	Status    string
}

// SSLProvider issues/manages local HTTPS certificates from a local CA.
// TrustCA never runs implicitly -- it is always a distinct, explicitly
// user-confirmed call (§9.6, §22). Implemented in Phase 4.
type SSLProvider interface {
	EnsureCA(ctx context.Context) (CAInfo, error)
	IssueCertificate(ctx context.Context, domain string) (Certificate, error)
	Renew(ctx context.Context, certID string) (Certificate, error)
	Revoke(ctx context.Context, certID string) error
	TrustCA(ctx context.Context, confirmed bool) error
}

// RemoteFileInfo is one entry returned by RemoteSession.ListDir -- the
// minimal shape a remote file browser (Phase 7, internal/providers/remote/ssh)
// needs to render a listing, over SFTP.
type RemoteFileInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"modTime"`
}

// RemoteMetrics is a best-effort, provider-agnostic snapshot of a remote
// server's live state -- deliberately shaped like the local
// ServiceLifecycle/ServiceStatus story (§7's explicit note that remote
// services should look like local ones to the UI) rather than a rich,
// provider-specific metrics format. Raw carries anything else parsed
// out of the underlying command's output (e.g. /proc/meminfo fields) that
// doesn't have its own struct field yet.
type RemoteMetrics struct {
	Uptime      string            `json:"uptime"`
	LoadAverage string            `json:"loadAverage"`
	MemTotalKB  int64             `json:"memTotalKb"`
	MemFreeKB   int64             `json:"memFreeKb"`
	Raw         map[string]string `json:"raw,omitempty"`
}

// RemoteSession exposes operations over one already-established remote
// (SSH) connection -- Exec, a basic SFTP-backed file browser, and a
// best-effort Metrics call. Implemented in Phase 7
// (internal/providers/remote/ssh). It never assumes a Volt daemon runs on
// the far end (§7): every operation is a plain command or SFTP call any
// sshd supports.
type RemoteSession interface {
	// Exec runs command and returns its captured stdout/stderr once it
	// exits (or ctx is cancelled, in which case the session is closed and
	// ctx.Err() is returned alongside whatever output was captured so far).
	Exec(ctx context.Context, command string) (stdout, stderr []byte, err error)
	// ListDir lists one remote directory over SFTP.
	ListDir(ctx context.Context, path string) ([]RemoteFileInfo, error)
	// ReadFile reads one remote file's full contents over SFTP.
	ReadFile(ctx context.Context, path string) ([]byte, error)
	// WriteFile writes (creating or truncating) one remote file over SFTP.
	WriteFile(ctx context.Context, path string, data []byte) error
	// Metrics reports a best-effort snapshot of the remote host's basic
	// stats (uptime/load/memory) -- see RemoteMetrics.
	Metrics(ctx context.Context) (RemoteMetrics, error)
	Close() error
}

// Server is the minimal shape RemoteProvider operates on until Phase 7
// formalizes internal/domain/server. AuthMethod is "agent" (default,
// preferred per §9.9) or "key"; SecretRef is only meaningful for "key" and
// is resolved through the Secret abstraction (internal/security), never a
// plaintext key file.
type Server struct {
	ID         string
	Hostname   string
	Port       int
	Username   string
	AuthMethod string
	SecretRef  string
}

// RemoteProvider connects to a remote server. It never assumes a Volt
// daemon runs on the far end (§7). Implemented in Phase 7.
type RemoteProvider interface {
	Connect(ctx context.Context, server Server) (RemoteSession, error)
}

// Repo and Branch are the minimal shapes GitProvider operates on until
// Phase 8 formalizes internal/integrations/git.
type Repo struct {
	ID       string
	Name     string
	CloneURL string
}

type Branch struct {
	Name string
	SHA  string
}

// GitProvider integrates with a git hosting service (GitHub first, per
// §17 Phase 8; GitLab/Bitbucket to follow the same interface).
type GitProvider interface {
	ListRepos(ctx context.Context) ([]Repo, error)
	Clone(ctx context.Context, repo Repo, dest string) error
	Branches(ctx context.Context, repo Repo) ([]Branch, error)
}

// DeploymentSpec, Deployment and RollbackScope are the minimal shapes
// DeploymentProvider operates on until Phase 9 formalizes
// internal/domain/deployment.
type DeploymentSpec struct {
	ProjectID string
	ServerID  string
	CommitSHA string
	Branch    string
}

type Deployment struct {
	ID        string
	ProjectID string
	ServerID  string
	Status    string
}

// RollbackScope keeps the three-way rollback split from §18 explicit:
// migration rollback is never automatic and has no field here on purpose.
type RollbackScope struct {
	Code     bool
	Artifact bool
}

// DeploymentProvider ships code to a Server and can roll it back.
// Implemented in Phase 9.
type DeploymentProvider interface {
	Deploy(ctx context.Context, spec DeploymentSpec) (*Deployment, error)
	Rollback(ctx context.Context, deploymentID string, scope RollbackScope) error
}
