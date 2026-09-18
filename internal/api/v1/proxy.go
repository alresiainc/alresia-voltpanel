// proxy.go exposes read-only status for the reverse proxy (internal/proxy)
// so the frontend can build a real "reachable at" URL for a bound domain
// instead of guessing a port.
package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func proxyStatus(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Proxy == nil {
			c.JSON(http.StatusOK, gin.H{"port": 0, "routes": gin.H{}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"port": d.Proxy.Port(), "routes": d.Proxy.Routes()})
	}
}
