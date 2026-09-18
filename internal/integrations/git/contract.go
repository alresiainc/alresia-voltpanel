// Package git holds the shared, GitHost-agnostic pieces of the §17 Phase 8
// Git integration: a thin superset of internal/providers.GitProvider (§7)
// plus the types that interface's trailing "... pull/push/commits/tags/
// deploy-keys/webhooks" comment calls out as real, near-term additions.
// GitHub-specific code lives in the github subpackage and implements this
// Provider interface; GitLab/Bitbucket can follow later against the same
// shape without touching GitHub's code.
package git

import (
	"context"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// Commit is the minimal shape Provider.Commits returns. Kept in this
// package (rather than internal/providers) since commits aren't part of
// providers.GitProvider's stable, cross-phase-referenced contract -- only
// this package's superset of it.
type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// Provider is providers.GitProvider (ListRepos/Clone/Branches) plus
// Pull/Commits. Push/tags/deploy-keys/webhooks are deliberately not part of
// this interface yet -- no implementation exists for them, and adding
// methods nothing implements would just be a promise the interface can't
// keep; §7's plan comment lists them as the eventual, not immediate, scope.
type Provider interface {
	providers.GitProvider
	// Pull fast-forwards an already-cloned repo at dest to the tip of
	// branch (or the remote's default branch if branch is empty).
	Pull(ctx context.Context, repo providers.Repo, branch, dest string) error
	// Commits lists up to limit recent commits on branch (or the default
	// branch if branch is empty), newest first.
	Commits(ctx context.Context, repo providers.Repo, branch string, limit int) ([]Commit, error)
}
