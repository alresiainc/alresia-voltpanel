package server

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alresia/voltpanel/internal/agent"
	"github.com/alresia/voltpanel/internal/metrics"
	"github.com/alresia/voltpanel/internal/storage"
	"github.com/alresia/voltpanel/internal/ws"
	"github.com/gin-gonic/gin"
)

type Options struct {
	Port       int
	Bind       string
	Dev        bool
	Token      string
	EmbeddedFS embed.FS
	CfgDir     string
}

type Server struct {
	opt   Options
	r     *gin.Engine
	hub   *ws.Hub
	mgr   *agent.Manager
	store *storage.Store
}

func New(opt Options) (*Server, error) {
	g := gin.New()
	g.Use(gin.Logger(), gin.Recovery())
	g.SetTrustedProxies(nil)

	// Initialize subsystems
	st := storage.NewStore(opt.CfgDir)
	mgr := agent.NewManager(st)
	hub := ws.NewHub()
	go hub.Run()

	s := &Server{opt: opt, r: g, hub: hub, mgr: mgr, store: st}

	// Public endpoints
	g.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	g.GET("/ws/events", func(c *gin.Context) {
		if !s.authorized(c.Request) && !opt.Dev {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		ws.ServeWs(hub, c.Writer, c.Request)
	})

	// Auth middleware
	auth := func(c *gin.Context) {
		if !s.authorized(c.Request) && !opt.Dev {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}

	api := g.Group("/")
	api.Use(auth)

	api.POST("/auth/token/verify", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	api.GET("/services", func(c *gin.Context) {
		apps := st.ListApps()
		c.JSON(200, apps)
	})
	api.POST("/services/start", func(c *gin.Context) {
		var req agent.StartRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		proc, err := mgr.Start(req, s.hub)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, proc)
	})
	api.POST("/services/stop", func(c *gin.Context) {
		var req struct{ ID string `json:"id"` }
		if err := c.ShouldBindJSON(&req); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		if err := mgr.Stop(req.ID); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		c.JSON(200, gin.H{"ok": true})
	})
	api.POST("/services/restart", func(c *gin.Context) {
		var req struct{ ID string `json:"id"` }
		if err := c.ShouldBindJSON(&req); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		if err := mgr.Restart(req.ID, s.hub); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		c.JSON(200, gin.H{"ok": true})
	})
	api.GET("/processes", func(c *gin.Context) { c.JSON(200, mgr.List()) })

	// Files
	api.GET("/files/list", func(c *gin.Context) {
		p := c.Query("path")
		list, err := st.ListPath(p)
		if err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		c.JSON(200, list)
	})
	api.POST("/files/write", func(c *gin.Context) {
		var req struct{ Path, Content string }
		if err := c.ShouldBindJSON(&req); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		if err := st.WriteFile(req.Path, []byte(req.Content)); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		c.JSON(200, gin.H{"ok": true})
	})
	api.DELETE("/files", func(c *gin.Context) {
		p := c.Query("path")
		if err := st.DeletePath(p); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		c.JSON(200, gin.H{"ok": true})
	})
	api.POST("/files/upload", func(c *gin.Context) {
		p := c.Query("path")
		if p == "" { p = c.PostForm("path") }
		file, err := c.FormFile("file")
		if err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		f, err := file.Open()
		if err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil { c.JSON(500, gin.H{"error": err.Error()}); return }
		if err := st.WriteFile(p, b); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		c.JSON(200, gin.H{"ok": true})
	})

	// Logs
	api.GET("/logs/:id", func(c *gin.Context) {
		id := c.Param("id")
		tail := c.Query("tail") == "true"
		data, err := mgr.ReadLog(id, tail)
		if err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
		c.Data(200, "text/plain; charset=utf-8", data)
	})

	// Metrics
	api.GET("/metrics", func(c *gin.Context) {
		m, err := metrics.Collect()
		if err != nil { c.JSON(500, gin.H{"error": err.Error()}); return }
		c.JSON(200, m)
	})

	// Static UI from embedded FS under cmd/voltpanel/dist
	sub, err := fs.Sub(opt.EmbeddedFS, "dist")
	if err == nil {
		g.NoRoute(func(c *gin.Context) {
			p := c.Request.URL.Path
			if p == "/" || !strings.Contains(filepath.Base(p), ".") {
				// serve index.html for SPA routes
				file, err := sub.Open("index.html")
				if err == nil {
					stat, _ := file.Stat()
					http.ServeContent(c.Writer, c.Request, "index.html", stat.ModTime(), file)
					return
				}
			}
			http.FileServer(http.FS(sub)).ServeHTTP(c.Writer, c.Request)
		})
	}

	return s, nil
}

func (s *Server) authorized(r *http.Request) bool {
	if s.opt.Dev { // allow from localhost in dev
		return true
	}
	t := r.Header.Get("X-Volt-Token")
	if t == "" {
		t = r.Header.Get("x-volt-token")
	}
	return t != "" && t == s.opt.Token
}

func (s *Server) Run() error {
	addr := s.opt.Bind + ":" + strconv.Itoa(s.opt.Port)
	return s.r.Run(addr)
}
