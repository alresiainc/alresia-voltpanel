// pipelines.go implements Phase 10 (§17): pipeline CRUD, a manual run
// trigger, run history, and a webhook trigger endpoint that verifies an
// HMAC-SHA256 signature (the same scheme GitHub/GitLab/Bitbucket webhooks
// use) before running anything -- a pipeline can contain a "run" step
// (arbitrary local shell command), so an unverified webhook would be
// unauthenticated remote code execution. There is no weaker path: every
// trigger, webhook or manual, goes through runPipeline below.
package v1

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	pipelinedomain "github.com/alresiainc/alresia-voltpanel/internal/domain/pipeline"
	"github.com/alresiainc/alresia-voltpanel/internal/pipeline"
	"github.com/gin-gonic/gin"
)

func pipelineRepo(d Deps) *pipelinedomain.Repository { return pipelinedomain.NewRepository(d.DB()) }

func createPipeline(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			ProjectID      string `json:"projectId"`
			Name           string `json:"name"`
			DefinitionYAML string `json:"definitionYaml"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.ProjectID == "" || body.Name == "" || body.DefinitionYAML == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "projectId, name and definitionYaml are required"})
			return
		}
		if _, err := pipeline.Parse(body.DefinitionYAML); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pipeline definition: " + err.Error()})
			return
		}
		p, err := pipelineRepo(d).Create(body.ProjectID, body.Name, body.DefinitionYAML)
		audit(d.DB(), "pipeline.create", "pipeline", p.ID, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// The webhook secret is shown exactly once, here, so the operator
		// can configure it on the git host's webhook settings -- every
		// other response (List/Get) omits it via json:"-".
		c.JSON(http.StatusCreated, gin.H{
			"id": p.ID, "projectId": p.ProjectID, "name": p.Name, "definitionYaml": p.DefinitionYAML,
			"webhookSecret": p.WebhookSecret, "webhookPath": "/api/v1/pipelines/" + p.ID + "/webhook",
			"createdAt": p.CreatedAt, "updatedAt": p.UpdatedAt,
		})
	}
}

func listPipelines(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Query("projectId")
		if projectID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "projectId is required"})
			return
		}
		list, err := pipelineRepo(d).ListByProject(projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func deletePipeline(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if c.Query("confirm") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "delete requires confirm=true"})
			return
		}
		err := pipelineRepo(d).Delete(id)
		audit(d.DB(), "pipeline.delete", "pipeline", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func listPipelineRuns(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		runs, err := pipelineRepo(d).ListRuns(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, runs)
	}
}

func runPipelineManual(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		run, err := runPipeline(c.Request.Context(), d, c.Param("id"), "manual")
		audit(d.DB(), "pipeline.run", "pipeline", c.Param("id"), resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "run": run})
			return
		}
		c.JSON(http.StatusOK, run)
	}
}

// pipelineWebhook verifies X-Volt-Signature: sha256=<hex hmac of the raw
// body, keyed by the pipeline's own webhook secret> before running
// anything. This mirrors GitHub's X-Hub-Signature-256 convention closely
// enough that a GitHub webhook could point at this path directly, but the
// header name is Volt's own since we're not claiming GitHub-specific
// payload parsing here -- any trigger source that can compute the same
// HMAC can use this endpoint.
func pipelineWebhook(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		p, err := pipelineRepo(d).Get(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "pipeline not found"})
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		sig := c.GetHeader("X-Volt-Signature")
		if !verifyWebhookSignature(p.WebhookSecret, body, sig) {
			audit(d.DB(), "pipeline.webhook", "pipeline", id, "error")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
			return
		}
		run, err := runPipeline(c.Request.Context(), d, id, "webhook")
		audit(d.DB(), "pipeline.webhook", "pipeline", id, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "run": run})
			return
		}
		c.JSON(http.StatusOK, run)
	}
}

func verifyWebhookSignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if secret == "" || !strings.HasPrefix(header, prefix) {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	got := strings.TrimPrefix(header, prefix)
	return hmac.Equal([]byte(expected), []byte(got))
}

func runPipeline(ctx context.Context, d Deps, pipelineID, triggerKind string) (pipelinedomain.Run, error) {
	p, err := pipelineRepo(d).Get(pipelineID)
	if err != nil {
		return pipelinedomain.Run{}, err
	}
	def, err := pipeline.Parse(p.DefinitionYAML)
	if err != nil {
		return pipelinedomain.Run{}, err
	}
	run, err := pipelineRepo(d).CreateRun(pipelineID, triggerKind)
	if err != nil {
		return pipelinedomain.Run{}, err
	}

	results, runErr := d.PipelineEngine.Run(ctx, def)
	steps := make([]map[string]any, 0, len(results))
	for _, r := range results {
		steps = append(steps, map[string]any{"name": r.Name, "kind": r.Kind, "status": r.Status, "output": r.Output, "durationMs": r.DurationMs})
	}
	status := "success"
	if runErr != nil {
		status = "failed"
	}
	if err := pipelineRepo(d).FinishRun(run.ID, status, steps); err != nil {
		return pipelinedomain.Run{}, err
	}
	run.Status = status
	run.Steps = steps
	return run, runErr
}
