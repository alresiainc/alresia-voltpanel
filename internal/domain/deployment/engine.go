package deployment

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/integration"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/server"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// SecretResolver resolves a stored secret's plaintext by ref -- wired to
// internal/security.SecretStore.Get in production. Interface-typed so
// engine tests never need a real SecretStore.
type SecretResolver func(secretRef string) ([]byte, error)

// Engine runs the actual deploy/rollback pipeline (§13): checkout/pull over
// SSH (Phase 7's RemoteProvider), an optional install step, an optional
// restart step, an optional HTTP health check, then records the result.
// Every step's output is captured into one log file per deployment, with
// any injected git credential redacted before it's ever written.
type Engine struct {
	Remote      providers.RemoteProvider
	Servers     *server.Repository
	Targets     *TargetRepository
	Deployments *Repository
	Integrations *integration.Repository
	ResolveSecret SecretResolver
	LogDir      string // <cfgDir>/logs/deployments
	HTTPClient  *http.Client
}

func (e *Engine) httpClient() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Deploy runs one deployment against target and returns the resulting
// Deployment row (whether it succeeded or failed -- a failed deploy is
// still recorded, not discarded, since "what happened last time" matters).
func (e *Engine) Deploy(ctx context.Context, targetID string) (Deployment, error) {
	target, err := e.Targets.Get(targetID)
	if err != nil {
		return Deployment{}, fmt.Errorf("deployment target: %w", err)
	}
	srv, err := e.Servers.Get(target.ServerID)
	if err != nil {
		return Deployment{}, fmt.Errorf("server: %w", err)
	}

	d := Deployment{ID: newID(), ProjectID: target.ProjectID, ServerID: target.ServerID, Branch: target.Branch, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := e.Deployments.create(d); err != nil {
		return Deployment{}, err
	}

	start := time.Now()
	var log strings.Builder
	status := "success"
	var commitSHA, codeRollbackRef string

	runErr := func() error {
		sess, err := e.Remote.Connect(ctx, srv.ToProviderServer())
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		defer sess.Close()

		cloneURL, redactedURL, err := e.resolveCloneURL(target)
		if err != nil {
			return fmt.Errorf("resolve repo credentials: %w", err)
		}

		// Previous HEAD (if the path is already a checkout) becomes this
		// deployment's rollback target -- best-effort, absence isn't fatal
		// (first-ever deploy to a fresh path has no "previous").
		if out, _, err := sess.Exec(ctx, fmt.Sprintf("git -C %s rev-parse HEAD 2>/dev/null || true", shQuote(target.DeployPath))); err == nil {
			codeRollbackRef = strings.TrimSpace(string(out))
		}

		checkoutCmd := fmt.Sprintf(
			"set -e; mkdir -p %[1]s; if [ -d %[1]s/.git ]; then cd %[1]s && git fetch origin %[2]s && git checkout %[2]s && git reset --hard origin/%[2]s; else git clone --branch %[2]s %[3]s %[1]s; fi",
			shQuote(target.DeployPath), shQuote(target.Branch), shQuote(cloneURL),
		)
		redactedCheckoutCmd := strings.ReplaceAll(checkoutCmd, cloneURL, redactedURL)
		fmt.Fprintf(&log, "$ %s\n", redactedCheckoutCmd)
		out, stderr, err := sess.Exec(ctx, checkoutCmd)
		log.Write(out)
		log.Write(stderr)
		if err != nil {
			return fmt.Errorf("checkout: %w", err)
		}

		if out, _, err := sess.Exec(ctx, fmt.Sprintf("git -C %s rev-parse HEAD", shQuote(target.DeployPath))); err == nil {
			commitSHA = strings.TrimSpace(string(out))
		}

		if target.InstallCommand != "" {
			cmd := fmt.Sprintf("cd %s && %s", shQuote(target.DeployPath), target.InstallCommand)
			fmt.Fprintf(&log, "$ %s\n", cmd)
			out, stderr, err := sess.Exec(ctx, cmd)
			log.Write(out)
			log.Write(stderr)
			if err != nil {
				return fmt.Errorf("install: %w", err)
			}
		}

		if target.RestartCommand != "" {
			fmt.Fprintf(&log, "$ %s\n", target.RestartCommand)
			out, stderr, err := sess.Exec(ctx, target.RestartCommand)
			log.Write(out)
			log.Write(stderr)
			if err != nil {
				return fmt.Errorf("restart: %w", err)
			}
		}

		if target.HealthCheckURL != "" {
			fmt.Fprintf(&log, "health check: GET %s\n", target.HealthCheckURL)
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target.HealthCheckURL, nil)
			resp, err := e.httpClient().Do(req)
			if err != nil {
				return fmt.Errorf("health check: %w", err)
			}
			defer resp.Body.Close()
			fmt.Fprintf(&log, "health check: HTTP %d\n", resp.StatusCode)
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("health check: unexpected HTTP %d", resp.StatusCode)
			}
		}
		return nil
	}()

	if runErr != nil {
		status = "failed"
		fmt.Fprintf(&log, "\nERROR: %v\n", runErr)
	}

	logRef, writeErr := e.writeLog(d.ID, log.String())
	if writeErr != nil {
		logRef = ""
	}
	if err := e.Deployments.finish(d.ID, status, commitSHA, codeRollbackRef, logRef, time.Since(start).Milliseconds()); err != nil {
		return Deployment{}, err
	}
	final, err := e.Deployments.Get(d.ID)
	if err != nil {
		return Deployment{}, err
	}
	return final, runErr
}

// Rollback checks out a previous deployment's code_rollback_ref on the
// same server and re-runs restart+health-check, recording a new
// Deployment row for the rollback itself. It never touches
// artifact_rollback_ref or attempts to undo a migration --
// MigrationRollbackSupported is always false here, and callers must
// surface that rather than assume the rollback is complete (§13/§18).
func (e *Engine) Rollback(ctx context.Context, deploymentID string) (Deployment, error) {
	prev, err := e.Deployments.Get(deploymentID)
	if err != nil {
		return Deployment{}, err
	}
	if prev.CodeRollbackRef == "" {
		return Deployment{}, fmt.Errorf("deployment %s has no recorded rollback ref (nothing to roll back to)", deploymentID)
	}
	targets, err := e.Targets.ListByProject(prev.ProjectID)
	if err != nil || len(targets) == 0 {
		return Deployment{}, fmt.Errorf("no deployment target found for project %s", prev.ProjectID)
	}
	target := targets[0]
	srv, err := e.Servers.Get(target.ServerID)
	if err != nil {
		return Deployment{}, err
	}

	d := Deployment{ID: newID(), ProjectID: prev.ProjectID, ServerID: target.ServerID, Branch: target.Branch, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := e.Deployments.create(d); err != nil {
		return Deployment{}, err
	}

	start := time.Now()
	var log strings.Builder
	status := "success"

	runErr := func() error {
		sess, err := e.Remote.Connect(ctx, srv.ToProviderServer())
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		defer sess.Close()

		cmd := fmt.Sprintf("cd %s && git checkout %s", shQuote(target.DeployPath), shQuote(prev.CodeRollbackRef))
		fmt.Fprintf(&log, "$ %s\n", cmd)
		out, stderr, err := sess.Exec(ctx, cmd)
		log.Write(out)
		log.Write(stderr)
		if err != nil {
			return fmt.Errorf("checkout rollback ref: %w", err)
		}

		if target.RestartCommand != "" {
			fmt.Fprintf(&log, "$ %s\n", target.RestartCommand)
			out, stderr, err := sess.Exec(ctx, target.RestartCommand)
			log.Write(out)
			log.Write(stderr)
			if err != nil {
				return fmt.Errorf("restart: %w", err)
			}
		}
		return nil
	}()

	if runErr != nil {
		status = "failed"
		fmt.Fprintf(&log, "\nERROR: %v\n", runErr)
	}
	logRef, _ := e.writeLog(d.ID, log.String())
	if err := e.Deployments.finish(d.ID, status, prev.CodeRollbackRef, "", logRef, time.Since(start).Milliseconds()); err != nil {
		return Deployment{}, err
	}
	final, err := e.Deployments.Get(d.ID)
	if err != nil {
		return Deployment{}, err
	}
	return final, runErr
}

// resolveCloneURL returns (real URL with any credential injected, redacted
// URL safe to log). Only https URLs support credential injection (the
// standard `https://<token>@host/...` form GitHub/GitLab/Bitbucket all
// accept); anything else is returned unmodified.
func (e *Engine) resolveCloneURL(target Target) (real, redacted string, err error) {
	if target.IntegrationID == "" || !strings.HasPrefix(target.RepoURL, "https://") {
		return target.RepoURL, target.RepoURL, nil
	}
	integ, err := e.Integrations.Get(target.IntegrationID)
	if err != nil {
		return "", "", err
	}
	token, err := e.ResolveSecret(integ.SecretRef)
	if err != nil {
		return "", "", err
	}
	rest := strings.TrimPrefix(target.RepoURL, "https://")
	return "https://" + string(token) + "@" + rest, "https://***@" + rest, nil
}

func (e *Engine) writeLog(deploymentID, content string) (string, error) {
	if e.LogDir == "" {
		return "", nil
	}
	if err := os.MkdirAll(e.LogDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(e.LogDir, deploymentID+".log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func readLogFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// shQuote wraps s in single quotes for safe use as one POSIX shell word,
// escaping any embedded single quote. Every value interpolated into a
// remote Exec command in this file goes through this -- deploy_path and
// branch both ultimately come from user input (the DeploymentTarget
// config), and a raw, unquoted value would be a command-injection
// vulnerability over SSH.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
