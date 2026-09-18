// packages.go implements GET/POST /api/v1/packages/* and /api/v1/jobs/*
// (the Homebrew-backed "install fresh PHP/MySQL/PostgreSQL/Redis, manage
// multiple versions, upgrade Apache/Nginx" feature). Handlers here just
// validate input and call into internal/providers/pkgmanager/brew and
// internal/domain/job -- see those packages for why installs run as
// async jobs rather than blocking the request.
package v1

import (
	"net/http"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/job"
	"github.com/gin-gonic/gin"
)

// packagesUnavailable is what every handler below returns when d.Packages
// is nil (non-macOS today) or Homebrew itself isn't on PATH -- matching
// docker.go's "probe availability per request, report a clean error"
// pattern rather than assuming a non-nil provider means a working one.
func packagesUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Homebrew isn't available on this machine -- install it from https://brew.sh, then retry"})
}

func listInstalledPackages(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Packages == nil || !d.Packages.Available() {
			packagesUnavailable(c)
			return
		}
		list, err := d.Packages.ListInstalled(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func searchPackages(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Packages == nil || !d.Packages.Available() {
			packagesUnavailable(c)
			return
		}
		q := c.Query("q")
		if q == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "q must not be empty"})
			return
		}
		results, err := d.Packages.Search(c.Request.Context(), q)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, results)
	}
}

func installPackage(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Packages == nil || !d.Packages.Available() {
			packagesUnavailable(c)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		j, err := d.Packages.Install(body.Name)
		audit(d.DB(), "package.install", "package", body.Name, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, j)
	}
}

func upgradePackage(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Packages == nil || !d.Packages.Available() {
			packagesUnavailable(c)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		j, err := d.Packages.Upgrade(body.Name)
		audit(d.DB(), "package.upgrade", "package", body.Name, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, j)
	}
}

// uninstallPackage requires an explicit confirm:true query param (§9.6),
// the same pattern every other destructive action in this API follows.
func uninstallPackage(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Packages == nil || !d.Packages.Available() {
			packagesUnavailable(c)
			return
		}
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "uninstall requires confirm=true"})
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		j, err := d.Packages.Uninstall(body.Name)
		audit(d.DB(), "package.uninstall", "package", body.Name, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, j)
	}
}

func packageServiceAction(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Packages == nil || !d.Packages.Available() {
			packagesUnavailable(c)
			return
		}
		var body struct {
			Name   string `json:"name"`
			Action string `json:"action"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := d.Packages.ServiceAction(c.Request.Context(), body.Name, body.Action)
		audit(d.DB(), "package.service."+body.Action, "package", body.Name, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// setDefaultPackageVersion links a specific installed version of a
// versioned formula family (e.g. "php@8.3") as the one on PATH -- how
// switching PHP/Node/etc. versions actually takes effect for anything
// that shells out to the bare command name.
func setDefaultPackageVersion(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Packages == nil || !d.Packages.Available() {
			packagesUnavailable(c)
			return
		}
		var body struct {
			Target string `json:"target"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := d.Packages.SetDefaultVersion(c.Request.Context(), body.Target)
		audit(d.DB(), "package.setDefault", "package", body.Target, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func listJobs(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Jobs == nil {
			c.JSON(http.StatusOK, []job.Job{})
			return
		}
		list, err := d.Jobs.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func getJob(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d.Jobs == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
			return
		}
		j, err := d.Jobs.Get(c.Param("id"))
		if err != nil {
			if err == job.ErrNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, j)
	}
}
