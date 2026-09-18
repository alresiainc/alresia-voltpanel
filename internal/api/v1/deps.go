// Package v1 holds VoltPanel's versioned HTTP/WS API: transport concerns
// only (routing, request decoding, response encoding). Business logic lives
// in internal/domain/service, internal/storage, internal/ws, etc. --
// handlers here just call into those.
package v1

import (
	"database/sql"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/deployment"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/extension"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/job"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/service"
	"github.com/alresiainc/alresia-voltpanel/internal/pipeline"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/docker"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/pkgmanager/brew"
	"github.com/alresiainc/alresia-voltpanel/internal/proxy"
	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
)

// Deps bundles everything handlers need. Constructed once in internal/server
// and passed down instead of handlers reaching for package-level globals.
type Deps struct {
	Store   *storage.Store
	Mgr     *service.Manager
	Hub     *ws.Hub
	Session *security.SessionAuth
	Token   string
	Dev     bool
	// Providers holds registered RuntimeProviders (and, in later phases,
	// other provider kinds) by Kind() -- see internal/providers (§7).
	// Handlers always go through it rather than a concrete provider
	// package.
	Providers *providers.Registry
	// Docker is nil only on platforms NewClient outright refuses (Windows,
	// today); otherwise it's always set, even when no daemon is actually
	// reachable -- docker.go handlers probe with Available() per request
	// and report a clean "Docker not available" error rather than assuming
	// a non-nil client means a live daemon.
	Docker *docker.Client
	// Domains and SSL are the Phase 4 (§17) providers -- hosts-file
	// registration and the local CA. Interface-typed (rather than a
	// concrete package like Docker above) so router tests can substitute
	// a fake that never touches the real hosts file or OS trust store; see
	// internal/api/v1/domains_test.go / ssl_test.go.
	Domains providers.DomainProvider
	SSL     providers.SSLProvider
	// Remote is the RemoteProvider (§7 Phase 7) servers.go connects through
	// for TestConnection/exec/metrics/file-browsing. Never nil in
	// production (internal/providers/remote/ssh.Provider); tests may swap
	// in a fake.
	Remote providers.RemoteProvider
	// Extensions loads/enables/disables external providers (§17 Phase 11)
	// and registers/unregisters them into Providers above -- from any
	// handler's point of view, an enabled extension's subprocess is
	// indistinguishable from a built-in provider.
	Extensions *extension.Repository
	// Secrets is the §9.7 Secret abstraction (internal/security.SecretStore)
	// -- git.go stores integration PATs through it, never as plaintext DB
	// rows.
	Secrets *security.SecretStore
	// GitHubBaseURL overrides the GitHub REST API base URL used by git.go's
	// handlers. Empty in production (real api.github.com); tests set it to
	// an httptest.NewServer fake so nothing in the test suite ever makes a
	// real network call.
	GitHubBaseURL string
	// DeployEngine runs the Phase 9 deploy/rollback pipeline over Remote
	// above. Never nil in production; tests build their own Engine value
	// wired to a fake RemoteProvider.
	DeployEngine *deployment.Engine
	// PipelineEngine runs Phase 10 pipeline step definitions -- its
	// "deploy" step kind delegates straight to DeployEngine above.
	PipelineEngine *pipeline.Engine
	// Proxy is the reverse-proxy routing table (internal/proxy) that maps
	// a bound domain's hostname to whichever local port its project is
	// actually running on right now -- see projects.go's startProject/
	// stopProject and domains.go's create/delete, which are what keep it
	// in sync. Never nil in production; nil-checked anyway so router
	// tests that don't care about proxying can omit it.
	Proxy *proxy.Router
	// Packages drives Homebrew for install/upgrade/uninstall/version-switch
	// (§ "install fresh PHP/MySQL/PostgreSQL/Redis..."). Nil on platforms
	// this daemon runs on without Homebrew even being a concept (Windows);
	// packages.go handlers probe Available() per request, same pattern as
	// Docker above, rather than assuming non-nil means usable.
	Packages *brew.Provider
	// Jobs persists the background install/upgrade/uninstall operations
	// Packages kicks off -- see internal/domain/job for why those need to
	// survive a page refresh instead of just living in memory.
	Jobs *job.Repository
}

func (d Deps) DB() *sql.DB { return d.Store.DB() }
