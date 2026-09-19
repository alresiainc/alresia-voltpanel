package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	runtimedomain "github.com/alresiainc/alresia-voltpanel/internal/domain/runtime"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/gin-gonic/gin"
)

// runtimeView/versionView are the JSON shapes returned by the runtimes
// API -- kept separate from runtimedomain.WithVersions/Version so storage
// column choices don't leak into the wire format.
type runtimeView struct {
	ID       string        `json:"id"`
	Kind     string        `json:"kind"`
	Name     string        `json:"name"`
	Versions []versionView `json:"versions"`
}

type versionView struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	InstallPath string `json:"installPath"`
	IsDefault   bool   `json:"isDefault"`
	Status      string `json:"status"`
}

func toRuntimeView(rt runtimedomain.WithVersions) runtimeView {
	v := runtimeView{ID: rt.ID, Kind: rt.Kind, Name: rt.Name, Versions: make([]versionView, 0, len(rt.Versions))}
	for _, ver := range rt.Versions {
		v.Versions = append(v.Versions, versionView{
			ID: ver.ID, Version: ver.Version, InstallPath: ver.InstallPath, IsDefault: ver.IsDefault, Status: ver.Status,
		})
	}
	return v
}

// runtimeDisplayName maps a provider Kind() to a human-readable Runtime
// name for rows this handler creates. Falls back to the kind itself
// (capitalized) for any provider this list doesn't know about, so adding a
// new RuntimeProvider never requires touching this file.
func runtimeDisplayName(kind string) string {
	switch kind {
	case "node":
		return "Node.js"
	case "php":
		return "PHP"
	default:
		if kind == "" {
			return kind
		}
		return strings.ToUpper(kind[:1]) + kind[1:]
	}
}

// detectAndPersist runs DetectInstalled for one RuntimeProvider and
// persists the result via the runtime repository. Shared by listRuntimes
// (which refreshes every registered provider) and detectRuntimeKind
// (which refreshes just one).
func detectAndPersist(ctx context.Context, repo *runtimedomain.Repository, p providers.RuntimeProvider) (runtimedomain.WithVersions, error) {
	detected, err := p.DetectInstalled(ctx)
	if err != nil {
		return runtimedomain.WithVersions{}, err
	}
	versions := make([]runtimedomain.DetectedVersion, 0, len(detected))
	for _, d := range detected {
		versions = append(versions, runtimedomain.DetectedVersion{Version: d.Version, InstallPath: d.InstallPath, IsDefault: d.IsDefault})
	}
	rt, err := repo.ReplaceDetected(ctx, p.Kind(), runtimeDisplayName(p.Kind()), versions)
	if err != nil {
		return runtimedomain.WithVersions{}, err
	}
	all, err := repo.ListRuntimes(ctx)
	if err != nil {
		return runtimedomain.WithVersions{}, err
	}
	for _, r := range all {
		if r.ID == rt.ID {
			return r, nil
		}
	}
	return runtimedomain.WithVersions{Runtime: rt}, nil
}

// listRuntimes refreshes detection for every registered RuntimeProvider,
// persists what each finds, and returns the merged runtimes+versions list
// (§17 Phase 2 acceptance criteria: "UI can list detected Node/PHP
// versions on the developer's own machine"). A provider whose detection
// fails is skipped (its previously-persisted rows still come back) rather
// than failing the whole request.
func listRuntimes(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		repo := runtimedomain.NewRepository(d.DB())
		for _, p := range d.Providers.Runtimes() {
			if _, err := detectAndPersist(c.Request.Context(), repo, p); err != nil {
				audit(d.DB(), "runtime.detect", "runtime", p.Kind(), "error")
				continue
			}
			audit(d.DB(), "runtime.detect", "runtime", p.Kind(), "ok")
		}

		runtimes, err := repo.ListRuntimes(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]runtimeView, 0, len(runtimes))
		for _, rt := range runtimes {
			out = append(out, toRuntimeView(rt))
		}
		c.JSON(http.StatusOK, out)
	}
}

// detectRuntimeKind re-runs detection for a single runtime kind and
// persists the result.
func detectRuntimeKind(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		kind := c.Param("kind")
		p, ok := d.Providers.Runtime(kind)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "no runtime provider registered for kind " + kind})
			return
		}
		repo := runtimedomain.NewRepository(d.DB())
		rt, err := detectAndPersist(c.Request.Context(), repo, p)
		audit(d.DB(), "runtime.detect", "runtime", kind, resultOf(err))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, toRuntimeView(rt))
	}
}

// setDefaultRuntimeVersion asks the RuntimeProvider to actually switch the
// system/user default (e.g. `nvm alias default`), then persists that
// choice, so the "installed" list and "default" reflect real system state
// rather than only VoltPanel's opinion of it.
func setDefaultRuntimeVersion(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		kind := c.Param("kind")
		version := c.Param("version")

		p, ok := d.Providers.Runtime(kind)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "no runtime provider registered for kind " + kind})
			return
		}

		if err := p.SetDefault(c.Request.Context(), version); err != nil {
			audit(d.DB(), "runtime.setDefault", "runtime", kind+"@"+version, "error")
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		repo := runtimedomain.NewRepository(d.DB())
		if err := repo.SetDefaultVersion(c.Request.Context(), kind, version); err != nil {
			audit(d.DB(), "runtime.setDefault", "runtime", kind+"@"+version, "error")
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		audit(d.DB(), "runtime.setDefault", "runtime", kind+"@"+version, "ok")

		runtimes, err := repo.ListRuntimes(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, rt := range runtimes {
			if rt.Kind == kind {
				c.JSON(http.StatusOK, toRuntimeView(rt))
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// installRuntimeVersion downloads and installs a new version for a
// registered RuntimeProvider (today: real nodejs.org binaries for
// "node" -- see internal/providers/runtime/node). Progress streams over
// the same WS "log" event every job/service log already uses, keyed by
// "runtime:<kind>:<version>", since a real download+extract can take
// more than an instant even though (unlike a Homebrew from-source build)
// it's normally seconds, not minutes -- not worth the full async
// internal/domain/job machinery, but still worth live feedback.
func installRuntimeVersion(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		kind := c.Param("kind")
		var body struct {
			Version string `json:"version"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.Version == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "version is required"})
			return
		}

		p, ok := d.Providers.Runtime(kind)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "no runtime provider registered for kind " + kind})
			return
		}

		progressID := "runtime:" + kind + ":" + body.Version
		progress := func(percent int, message string) {
			if d.Hub == nil {
				return
			}
			payload, _ := json.Marshal(map[string]any{"type": "log", "id": progressID, "data": message + "\n", "ts": time.Now().UnixMilli()})
			d.Hub.Emit(payload)
		}

		err := p.Install(c.Request.Context(), body.Version, progress)
		audit(d.DB(), "runtime.install", "runtime", kind+"@"+body.Version, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}

		repo := runtimedomain.NewRepository(d.DB())
		rt, err := detectAndPersist(c.Request.Context(), repo, p)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, toRuntimeView(rt))
	}
}

// removeRuntimeVersion uninstalls a version -- requires confirm=true
// (§9.6), the same pattern every other destructive action in this API
// follows.
func removeRuntimeVersion(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		kind := c.Param("kind")
		version := c.Param("version")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "remove requires confirm=true"})
			return
		}

		p, ok := d.Providers.Runtime(kind)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "no runtime provider registered for kind " + kind})
			return
		}

		err := p.Remove(c.Request.Context(), version)
		audit(d.DB(), "runtime.remove", "runtime", kind+"@"+version, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		repo := runtimedomain.NewRepository(d.DB())
		rt, err := detectAndPersist(c.Request.Context(), repo, p)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, toRuntimeView(rt))
	}
}
