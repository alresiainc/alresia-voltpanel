package v1

import (
	"io"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

func listFiles(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := d.Store.ListPath(c.Query("path"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

// readFile returns a file's raw content as plain text -- the read-side
// counterpart to writeFile, so the file manager's editor can load what's
// actually on disk instead of starting from a blank textarea.
func readFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		b, err := d.Store.ReadFile(c.Query("path"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", b)
	}
}

// downloadFile serves a file as an attachment (rather than inline text)
// so a browser saves it instead of trying to render it.
func downloadFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Query("path")
		b, err := d.Store.ReadFile(p)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Disposition", "attachment; filename=\""+filepath.Base(p)+"\"")
		c.Data(http.StatusOK, "application/octet-stream", b)
	}
}

func writeFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct{ Path, Content string }
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := d.Store.WriteFile(req.Path, []byte(req.Content))
		audit(d.DB(), "file.write", "file", req.Path, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// deleteFile requires an explicit confirm:true field (§9.6) so a stray
// script, extension, or malformed request can't silently delete something
// just by getting the path right.
func deleteFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Query("path")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "delete requires confirm=true"})
			return
		}
		err := d.Store.DeletePath(p)
		audit(d.DB(), "file.delete", "file", p, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func mkdirFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Path string `json:"path"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := d.Store.Mkdir(req.Path)
		audit(d.DB(), "file.mkdir", "file", req.Path, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func moveFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Src string `json:"src"`
			Dst string `json:"dst"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := d.Store.Move(req.Src, req.Dst)
		audit(d.DB(), "file.move", "file", req.Src+" -> "+req.Dst, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func copyFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Src string `json:"src"`
			Dst string `json:"dst"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := d.Store.Copy(req.Src, req.Dst)
		audit(d.DB(), "file.copy", "file", req.Src+" -> "+req.Dst, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func uploadFile(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Query("path")
		if p == "" {
			p = c.PostForm("path")
		}
		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		f, err := file.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		err = d.Store.WriteFile(p, b)
		audit(d.DB(), "file.upload", "file", p, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
