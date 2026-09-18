package v1

import (
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const sessionTTL = 24 * time.Hour

// verifyToken checks the static token and, on success, issues a session
// cookie so the browser doesn't need to keep re-sending (or storing in JS
// -reachable localStorage) the raw token for every request.
func verifyToken(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Token string `json:"token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if !d.Dev && subtle.ConstantTimeCompare([]byte(req.Token), []byte(d.Token)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		session := d.Session.Issue(sessionTTL)
		c.SetSameSite(http.SameSiteStrictMode)
		c.SetCookie(SessionCookieName, session, int(sessionTTL.Seconds()), "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
