package v1

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/server"
	"github.com/gin-gonic/gin"
)

func serverRepo(d Deps) *server.Repository {
	return server.NewRepository(d.DB())
}

func listServers(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := serverRepo(d).List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

// createServerBody is POST /servers's request shape. Key is the raw
// private-key material (PEM) for auth_method=="key" servers -- it is
// stored through the Secret abstraction (§9.7) immediately and never
// itself logged, returned, or audit-logged; only the fact that a secret
// was stored is. It's ignored (should be empty) for auth_method=="agent".
type createServerBody struct {
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthMethod string `json:"authMethod"`
	Key        string `json:"key,omitempty"`
}

// createServer registers a new remote server. For auth_method=="key", the
// provided key material is stored via Deps.Secrets *before* the server row
// is created (§9.7 two-tier Secret abstraction: OS keychain first, an
// AES-GCM-encrypted blob otherwise) -- the plaintext key never reaches the
// `servers` table, an audit-log line, or this handler's response.
func createServer(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body createServerBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		authMethod := body.AuthMethod
		if authMethod == "" {
			authMethod = "agent"
		}

		req := server.CreateRequest{
			Name:       body.Name,
			Hostname:   body.Hostname,
			Port:       body.Port,
			Username:   body.Username,
			AuthMethod: authMethod,
		}

		if authMethod == "key" {
			if body.Key == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "auth_method \"key\" requires \"key\" (private key material) in the request body"})
				return
			}
			if d.Secrets == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "secret storage not configured"})
				return
			}
			req.ID = server.NewID()
			secretRef, err := d.Secrets.Put("server", req.ID, "ssh_key", []byte(body.Key))
			// Never log/audit the key material itself -- only that a
			// secret was (or wasn't) stored.
			audit(d.DB(), "server.secret.store", "server", req.ID, resultOf(err))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store ssh key: " + err.Error()})
				return
			}
			req.SecretRef = secretRef
		}

		s, err := serverRepo(d).Create(req)
		audit(d.DB(), "server.create", "server", s.ID, resultOf(err))
		if err != nil {
			// The server row failed after we may have already stored a
			// secret for it (key case) -- clean that up rather than
			// leaving an orphaned secret behind.
			if req.SecretRef != "" && d.Secrets != nil {
				_ = d.Secrets.Delete(req.SecretRef)
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, s)
	}
}

// deleteServer requires an explicit confirm=true (§9.6, the same pattern
// projects.go/files.go use), and also deletes any secret the server
// referenced -- an orphaned key would otherwise sit in the keychain/
// encrypted blob forever with nothing pointing at it.
func deleteServer(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "delete requires confirm=true"})
			return
		}
		repo := serverRepo(d)
		s, getErr := repo.Get(id)
		err := repo.Delete(id)
		audit(d.DB(), "server.delete", "server", id, resultOf(err))
		if err != nil {
			writeServerError(c, err)
			return
		}
		if getErr == nil && s.SecretRef != "" && d.Secrets != nil {
			_ = d.Secrets.Delete(s.SecretRef)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// testServerConnection is the connection-test-before-destructive-action
// check §9's server-security row calls for: connect, run a harmless no-op
// command, close. Intended to be run by the UI before offering exec/file
// actions on a newly-added server.
func testServerConnection(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()

		err := serverRepo(d).TestConnection(ctx, d.Remote, id)
		audit(d.DB(), "server.test", "server", id, resultOf(err))
		if err != nil {
			writeServerError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// serverMetrics reports a best-effort remote snapshot (uptime/load/memory)
// over the same RemoteProvider connection TestConnection uses -- this is
// what lets a remote server's status look roughly like a local service's
// to the UI (§7's ServiceLifecycle-shape note).
func serverMetrics(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		repo := serverRepo(d)
		s, err := repo.Get(id)
		if err != nil {
			writeServerError(c, err)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()

		sess, err := d.Remote.Connect(ctx, s.ToProviderServer())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "connect: " + err.Error()})
			return
		}
		defer sess.Close()

		m, err := sess.Metrics(ctx)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "metrics: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, m)
	}
}

// execServerBody is POST /servers/:id/exec's request shape.
type execServerBody struct {
	Command string `json:"command"`
}

// execServer runs an arbitrary command on the remote server and returns its
// stdout/stderr. This is a genuinely powerful/dangerous endpoint (§9.8) --
// every call is audit-logged with the full command string, always, whether
// it succeeds or fails.
func execServer(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var body execServerBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.Command == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "command is required"})
			return
		}

		repo := serverRepo(d)
		s, err := repo.Get(id)
		if err != nil {
			writeServerError(c, err)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()

		sess, connErr := d.Remote.Connect(ctx, s.ToProviderServer())
		if connErr != nil {
			audit(d.DB(), "server.exec", "server", id, "error", body.Command)
			c.JSON(http.StatusBadGateway, gin.H{"error": "connect: " + connErr.Error()})
			return
		}
		defer sess.Close()

		stdout, stderr, execErr := sess.Exec(ctx, body.Command)
		audit(d.DB(), "server.exec", "server", id, resultOf(execErr), body.Command)
		c.JSON(http.StatusOK, gin.H{
			"stdout":   string(stdout),
			"stderr":   string(stderr),
			"error":    errString(execErr),
			"exitCode": exitCodeOf(execErr),
		})
	}
}

// listServerFiles browses one remote directory over SFTP.
func listServerFiles(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		path := c.Query("path")
		if path == "" {
			path = "."
		}

		repo := serverRepo(d)
		s, err := repo.Get(id)
		if err != nil {
			writeServerError(c, err)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		sess, err := d.Remote.Connect(ctx, s.ToProviderServer())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "connect: " + err.Error()})
			return
		}
		defer sess.Close()

		entries, err := sess.ListDir(ctx, path)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, entries)
	}
}

// readServerFile reads one remote file's contents over SFTP.
func readServerFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		path := c.Query("path")
		if path == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
			return
		}

		repo := serverRepo(d)
		s, err := repo.Get(id)
		if err != nil {
			writeServerError(c, err)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		sess, err := d.Remote.Connect(ctx, s.ToProviderServer())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "connect: " + err.Error()})
			return
		}
		defer sess.Close()

		data, err := sess.ReadFile(ctx, path)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", data)
	}
}

// writeServerFileBody is POST /servers/:id/files/write's request shape.
type writeServerFileBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// writeServerFile writes (creating or truncating) one remote file over
// SFTP -- a destructive-ish action, so it's audit-logged like exec.
func writeServerFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var body writeServerFileBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.Path == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
			return
		}

		repo := serverRepo(d)
		s, err := repo.Get(id)
		if err != nil {
			writeServerError(c, err)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		sess, connErr := d.Remote.Connect(ctx, s.ToProviderServer())
		if connErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "connect: " + connErr.Error()})
			return
		}
		defer sess.Close()

		writeErr := sess.WriteFile(ctx, body.Path, []byte(body.Content))
		audit(d.DB(), "server.file.write", "server", id, resultOf(writeErr), body.Path)
		if writeErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": writeErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func writeServerError(c *gin.Context, err error) {
	if errors.Is(err, server.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	return 1
}
