package server

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	v1 "github.com/alresiainc/alresia-voltpanel/internal/api/v1"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/deployment"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/extension"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/integration"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/server"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/service"
	"github.com/alresiainc/alresia-voltpanel/internal/pipeline"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/docker"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/domainprovider/hosts"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/remote/ssh"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/runtime/node"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/runtime/php"
	"github.com/alresiainc/alresia-voltpanel/internal/providers/ssl/localca"
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
	opt        Options
	r          *gin.Engine
	mgr        *service.Manager
	http       *http.Server
	extensions *extension.Repository
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
	session, err := security.NewSessionAuth(opt.SessionSecret)
	if err != nil {
		return nil, err
	}
	hub := ws.NewHub(opt.Token, opt.Dev, session)
	go hub.Run()

	mgr := service.NewManager(st, hub)

	registry := providers.NewRegistry()
	registry.RegisterRuntime(node.New())
	registry.RegisterRuntime(php.New())

	// Construction never fails except on platforms with no supported
	// transport (Windows, today) -- Docker not being installed/running
	// here is expected and handled per-request by the docker.go handlers,
	// not treated as a startup error.
	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Printf("volt: docker provider unavailable: %v", err)
	}

	domainProvider := hosts.New()
	sslProvider := localca.New(opt.CfgDir)

	extensions := extension.NewRepository(st.DB(), registry)
	// Re-registers every previously-enabled extension's subprocess into
	// registry on startup -- an enabled extension survives a daemon
	// restart, exactly like a built-in provider always being there.
	extensions.LoadEnabled(context.Background())

	secrets, err := security.NewSecretStore(st.DB(), opt.CfgDir)
	if err != nil {
		return nil, err
	}
	sshProvider := &ssh.Provider{ResolveKey: secrets.Get}

	deployEngine := &deployment.Engine{
		Remote:        sshProvider,
		Servers:       server.NewRepository(st.DB()),
		Targets:       deployment.NewTargetRepository(st.DB()),
		Deployments:   deployment.NewRepository(st.DB()),
		Integrations:  integration.NewRepository(st.DB()),
		ResolveSecret: secrets.Get,
		LogDir:        filepath.Join(opt.CfgDir, "logs", "deployments"),
	}

	pipelineEngine := &pipeline.Engine{
		Remote:       sshProvider,
		Servers:      server.NewRepository(st.DB()),
		DeployEngine: deployEngine,
	}

	v1.Mount(g, v1.Deps{Store: st, Mgr: mgr, Hub: hub, Session: session, Token: opt.Token, Dev: opt.Dev, Providers: registry, Docker: dockerClient, Domains: domainProvider, SSL: sslProvider, Extensions: extensions, Secrets: secrets, Remote: sshProvider, DeployEngine: deployEngine, PipelineEngine: pipelineEngine})

	// Public, unversioned.
	g.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	mountStaticUI(g, opt.EmbeddedFS)

	return &Server{opt: opt, r: g, mgr: mgr, extensions: extensions}, nil
}

// Close releases resources that outlive a single request but must stop
// when the daemon does -- today, that's stopping every enabled
// extension's subprocess (§17 Phase 11).
func (s *Server) Close() {
	if s.extensions != nil {
		s.extensions.CloseAll()
	}
}

// StartAutostartServices runs the Phase 5 dependency-ordered autostart pass
// (§15) over every service persisted with autostart=true. Intended to be
// called once by cmd/voltpanel/main.go after New() and before Run(), so the
// daemon's own service registration (internal/platform/*) has nothing to do
// with whether *managed* services autostart -- that's this, independent of
// how the Volt daemon itself was launched.
func (s *Server) StartAutostartServices(ctx context.Context) error {
	return s.mgr.StartAutostart(ctx)
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

// Run serves until Shutdown is called (typically from a signal handler in
// main), then returns cleanly instead of blocking forever like gin's own
// Engine.Run -- this is what lets `volt stop` (§12) actually work.
func (s *Server) Run() error {
	addr := s.opt.Bind + ":" + strconv.Itoa(s.opt.Port)
	s.http = &http.Server{Addr: addr, Handler: s.r}
	err := s.http.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully stops the HTTP server, waiting up to the given
// context's deadline for in-flight requests to finish.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}
