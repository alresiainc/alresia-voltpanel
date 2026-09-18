// deployments.go implements Phase 9 (§17): DeploymentTarget CRUD, a manual
// Deploy action, deployment history, and rollback -- glue between
// internal/domain/deployment's Engine and the HTTP layer. The engine
// itself is what actually SSHes in and runs the pipeline; handlers here
// only decode requests and shape responses.
package v1

import (
	"net/http"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/deployment"
	"github.com/gin-gonic/gin"
)

func targetRepo(d Deps) *deployment.TargetRepository { return deployment.NewTargetRepository(d.DB()) }
func deploymentRepo(d Deps) *deployment.Repository   { return deployment.NewRepository(d.DB()) }

func createDeploymentTarget(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			ProjectID      string `json:"projectId"`
			ServerID       string `json:"serverId"`
			RepoURL        string `json:"repoUrl"`
			IntegrationID  string `json:"integrationId"`
			Branch         string `json:"branch"`
			DeployPath     string `json:"deployPath"`
			InstallCommand string `json:"installCommand"`
			RestartCommand string `json:"restartCommand"`
			HealthCheckURL string `json:"healthCheckUrl"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.ProjectID == "" || body.ServerID == "" || body.RepoURL == "" || body.DeployPath == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "projectId, serverId, repoUrl and deployPath are required"})
			return
		}
		t, err := targetRepo(d).Create(deployment.CreateTargetRequest{
			ProjectID: body.ProjectID, ServerID: body.ServerID, RepoURL: body.RepoURL, IntegrationID: body.IntegrationID,
			Branch: body.Branch, DeployPath: body.DeployPath, InstallCommand: body.InstallCommand,
			RestartCommand: body.RestartCommand, HealthCheckURL: body.HealthCheckURL,
		})
		audit(d.DB(), "deployment_target.create", "deployment_target", t.ID, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, t)
	}
}

func listDeploymentTargets(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Query("projectId")
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "projectId is required"})
			return
		}
		list, err := targetRepo(d).ListByProject(projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func deleteDeploymentTarget(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "delete requires confirm=true"})
			return
		}
		err := targetRepo(d).Delete(id)
		audit(d.DB(), "deployment_target.delete", "deployment_target", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// deploy runs synchronously and returns once the pipeline finishes (or
// fails) -- no job queue exists yet, matching the plan's "manual Deploy
// action" scope for this phase; a long deploy blocks the request for its
// duration, which is an acceptable, honest trade-off for this phase rather
// than building an async job system speculatively.
func deploy(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			TargetID string `json:"targetId"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.TargetID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "targetId is required"})
			return
		}
		result, err := d.DeployEngine.Deploy(c.Request.Context(), body.TargetID)
		audit(d.DB(), "deployment.deploy", "deployment", result.ID, resultOf(err))
		if err != nil {
			// The deployment itself is still recorded (status=failed) even
			// though this request reports an error -- surface both.
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "deployment": result})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func listDeployments(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Query("projectId")
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "projectId is required"})
			return
		}
		list, err := deploymentRepo(d).ListByProject(projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func deploymentLog(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		log, err := deploymentRepo(d).Log(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(log))
	}
}

// rollbackDeployment never claims to undo a migration -- the response
// always carries migrationRollbackSupported:false (§13/§18), and the UI
// must show that plainly rather than implying the rollback is complete.
func rollbackDeployment(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		result, err := d.DeployEngine.Rollback(c.Request.Context(), id)
		audit(d.DB(), "deployment.rollback", "deployment", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
