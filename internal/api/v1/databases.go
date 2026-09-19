// databases.go implements the database-admin tool's API (§ "something
// like Adminer"): CRUD over saved connections (internal/domain/dbconn)
// plus browse/query operations delegated to internal/providers/dbadmin.
package v1

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/dbconn"
	"github.com/gin-gonic/gin"
)

func dbConnRepo(d Deps) *dbconn.Repository {
	return dbconn.NewRepository(d.DB())
}

func listDBConnections(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := dbConnRepo(d).List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

// createDBConnectionBody is POST /db-connections's request shape.
// Password is stored through Deps.Secrets *before* the connection row is
// created (the same two-tier Secret abstraction §9.7 pattern
// createServer uses for SSH keys) -- the plaintext password never
// reaches the `db_connections` table, an audit-log line, or this
// handler's response.
type createDBConnectionBody struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password,omitempty"`
	DatabaseName string `json:"databaseName,omitempty"`
}

func createDBConnection(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body createDBConnectionBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		req := dbconn.CreateRequest{
			Name: body.Name, Kind: dbconn.Kind(body.Kind), Host: body.Host,
			Port: body.Port, Username: body.Username, DatabaseName: body.DatabaseName,
		}

		if body.Password != "" {
			if d.Secrets == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "secret storage not configured"})
				return
			}
			req.ID = dbconn.NewID()
			secretRef, err := d.Secrets.Put("db_connection", req.ID, "password", []byte(body.Password))
			// Never log/audit the password itself -- only that a secret
			// was (or wasn't) stored.
			audit(d.DB(), "dbconnection.secret.store", "db_connection", req.ID, resultOf(err))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store password: " + err.Error()})
				return
			}
			req.SecretRef = secretRef
		}

		conn, err := dbConnRepo(d).Create(req)
		audit(d.DB(), "dbconnection.create", "db_connection", conn.ID, resultOf(err))
		if err != nil {
			if req.SecretRef != "" && d.Secrets != nil {
				_ = d.Secrets.Delete(req.SecretRef)
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, conn)
	}
}

// deleteDBConnection requires confirm=true (§9.6), deletes any stored
// password, and evicts every pooled connection dbadmin holds open for it.
func deleteDBConnection(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "delete requires confirm=true"})
			return
		}
		repo := dbConnRepo(d)
		conn, getErr := repo.Get(id)
		err := repo.Delete(id)
		audit(d.DB(), "dbconnection.delete", "db_connection", id, resultOf(err))
		if err != nil {
			writeDBConnError(c, err)
			return
		}
		if getErr == nil && conn.SecretRef != "" && d.Secrets != nil {
			_ = d.Secrets.Delete(conn.SecretRef)
		}
		if d.DBAdmin != nil {
			d.DBAdmin.Evict(id)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func testDBConnection(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.DBAdmin == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database admin is not available"})
			return
		}
		id := c.Param("id")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		err := d.DBAdmin.TestConnection(ctx, id)
		audit(d.DB(), "dbconnection.test", "db_connection", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func listDatabases(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.DBAdmin == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database admin is not available"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		list, err := d.DBAdmin.ListDatabases(ctx, c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func listTables(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.DBAdmin == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database admin is not available"})
			return
		}
		database := c.Query("database")
		if database == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "database is required"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		list, err := d.DBAdmin.ListTables(ctx, c.Param("id"), database)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func listColumns(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.DBAdmin == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database admin is not available"})
			return
		}
		database, table := c.Query("database"), c.Param("table")
		if database == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "database is required"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		list, err := d.DBAdmin.ListColumns(ctx, c.Param("id"), database, table)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func browseTable(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.DBAdmin == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database admin is not available"})
			return
		}
		database, table := c.Query("database"), c.Param("table")
		if database == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "database is required"})
			return
		}
		offset := 0
		if v := c.Query("offset"); v != "" {
			if n, err := parsePositiveInt(v); err == nil {
				offset = n
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		result, err := d.DBAdmin.BrowseTable(ctx, c.Param("id"), database, table, offset)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// runQueryBody is POST /db-connections/:id/query's request shape.
type runQueryBody struct {
	Database string `json:"database"`
	SQL      string `json:"sql"`
}

// runQuery executes exactly the SQL a user typed against a real database
// connection. Like execServer, this is a genuinely powerful endpoint --
// every call is audit-logged with the full query text, always, whether
// it succeeds or fails (§9.8).
func runQuery(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.DBAdmin == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database admin is not available"})
			return
		}
		id := c.Param("id")
		var body runQueryBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.SQL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sql is required"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		result, err := d.DBAdmin.RunQuery(ctx, id, body.Database, body.SQL)
		audit(d.DB(), "dbconnection.query", "db_connection", id, resultOf(err), body.SQL)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func writeDBConnError(c *gin.Context, err error) {
	if errors.Is(err, dbconn.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "connection not found"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
