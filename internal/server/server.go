package server

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	v1 "github.com/alresiainc/alresia-voltpanel/internal/api/v1"
	"github.com/alresiainc/alresia-voltpanel/internal/agent"
	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
	"github.com/gin-gonic/gin"
)

type Options struct {
	Port       int
	Bind       string
	Dev        bool
	Token      string
	// SessionSecret signs session cookies (see internal/security.SessionAuth).
	SessionSecret string
	EmbeddedFS embed.FS
	CfgDir     string
}

type Server struct {
	opt Options
	r   *gin.Engine
}

// New wires the daemon's subsystems (storage, process manager, WS hub,
// session auth) together and mounts the versioned API (internal/api/v1)
// plus the embedded-UI static fallback. Route definitions themselves live
// in internal/api/v1, not here -- this file is transport/process glue only.
func New(opt Options) (*Server, error) {
	g := gin.New()
	g.Use(gin.Logger(), gin.Recovery())
	g.SetTrustedProxies(nil)

	st, err := storage.NewStore(opt.CfgDir)
	if err != nil {
		return nil, err
	}
	mgr := agent.NewManager(st)
	session, err := security.NewSessionAuth(opt.SessionSecret)
	if err != nil {
		return nil, err
	}
	hub := ws.NewHub(opt.Token, opt.Dev, session)
	go hub.Run()

	v1.Mount(g, v1.Deps{Store: st, Mgr: mgr, Hub: hub, Session: session, Token: opt.Token, Dev: opt.Dev})

	// Public, unversioned.
	g.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	mountStaticUI(g, opt.EmbeddedFS)

	return &Server{opt: opt, r: g}, nil
}

// mountStaticUI serves the embedded Vite build (cmd/voltpanel/dist) for
// everything the API router above didn't claim, falling back to index.html
// for SPA client-side routes.
func mountStaticUI(g *gin.Engine, embedded embed.FS) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return
	}
	g.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if p == "/" || !strings.Contains(filepath.Base(p), ".") {
			file, err := sub.Open("index.html")
			if err == nil {
				defer file.Close()
				stat, _ := file.Stat()
				// fs.Sub's wrapper type doesn't always preserve io.Seeker
				// (which ServeContent requires), so fall back to a plain copy.
				if rs, ok := file.(io.ReadSeeker); ok {
					http.ServeContent(c.Writer, c.Request, "index.html", stat.ModTime(), rs)
				} else {
					c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
					_, _ = io.Copy(c.Writer, file)
				}
				return
			}
		}
		http.FileServer(http.FS(sub)).ServeHTTP(c.Writer, c.Request)
	})
}

func (s *Server) Run() error {
	addr := s.opt.Bind + ":" + strconv.Itoa(s.opt.Port)
	return s.r.Run(addr)
}
