package v1

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/gin-gonic/gin"
)

const SessionCookieName = security.SessionCookieName

// authMiddleware accepts either a valid session cookie (issued by
// /api/v1/auth/token/verify) or the static bearer token via X-Volt-Token --
// §9.2 of the plan: the session model layers on top of the token rather
// than replacing it, since some callers (scripts, the CLI) will never hold
// a browser cookie jar.
func authMiddleware(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Dev {
			c.Next()
			return
		}
		if cookie, err := c.Cookie(SessionCookieName); err == nil && d.Session.Verify(cookie) {
			c.Next()
			return
		}
		t := c.GetHeader("X-Volt-Token")
		if t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(d.Token)) == 1 {
			c.Next()
			return
		}
		c.AbortWithStatus(http.StatusUnauthorized)
	}
}

// csrfMiddleware rejects cross-origin state-changing requests (§9.4):
// defense against a malicious page in another browser tab making
// credentialed requests against the daemon using the browser's automatic
// same-origin-cookie behavior. Only applied to mutating verbs; GET/HEAD
// never need it since they shouldn't have side effects.
func csrfMiddleware(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		origin := c.GetHeader("Origin")
		if origin != "" && !isLocalOrigin(origin) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
	}
	c.Next()
}

func isLocalOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1" || strings.HasSuffix(host, ".localhost")
}
