// Package github implements internal/integrations/git.Provider against
// GitHub's REST API, talked to directly over net/http (no third-party
// GitHub SDK -- the surface needed here, per §17 Phase 8, is small enough
// that stdlib is the right call). The base URL is injectable
// (WithBaseURL) specifically so tests can point a Client at an
// httptest.NewServer fake instead of the real api.github.com -- nothing in
// this package's test suite makes a real network call.
//
// Authentication is PAT-based (a user-supplied personal access token), not
// OAuth -- per the plan, that needs no client-secret registration, which
// matters since Phase 8 has no OAuth app credentials available to it.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	gitcontract "github.com/alresiainc/alresia-voltpanel/internal/integrations/git"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// defaultBaseURL is the real GitHub REST API. Production code never
// overrides this; tests always do via WithBaseURL.
const defaultBaseURL = "https://api.github.com"

// Client talks to one GitHub account's REST API using a single PAT. Zero
// value is not usable; construct with NewClient.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// Option customizes a Client constructed by NewClient.
type Option func(*Client)

// WithBaseURL points the client at a different API base URL -- used by
// tests to talk to an httptest.NewServer fake instead of the real GitHub
// API. Trailing slashes are stripped.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient swaps the underlying *http.Client (tests use this to
// point at an httptest.NewServer's own client, e.g. for TLS test servers).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.http = hc }
}

// NewClient builds a Client authenticated with token. token may be empty
// only for unauthenticated, read-only smoke checks against public
// endpoints -- every method that needs a real account requires it.
func NewClient(token string, opts ...Option) *Client {
	c := &Client{
		token:   token,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// apiRepo is the subset of GitHub's repository JSON shape this package
// needs. FullName ("owner/repo") is used as providers.Repo.ID so later
// calls (Branches, Commits, Clone) can recover owner/repo without a second
// lookup.
type apiRepo struct {
	FullName string `json:"full_name"`
	Name     string `json:"name"`
	CloneURL string `json:"clone_url"`
}

type apiBranch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

type apiCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

type apiUser struct {
	Login string `json:"login"`
}

// doJSON issues an authenticated GET against path (relative to baseURL,
// must start with "/") and decodes a 2xx JSON body into out. Non-2xx
// responses become a descriptive error that never includes the token --
// the token only ever appears in the Authorization header, never in any
// string this method builds.
func (c *Client) doJSON(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.http.Do(req)
	if err != nil {
		// http.Client's error can embed the request URL, which never
		// contains the token (it's header-only auth here) -- safe as-is.
		return fmt.Errorf("github request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github API returned %s for %s", resp.Status, path)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode github response: %w", err)
	}
	return nil
}

// ValidateToken confirms the token works by calling GET /user, returning
// the authenticated login on success. Used by the integrations API to
// reject a bad PAT before it's ever persisted.
func (c *Client) ValidateToken(ctx context.Context) (string, error) {
	var u apiUser
	if err := c.doJSON(ctx, http.MethodGet, "/user", &u); err != nil {
		return "", fmt.Errorf("token validation failed: %w", err)
	}
	if u.Login == "" {
		return "", fmt.Errorf("token validation failed: empty login in response")
	}
	return u.Login, nil
}

// ListRepos implements providers.GitProvider. It paginates through the
// authenticated user's repos (100 per page, capped at 10 pages / 1000
// repos, which is far past what a local dev tool needs to handle
// gracefully rather than looping forever against a misbehaving server).
func (c *Client) ListRepos(ctx context.Context) ([]providers.Repo, error) {
	var out []providers.Repo
	for page := 1; page <= 10; page++ {
		var batch []apiRepo
		path := fmt.Sprintf("/user/repos?per_page=100&page=%d", page)
		if err := c.doJSON(ctx, http.MethodGet, path, &batch); err != nil {
			return nil, fmt.Errorf("list repos: %w", err)
		}
		for _, r := range batch {
			out = append(out, providers.Repo{ID: r.FullName, Name: r.Name, CloneURL: r.CloneURL})
		}
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

// splitFullName recovers owner/repo from a providers.Repo.ID built by
// ListRepos ("owner/repo"), or from a caller-constructed Repo whose ID is
// already in that shape (the API handlers accept "owner/repo" directly).
func splitFullName(id string) (owner, repo string, err error) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repo id %q is not in owner/repo form", id)
	}
	return parts[0], parts[1], nil
}

// Branches implements providers.GitProvider.
func (c *Client) Branches(ctx context.Context, repo providers.Repo) ([]providers.Branch, error) {
	owner, name, err := splitFullName(repo.ID)
	if err != nil {
		return nil, err
	}
	var batch []apiBranch
	path := fmt.Sprintf("/repos/%s/%s/branches?per_page=100", url.PathEscape(owner), url.PathEscape(name))
	if err := c.doJSON(ctx, http.MethodGet, path, &batch); err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	out := make([]providers.Branch, 0, len(batch))
	for _, b := range batch {
		out = append(out, providers.Branch{Name: b.Name, SHA: b.Commit.SHA})
	}
	return out, nil
}

// Commits implements git.Provider's extension to providers.GitProvider.
func (c *Client) Commits(ctx context.Context, repo providers.Repo, branch string, limit int) ([]gitcontract.Commit, error) {
	owner, name, err := splitFullName(repo.ID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	path := fmt.Sprintf("/repos/%s/%s/commits?per_page=%d", url.PathEscape(owner), url.PathEscape(name), limit)
	if branch != "" {
		path += "&sha=" + url.QueryEscape(branch)
	}
	var batch []apiCommit
	if err := c.doJSON(ctx, http.MethodGet, path, &batch); err != nil {
		return nil, fmt.Errorf("list commits: %w", err)
	}
	out := make([]gitcontract.Commit, 0, len(batch))
	for _, cm := range batch {
		out = append(out, gitcontract.Commit{
			SHA:     cm.SHA,
			Message: cm.Commit.Message,
			Author:  cm.Commit.Author.Name,
			Date:    cm.Commit.Author.Date,
		})
	}
	return out, nil
}

// Clone implements providers.GitProvider by shelling out to the real `git`
// binary -- this is intentionally not a from-scratch git-protocol
// implementation. The PAT is injected into the clone URL as userinfo
// (never passed as a bare CLI flag, never logged); on failure, the
// process's combined output is redacted of the token before it's wrapped
// into the returned error, so it can never reach a log line or an API
// response even indirectly.
func (c *Client) Clone(ctx context.Context, repo providers.Repo, dest string) error {
	authURL, err := c.authenticatedURL(repo.CloneURL)
	if err != nil {
		return fmt.Errorf("build clone url: %w", err)
	}
	out, err := exec.CommandContext(ctx, "git", "clone", authURL, dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %s", c.redact(string(out)))
	}
	return nil
}

// Pull fast-forwards an already-cloned repo at dest. It targets the given
// remote URL directly (rather than assuming `origin` is already configured
// with credentials) so a fresh PAT is used on every call instead of one
// baked into the repo's stored remote config.
func (c *Client) Pull(ctx context.Context, repo providers.Repo, branch, dest string) error {
	authURL, err := c.authenticatedURL(repo.CloneURL)
	if err != nil {
		return fmt.Errorf("build pull url: %w", err)
	}
	args := []string{"-C", dest, "pull", authURL}
	if branch != "" {
		args = append(args, branch)
	}
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %s", c.redact(string(out)))
	}
	return nil
}

// authenticatedURL injects the token as URL userinfo for HTTP(S) clone
// URLs (GitHub's supported PAT-over-HTTPS auth mechanism). Non-HTTP(S)
// remotes (SSH) are returned unchanged -- PAT auth doesn't apply to them.
func (c *Client) authenticatedURL(rawURL string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("repo has no clone URL")
	}
	if c.token == "" {
		return rawURL, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return rawURL, nil
	}
	u.User = url.User(c.token)
	return u.String(), nil
}

// redact strips the raw token out of s so it can never reach a log line or
// an API response, even via a wrapped git-process error. A no-op when no
// token is configured.
func (c *Client) redact(s string) string {
	if c.token == "" {
		return s
	}
	return strings.ReplaceAll(s, c.token, "***")
}

var _ providers.GitProvider = (*Client)(nil)
var _ gitcontract.Provider = (*Client)(nil)
