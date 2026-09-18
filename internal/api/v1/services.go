package v1

import (
	"net/http"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/service"
	"github.com/alresiainc/alresia-voltpanel/internal/metrics"
	"github.com/gin-gonic/gin"
)

// serviceView merges the persisted App record with any live process state
// the manager holds in memory, so callers get one consistent view instead
// of the old /services-vs-/processes split (§10).
type serviceView struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	PID     int               `json:"pid"`
	Status  string            `json:"status"`
	LogFile string            `json:"logFile"`
}

func listServices(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		records, err := d.Store.ListServiceRecords()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]serviceView, 0, len(records))
		for _, r := range records {
			v := serviceView{ID: r.ID, Name: r.Name, Command: r.Command, Args: r.Args, Cwd: r.Cwd, Env: r.Env, PID: r.PID, Status: r.Status, LogFile: r.LogFile}
			if live, ok := d.Mgr.Get(r.ID); ok {
				v.PID, v.Status = live.PID, string(live.Status)
			}
			out = append(out, v)
		}
		c.JSON(http.StatusOK, out)
	}
}

func startService(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Name    string            `json:"name"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Cwd     string            `json:"cwd"`
			Env     map[string]string `json:"env"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		id := c.Param("id")
		proc, err := d.Mgr.Start(service.StartRequest{ID: id, Name: body.Name, Command: body.Command, Args: body.Args, Cwd: body.Cwd, Env: body.Env})
		audit(d.DB(), "service.start", "service", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, proc)
	}
}

// stopService stops a running service gracefully by default (§ graceful
// stop): a SIGTERM-equivalent, then a timeout before force-kill, using the
// service's configured graceful timeout (falling back to a package
// default). Pass ?graceful=false to hard-kill immediately instead.
func stopService(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		graceful := c.Query("graceful") != "false"
		err := d.Mgr.Stop(id, graceful, 0)
		audit(d.DB(), "service.stop", "service", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func restartService(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		err := d.Mgr.Restart(id)
		audit(d.DB(), "service.restart", "service", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func serviceLogs(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		tail := c.Query("tail") == "true"
		data, err := d.Mgr.ReadLog(id, tail)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", data)
	}
}

func serviceMetrics(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		live, ok := d.Mgr.Get(id)
		if !ok || live.PID == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "service not running"})
			return
		}
		m, err := metrics.CollectProcess(live.PID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, m)
	}
}

func resultOf(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}
