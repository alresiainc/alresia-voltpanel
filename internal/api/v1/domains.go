// domains.go implements GET/POST/DELETE /api/v1/domains and
// GET /api/v1/domains/:id/conflicts (§17 Phase 4). Handlers glue the
// internal/domain/domainname repository (persisted metadata) together
// with Deps.Domains (the real hosts-file DomainProvider) -- the repository
// never touches the filesystem itself, and the provider never touches
// SQLite; this file is where the two meet.
package v1

import (
	"errors"
	"net/http"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/domainname"
	"github.com/gin-gonic/gin"
)

func domainRepo(d Deps) *domainname.Repository {
	return domainname.NewRepository(d.DB())
}

func listDomains(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := domainRepo(d).ListDomains()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

// createDomain registers a new hostname: it checks for hosts-file-level
// conflicts, inserts the DB row (whose UNIQUE(hostname) column is the
// authoritative duplicate check across every other registered domain),
// then adds the real hosts-file entry via the DomainProvider -- rolling
// the DB row back if that second step fails, so the two never drift out of
// sync (§14: "Conflicts checks both the hosts file and other projects'
// registered domains before Add() succeeds").
func createDomain(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Hostname  string `json:"hostname"`
			ProjectID string `json:"projectId"`
			Port      int    `json:"port"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.Hostname == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "hostname must not be empty"})
			return
		}

		if d.Domains != nil {
			conflicts, err := d.Domains.Conflicts(c.Request.Context(), body.Hostname)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			if len(conflicts) > 0 {
				c.JSON(http.StatusConflict, gin.H{"error": "hostname conflicts with an existing hosts-file entry", "conflicts": conflicts})
				return
			}
		}

		repo := domainRepo(d)
		domain, err := repo.CreateDomain(body.Hostname, body.ProjectID, body.Port)
		if err != nil {
			audit(d.DB(), "domain.create", "domain", body.Hostname, "error")
			if errors.Is(err, domainname.ErrHostnameTaken) {
				c.JSON(http.StatusConflict, gin.H{"error": "hostname is already registered"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if d.Domains != nil {
			if err := d.Domains.Add(c.Request.Context(), body.Hostname); err != nil {
				// Roll back the DB row so it never claims a hostname the
				// hosts file doesn't actually have -- best-effort; if the
				// delete itself fails there's nothing more useful to do
				// than surface the original Add error.
				_ = repo.DeleteDomain(domain.ID)
				audit(d.DB(), "domain.create", "domain", body.Hostname, "error")
				c.JSON(http.StatusBadRequest, gin.H{"error": "failed to write hosts-file entry: " + err.Error()})
				return
			}
		}

		audit(d.DB(), "domain.create", "domain", domain.ID, "ok")
		c.JSON(http.StatusCreated, domain)
	}
}

// deleteDomain requires an explicit confirm:true field (§9.6), mirroring
// deleteProject/deleteFile.
func deleteDomain(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "delete requires confirm=true"})
			return
		}

		repo := domainRepo(d)
		domain, err := repo.GetDomain(id)
		if err != nil {
			writeDomainError(c, err)
			return
		}

		if d.Domains != nil {
			if err := d.Domains.Remove(c.Request.Context(), domain.Hostname); err != nil {
				audit(d.DB(), "domain.delete", "domain", id, "error")
				c.JSON(http.StatusBadRequest, gin.H{"error": "failed to remove hosts-file entry: " + err.Error()})
				return
			}
		}

		err = repo.DeleteDomain(id)
		audit(d.DB(), "domain.delete", "domain", id, resultOf(err))
		if err != nil {
			writeDomainError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// domainConflicts reports whatever would stop this domain's hostname from
// being cleanly (re-)added -- purely informational, since the domain
// already exists by the time this is called.
func domainConflicts(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		domain, err := domainRepo(d).GetDomain(id)
		if err != nil {
			writeDomainError(c, err)
			return
		}
		if d.Domains == nil {
			c.JSON(http.StatusOK, gin.H{"conflicts": []string{}})
			return
		}
		conflicts, err := d.Domains.Conflicts(c.Request.Context(), domain.Hostname)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if conflicts == nil {
			conflicts = []string{}
		}
		c.JSON(http.StatusOK, gin.H{"conflicts": conflicts})
	}
}

func writeDomainError(c *gin.Context, err error) {
	if errors.Is(err, domainname.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}
