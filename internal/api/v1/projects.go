package v1

import (
	"errors"
	"net/http"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/project"
	"github.com/gin-gonic/gin"
)

func projectRepo(d Deps) *project.Repository {
	return project.NewRepository(d.DB())
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
		err := projectRepo(d).Delete(id)
		audit(d.DB(), "project.delete", "project", id, resultOf(err))
		if err != nil {
			writeProjectError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
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

func writeProjectError(c *gin.Context, err error) {
	if errors.Is(err, project.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}
