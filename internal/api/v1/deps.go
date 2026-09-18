// Package v1 holds VoltPanel's versioned HTTP/WS API: transport concerns
// only (routing, request decoding, response encoding). Business logic lives
// in internal/agent, internal/storage, internal/ws, etc. -- handlers here
// just call into those.
package v1

import (
	"database/sql"

	"github.com/alresiainc/alresia-voltpanel/internal/agent"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
)

// Deps bundles everything handlers need. Constructed once in internal/server
// and passed down instead of handlers reaching for package-level globals.
type Deps struct {
	Store     *storage.Store
	Mgr       *agent.Manager
	Hub       *ws.Hub
	Session   *security.SessionAuth
	Token     string
	Dev       bool
	// Providers holds registered RuntimeProviders (and, in later phases,
	// other provider kinds) by Kind() -- see internal/providers (§7).
	// Handlers always go through it rather than a concrete provider
	// package.
	Providers *providers.Registry
}

func (d Deps) DB() *sql.DB { return d.Store.DB() }
