// Package v1 holds VoltPanel's versioned HTTP/WS API: transport concerns
// only (routing, request decoding, response encoding). Business logic lives
// in internal/domain/service, internal/storage, internal/ws, etc. --
// handlers here just call into those.
package v1

import (
	"database/sql"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/service"
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
}

func (d Deps) DB() *sql.DB { return d.Store.DB() }
