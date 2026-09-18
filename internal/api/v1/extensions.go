// extensions.go implements GET/POST/DELETE /api/v1/extensions and
// POST /api/v1/extensions/:id/enable|disable (§17 Phase 11). Handlers glue
// the internal/domain/extension repository together with the shared
// providers.Registry -- Enable/Disable are what actually load/unload the
// external subprocess and register/unregister it, exactly like any other
// RuntimeProvider from the caller's point of view.
package v1

import (
	"net/http"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/extension"
	"github.com/gin-gonic/gin"
)

func listExtensions(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := d.Extensions.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

// installExtension registers an extension from a local path (§17 explicitly
// scopes "install from URL" out for this phase: fetching and running
// arbitrary code from a URL is a materially bigger trust question than
// this task resolves).
func installExtension(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Path string `json:"path"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		ext, err := d.Extensions.Install(c.Request.Context(), body.Path)
		audit(d.DB(), "extension.install", "extension", body.Path, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, ext)
	}
}

func removeExtension(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "removing an extension requires confirm=true"})
			return
		}
		err := d.Extensions.Remove(c.Request.Context(), id)
		audit(d.DB(), "extension.remove", "extension", id, resultOf(err))
		if err != nil {
			if err == extension.ErrNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "extension not found"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func enableExtension(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		ext, err := d.Extensions.Enable(c.Request.Context(), id)
		audit(d.DB(), "extension.enable", "extension", id, resultOf(err))
		if err != nil {
			if err == extension.ErrNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "extension not found"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, ext)
	}
}

func disableExtension(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		ext, err := d.Extensions.Disable(c.Request.Context(), id)
		audit(d.DB(), "extension.disable", "extension", id, resultOf(err))
		if err != nil {
			if err == extension.ErrNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "extension not found"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, ext)
	}
}
