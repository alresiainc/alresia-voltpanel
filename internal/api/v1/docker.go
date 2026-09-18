package v1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/providers/docker"
	"github.com/gin-gonic/gin"
)

// dockerUnavailable writes a clean, structured "Docker not available"
// response. Docker may simply not be installed or running wherever this
// daemon runs, so every handler below reports that as a normal 503, never
// a 500 or a panic.
func dockerUnavailable(c *gin.Context, err error) {
	msg := "Docker not available"
	if err != nil {
		msg = "Docker not available: " + err.Error()
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": msg, "available": false})
}

// requireDocker checks that d.Docker is set and a daemon actually responds,
// writing the standard dockerUnavailable response and returning false if
// not. Handlers call this first thing and return immediately on false.
func requireDocker(c *gin.Context, d Deps) (*docker.Client, bool) {
	if d.Docker == nil {
		dockerUnavailable(c, nil)
		return nil, false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	if err := d.Docker.Available(ctx); err != nil {
		dockerUnavailable(c, err)
		return nil, false
	}
	return d.Docker, true
}

// dockerErrorStatus maps a docker-provider error to an HTTP status. A
// mid-request failure (daemon went away between the availability probe and
// the actual call) still gets the clean unavailable response instead of a
// 500; anything else (e.g. "no such container") is a 404/400-shaped client
// error.
func writeDockerError(c *gin.Context, err error) {
	if docker.IsUnavailable(err) {
		dockerUnavailable(c, err)
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func listDockerContainers(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		containers, err := cl.ListContainers(c.Request.Context())
		if err != nil {
			writeDockerError(c, err)
			return
		}
		if c.Query("group") == "compose" {
			c.JSON(http.StatusOK, docker.GroupByComposeProject(containers))
			return
		}
		c.JSON(http.StatusOK, containers)
	}
}

func getDockerContainer(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		detail, err := cl.InspectContainer(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeDockerError(c, err)
			return
		}
		c.JSON(http.StatusOK, detail)
	}
}

func startDockerContainer(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		id := c.Param("id")
		err := cl.StartContainer(c.Request.Context(), id)
		audit(d.DB(), "docker.container.start", "container", id, resultOf(err))
		if err != nil {
			writeDockerError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func stopDockerContainer(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		id := c.Param("id")
		err := cl.StopContainer(c.Request.Context(), id)
		audit(d.DB(), "docker.container.stop", "container", id, resultOf(err))
		if err != nil {
			writeDockerError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func restartDockerContainer(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		id := c.Param("id")
		err := cl.RestartContainer(c.Request.Context(), id)
		audit(d.DB(), "docker.container.restart", "container", id, resultOf(err))
		if err != nil {
			writeDockerError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func dockerContainerLogs(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		tail := 200
		if q := c.Query("tail"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 {
				tail = n
			}
		}
		out, err := cl.ContainerLogs(c.Request.Context(), c.Param("id"), tail)
		if err != nil {
			writeDockerError(c, err)
			return
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", out)
	}
}

func listDockerImages(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		images, err := cl.ListImages(c.Request.Context())
		if err != nil {
			writeDockerError(c, err)
			return
		}
		c.JSON(http.StatusOK, images)
	}
}

func listDockerVolumes(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		volumes, err := cl.ListVolumes(c.Request.Context())
		if err != nil {
			writeDockerError(c, err)
			return
		}
		c.JSON(http.StatusOK, volumes)
	}
}

func listDockerNetworks(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		networks, err := cl.ListNetworks(c.Request.Context())
		if err != nil {
			writeDockerError(c, err)
			return
		}
		c.JSON(http.StatusOK, networks)
	}
}

func execDockerContainer(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl, ok := requireDocker(c, d)
		if !ok {
			return
		}
		var body struct {
			Cmd []string `json:"cmd"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		id := c.Param("id")
		// Exec can legitimately take a while; give it more room than the
		// 3s availability probe without hanging the request forever.
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		result, err := cl.Exec(ctx, id, body.Cmd)
		audit(d.DB(), "docker.container.exec", "container", id, resultOf(err))
		if err != nil {
			writeDockerError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
