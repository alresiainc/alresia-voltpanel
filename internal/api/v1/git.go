package v1

import (
	"errors"
	"net/http"
	"strings"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/integration"
	"github.com/alresiainc/alresia-voltpanel/internal/integrations/git/github"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/gin-gonic/gin"
)

// §17 Phase 8: Git integration API. Only GitHub is implemented today
// (kind "github"); the handlers below are written so a second kind
// ("gitlab", "bitbucket") would only need a new client constructor
// alongside githubClientFor, not new routes or new storage shape.

func integrationRepo(d Deps) *integration.Repository {
	return integration.NewRepository(d.DB())
}

// gitRepoView/gitBranchView give providers.Repo/Branch (shared,
// interface-defined placeholder types with no json tags -- see the
// pattern already used by ssl.go's caInfoView) a proper camelCase wire
// format without touching that shared file.
type gitRepoView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CloneURL string `json:"cloneUrl"`
}

type gitBranchView struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
}

// githubClientFor resolves a stored integration's secret and returns a
// GitHub client authenticated with it. d.GitHubBaseURL lets tests redirect
// every call at an httptest.NewServer fake instead of the real GitHub API.
func githubClientFor(d Deps, integrationID string) (*github.Client, error) {
	i, err := integrationRepo(d).Get(integrationID)
	if err != nil {
		return nil, err
	}
	if i.Kind != "github" {
		return nil, errors.New("integration is not a github integration")
	}
	token, err := d.Secrets.Get(i.SecretRef)
	if err != nil {
		return nil, err
	}
	opts := []github.Option{}
	if d.GitHubBaseURL != "" {
		opts = append(opts, github.WithBaseURL(d.GitHubBaseURL))
	}
	return github.NewClient(string(token), opts...), nil
}

// createIntegration handles POST /api/v1/integrations: {kind, token}.
// Today only kind "github" is supported. The token is validated against
// the real API (or the test fake, via d.GitHubBaseURL) before anything is
// persisted, and only ever stored through the Secret abstraction --
// integrations.secret_ref, never integrations.token.
func createIntegration(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Kind  string `json:"kind"`
			Token string `json:"token"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.Kind != "github" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported integration kind (only \"github\" today)"})
			return
		}
		if strings.TrimSpace(body.Token) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
			return
		}

		opts := []github.Option{}
		if d.GitHubBaseURL != "" {
			opts = append(opts, github.WithBaseURL(d.GitHubBaseURL))
		}
		client := github.NewClient(body.Token, opts...)
		login, err := client.ValidateToken(c.Request.Context())
		if err != nil {
			audit(d.DB(), "integration.create", "integration", "github", "failure")
			c.JSON(http.StatusBadRequest, gin.H{"error": "token validation failed"})
			return
		}

		secretRef, err := d.Secrets.Put("integration", login, "github-pat", []byte(body.Token))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store secret"})
			return
		}
		i, err := integrationRepo(d).Create("github", login, secretRef, []string{})
		audit(d.DB(), "integration.create", "integration", i.ID, resultOf(err))
		if err != nil {
			_ = d.Secrets.Delete(secretRef)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, i)
	}
}

// listIntegrations handles GET /api/v1/integrations.
func listIntegrations(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := integrationRepo(d).List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

// deleteIntegration handles DELETE /api/v1/integrations/:id. It removes
// both the integration row and its backing secret.
func deleteIntegration(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		i, err := integrationRepo(d).Get(id)
		if err != nil {
			writeIntegrationError(c, err)
			return
		}
		err = integrationRepo(d).Delete(id)
		audit(d.DB(), "integration.delete", "integration", id, resultOf(err))
		if err != nil {
			writeIntegrationError(c, err)
			return
		}
		if i.SecretRef != "" {
			_ = d.Secrets.Delete(i.SecretRef)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// listGitRepos handles GET /api/v1/git/repos?integrationId=....
func listGitRepos(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		integrationID := c.Query("integrationId")
		if integrationID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "integrationId is required"})
			return
		}
		client, err := githubClientFor(d, integrationID)
		if err != nil {
			writeIntegrationError(c, err)
			return
		}
		repos, err := client.ListRepos(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to list repos"})
			return
		}
		out := make([]gitRepoView, 0, len(repos))
		for _, r := range repos {
			out = append(out, gitRepoView{ID: r.ID, Name: r.Name, CloneURL: r.CloneURL})
		}
		c.JSON(http.StatusOK, out)
	}
}

// gitBranches handles GET /api/v1/git/repos/:owner/:repo/branches?integrationId=....
func gitBranches(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		integrationID := c.Query("integrationId")
		if integrationID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "integrationId is required"})
			return
		}
		client, err := githubClientFor(d, integrationID)
		if err != nil {
			writeIntegrationError(c, err)
			return
		}
		repo := providers.Repo{ID: c.Param("owner") + "/" + c.Param("repo")}
		branches, err := client.Branches(c.Request.Context(), repo)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to list branches"})
			return
		}
		out := make([]gitBranchView, 0, len(branches))
		for _, b := range branches {
			out = append(out, gitBranchView{Name: b.Name, SHA: b.SHA})
		}
		c.JSON(http.StatusOK, out)
	}
}

// cloneGitRepo handles POST /api/v1/git/clone:
// {integrationId, repo: {id, name, cloneUrl}, dest}.
func cloneGitRepo(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			IntegrationID string `json:"integrationId"`
			Repo          struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				CloneURL string `json:"cloneUrl"`
			} `json:"repo"`
			Dest string `json:"dest"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.IntegrationID == "" || body.Repo.CloneURL == "" || body.Dest == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "integrationId, repo.cloneUrl and dest are required"})
			return
		}
		client, err := githubClientFor(d, body.IntegrationID)
		if err != nil {
			writeIntegrationError(c, err)
			return
		}
		repo := providers.Repo{ID: body.Repo.ID, Name: body.Repo.Name, CloneURL: body.Repo.CloneURL}
		err = client.Clone(c.Request.Context(), repo, body.Dest)
		// Audit metadata never includes the clone URL with credentials --
		// only the destination path and, on failure, an already-redacted
		// error string (never surfaced to the API response either).
		audit(d.DB(), "git.clone", "repo", body.Repo.Name, resultOf(err))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "clone failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "dest": body.Dest})
	}
}

func writeIntegrationError(c *gin.Context, err error) {
	if errors.Is(err, integration.ErrNotFound) || errors.Is(err, security.ErrSecretNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "integration not found"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}
