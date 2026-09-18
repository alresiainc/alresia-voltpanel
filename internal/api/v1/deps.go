// Package v1 holds VoltPanel's versioned HTTP/WS API: transport concerns
// only (routing, request decoding, response encoding). Business logic lives
// in internal/domain/service, internal/storage, internal/ws, etc. --
// handlers here just call into those.
package v1

import (
	"database/sql"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/service"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/docker"
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
	// Secrets is the Secret abstraction (§9.7) servers.go stores/resolves
	// SSH key material through -- the `secrets` table only ever sees
	// storage_backend metadata, never plaintext.
	Secrets *security.SecretStore
	// Remote is the RemoteProvider (§7) servers.go connects through for
	// TestConnection/exec/metrics/file-browsing. Never nil in production
	// (internal/providers/remote/ssh.New()); tests may swap in a fake.
	Remote providers.RemoteProvider
}

func (d Deps) DB() *sql.DB { return d.Store.DB() }
