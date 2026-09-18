package v1

import (
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
	"github.com/gin-gonic/gin"
)

// Mount registers every /api/v1/* route on g. Called once from
// internal/server.New; kept separate from that package so route wiring
// (this file) stays decoupled from process/embed/static-file concerns.
func Mount(g *gin.Engine, d Deps) {
	g.GET("/api/v1/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// Auth for /api/v1/ws happens inside ws.ServeWs (cookie at handshake,
	// or first-message frame as fallback) -- browsers can't set custom
	// headers on a WS handshake, so the header-based middleware below
	// can't apply to this route.
	g.GET("/api/v1/ws", func(c *gin.Context) { ws.ServeWs(d.Hub, c.Writer, c.Request) })

	api := g.Group("/api/v1")
	api.Use(csrfMiddleware)

	api.POST("/auth/token/verify", verifyToken(d))

	authed := api.Group("/")
	authed.Use(authMiddleware(d))

	authed.GET("/runtimes", listRuntimes(d))
	authed.POST("/runtimes/:kind/detect", detectRuntimeKind(d))
	authed.POST("/runtimes/:kind/versions/:version/default", setDefaultRuntimeVersion(d))

	authed.GET("/services", listServices(d))
	authed.POST("/services/:id/start", startService(d))
	authed.POST("/services/:id/stop", stopService(d))
	authed.POST("/services/:id/restart", restartService(d))
	authed.GET("/services/:id/logs", serviceLogs(d))
	authed.GET("/services/:id/metrics", serviceMetrics(d))

	authed.GET("/files", listFiles(d))
	authed.PUT("/files", writeFile(d))
	authed.DELETE("/files", deleteFile(d))
	authed.POST("/files/mkdir", mkdirFile(d))
	authed.POST("/files/move", moveFile(d))
	authed.POST("/files/copy", copyFile(d))
	authed.POST("/files/upload", uploadFile(d))

	authed.GET("/system/metrics", systemMetrics)

	authed.GET("/projects", listProjects(d))
	authed.POST("/projects", createProject(d))
	authed.GET("/projects/:id", getProject(d))
	authed.DELETE("/projects/:id", deleteProject(d))
	authed.POST("/projects/:id/detect", redetectProject(d))

	authed.GET("/docker/containers", listDockerContainers(d))
	authed.GET("/docker/containers/:id", getDockerContainer(d))
	authed.POST("/docker/containers/:id/start", startDockerContainer(d))
	authed.POST("/docker/containers/:id/stop", stopDockerContainer(d))
	authed.POST("/docker/containers/:id/restart", restartDockerContainer(d))
	authed.GET("/docker/containers/:id/logs", dockerContainerLogs(d))
	authed.POST("/docker/containers/:id/exec", execDockerContainer(d))
	authed.GET("/docker/images", listDockerImages(d))
	authed.GET("/docker/volumes", listDockerVolumes(d))
	authed.GET("/docker/networks", listDockerNetworks(d))

	authed.GET("/domains", listDomains(d))
	authed.POST("/domains", createDomain(d))
	authed.DELETE("/domains/:id", deleteDomain(d))
	authed.GET("/domains/:id/conflicts", domainConflicts(d))

	authed.POST("/ssl/ca/ensure", ensureCA(d))
	authed.POST("/ssl/certificates", issueCertificate(d))
	authed.POST("/ssl/ca/trust", trustCA(d))

	authed.GET("/extensions", listExtensions(d))
	authed.POST("/extensions", installExtension(d))
	authed.DELETE("/extensions/:id", removeExtension(d))
	authed.POST("/extensions/:id/enable", enableExtension(d))
	authed.POST("/extensions/:id/disable", disableExtension(d))
}
