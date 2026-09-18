package v1

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/domainname"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/project"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/service"
	"github.com/gin-gonic/gin"
)

func projectRepo(d Deps) *project.Repository {
	return project.NewRepository(d.DB())
}

// projectServiceID is the deterministic Service id "Start Project" uses,
// so starting the same project twice always targets the same managed
// process rather than accumulating duplicates.
func projectServiceID(projectID string) string {
	return "project:" + projectID
}

func listProjects(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := projectRepo(d).List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func createProject(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		p, err := projectRepo(d).Create(body.Name, body.Path)
		audit(d.DB(), "project.create", "project", body.Path, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, p)
	}
}

func getProject(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		p, err := projectRepo(d).Get(id)
		if err != nil {
			writeProjectError(c, err)
			return
		}
		c.JSON(http.StatusOK, p)
	}
}

// deleteProject requires an explicit confirm:true field (§9.6), the same
// pattern files.go's deleteFile uses -- checked server-side, not just a UI
// checkbox.
func deleteProject(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "delete requires confirm=true"})
			return
		}

		// A project that was ever started (startProject) has a persisted
		// Service row referencing it via a foreign key with no cascade;
		// deleting the project out from under it would otherwise fail at
		// the DB level with an opaque constraint error. Stop it if still
		// running, then drop the record -- deleting a project has never
		// implied "leave its process running forever, orphaned."
		svcID := projectServiceID(id)
		if _, running := d.Mgr.Get(svcID); running {
			_ = d.Mgr.Stop(svcID, true, 0)
		}
		_ = d.Store.DeleteServiceRecordsForProject(id)

		// Any domain bound to this project loses that link (and its live
		// proxy route) but survives as a plain hostname mapping -- deleting
		// a project shouldn't silently delete a hosts-file entry and
		// certificate someone may still want.
		domainRepo := domainname.NewRepository(d.DB())
		if bound, err := domainRepo.ListDomainsForProject(id); err == nil {
			for _, dom := range bound {
				if d.Proxy != nil {
					d.Proxy.Remove(dom.Hostname)
				}
				_ = domainRepo.ClearProject(dom.ID)
			}
		}

		err := projectRepo(d).Delete(id)
		audit(d.DB(), "project.delete", "project", id, resultOf(err))
		if err != nil {
			if isForeignKeyErr(err) {
				c.JSON(http.StatusConflict, gin.H{"error": "can't delete: this project still has deployment targets or deployment history attached -- remove those first"})
				return
			}
			writeProjectError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func isForeignKeyErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// redetectProject re-runs the framework-detection engine against a
// project's stored path -- useful once dependencies changed since it was
// added (e.g. `next` got installed after the fact).
func redetectProject(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		p, err := projectRepo(d).Redetect(id)
		audit(d.DB(), "project.detect", "project", id, resultOf(err))
		if err != nil {
			writeProjectError(c, err)
			return
		}
		c.JSON(http.StatusOK, p)
	}
}

// startProject runs a project's stored RunCommand as a managed Service
// (reusing internal/domain/service.Manager wholesale -- crash restart,
// logs, WS log streaming all come for free) and, if any domain is bound
// to this project, points the reverse proxy (internal/proxy) at whichever
// port it ends up listening on. That port is chosen here, not by the
// project's own command, and handed to the process the two ways a local
// dev server is realistically told its port: a PORT env var (Node/Express/
// CRA/Next custom servers, etc. all read this) and, for Laravel
// specifically (the one framework whose default RunCommand -- "php
// artisan serve" -- takes an explicit flag instead), an appended
// --host/--port pair.
func startProject(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		p, err := projectRepo(d).Get(id)
		if err != nil {
			writeProjectError(c, err)
			return
		}
		if strings.TrimSpace(p.RunCommand) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project has no run command detected -- set one by re-detecting or starting it as a plain process from Processes"})
			return
		}

		svcID := projectServiceID(p.ID)
		if _, running := d.Mgr.Get(svcID); running {
			c.JSON(http.StatusConflict, gin.H{"error": "project is already running"})
			return
		}

		port, err := allocateFreePort()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to allocate a local port: " + err.Error()})
			return
		}

		fields := strings.Fields(p.RunCommand)
		command, args := fields[0], append([]string{}, fields[1:]...)
		if p.DetectedKind == "laravel" {
			args = append(args, "--host=127.0.0.1", fmt.Sprintf("--port=%d", port))
		}

		proc, err := d.Mgr.Start(service.StartRequest{
			ID:            svcID,
			Name:          p.Name,
			Command:       command,
			Args:          args,
			Cwd:           p.Path,
			Env:           map[string]string{"PORT": strconv.Itoa(port), "HOST": "127.0.0.1"},
			ProjectID:     p.ID,
			Kind:          "project",
			RestartPolicy: service.RestartOnFailure,
		})
		audit(d.DB(), "project.start", "project", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		bound := syncProjectProxyRoutes(d, p.ID, "127.0.0.1:"+strconv.Itoa(port))

		c.JSON(http.StatusOK, gin.H{"service": proc, "port": port, "boundDomains": bound})
	}
}

// stopProject stops the managed Service startProject creates and removes
// any reverse-proxy routes pointing at it, so a bound domain fails clearly
// (404 from the proxy) rather than silently forwarding to a dead port.
func stopProject(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "stop requires confirm=true"})
			return
		}
		svcID := projectServiceID(id)
		err := d.Mgr.Stop(svcID, true, 0)
		audit(d.DB(), "project.stop", "project", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		unbound := syncProjectProxyRoutes(d, id, "")
		c.JSON(http.StatusOK, gin.H{"ok": true, "unboundDomains": unbound})
	}
}

// syncProjectProxyRoutes points every domain bound to projectID at addr
// (host:port), or -- when addr is "" -- removes them from the proxy
// instead. Persists the live port onto each domain row (best-effort; a
// failure to persist doesn't prevent the proxy route itself from working)
// so the UI can show where a domain is really pointing. Returns the
// hostnames it touched.
func syncProjectProxyRoutes(d Deps, projectID, addr string) []string {
	if d.Proxy == nil {
		return nil
	}
	repo := domainname.NewRepository(d.DB())
	domains, err := repo.ListDomainsForProject(projectID)
	if err != nil {
		return nil
	}
	touched := make([]string, 0, len(domains))
	for _, dom := range domains {
		if !dom.Enabled {
			continue
		}
		if addr == "" {
			d.Proxy.Remove(dom.Hostname)
		} else {
			d.Proxy.Set(dom.Hostname, addr)
			port := 0
			if idx := strings.LastIndexByte(addr, ':'); idx >= 0 {
				port, _ = strconv.Atoi(addr[idx+1:])
			}
			_ = repo.SetPort(dom.ID, port)
		}
		touched = append(touched, dom.Hostname)
	}
	return touched
}

// allocateFreePort asks the OS for an ephemeral port by binding to :0 and
// immediately releasing it -- standard practice for "give me a free local
// port"; the tiny window between closing this listener and the child
// process binding the same port is the same one every tool using this
// technique accepts.
func allocateFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func writeProjectError(c *gin.Context, err error) {
	if errors.Is(err, project.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}
