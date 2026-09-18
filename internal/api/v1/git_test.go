package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGitHubAPI stands in for api.github.com for the router tests below.
// It serves just enough of the REST surface (§GET /user, /user/repos,
// /repos/:owner/:repo/branches) for the acceptance flow: connect, list
// repos, clone, list branches. cloneURL is the (local, http) URL the fake
// /user/repos response advertises as the one test repo's clone_url.
func fakeGitHubAPI(t *testing.T, token, cloneURL string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/user":
			json.NewEncoder(w).Encode(map[string]string{"login": "octocat"})
		case r.URL.Path == "/user/repos":
			json.NewEncoder(w).Encode([]map[string]string{{
				"full_name": "octocat/hello", "name": "hello", "clone_url": cloneURL,
			}})
		case r.URL.Path == "/repos/octocat/hello/branches":
			json.NewEncoder(w).Encode([]map[string]any{{
				"name": "main", "commit": map[string]string{"sha": "abc123"},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// serveLocalBareRepoForRouterTest mirrors the github package's own helper
// (kept duplicated rather than shared across packages for such a small,
// test-only helper) -- a real bare git repo, served over plain HTTP via
// the dumb protocol, standing in for a real GitHub remote.
func serveLocalBareRepoForRouterTest(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	root := t.TempDir()
	barePath := filepath.Join(root, "srv", "repo.git")
	workPath := filepath.Join(root, "work")

	runGitCmd(t, "", "init", "--bare", barePath)
	runGitCmd(t, "", "init", workPath)
	if err := os.WriteFile(filepath.Join(workPath, "hello.txt"), []byte("hello from fake github\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, workPath, "add", "hello.txt")
	runGitCmd(t, workPath, "commit", "-m", "initial commit")
	runGitCmd(t, workPath, "push", barePath, "HEAD:refs/heads/main")
	runGitCmd(t, barePath, "update-server-info")

	srv := httptest.NewServer(http.FileServer(http.Dir(filepath.Join(root, "srv"))))
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git"
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Volt Test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=Volt Test", "GIT_COMMITTER_EMAIL=test@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// TestGitIntegrationEndToEnd is the plan's Phase 8 acceptance criterion,
// adapted for an environment with no real GitHub account available:
// against a fake GitHub API test server, connect an "account", list its
// repos, clone one (into a real local temp dir, from a real local bare git
// repo served over http), and list its branches -- all through the real
// HTTP router, never touching any real external host.
func TestGitIntegrationEndToEnd(t *testing.T) {
	const testToken = "fake-pat-should-never-leak-anywhere"
	cloneURL := serveLocalBareRepoForRouterTest(t)
	apiBaseURL := fakeGitHubAPI(t, testToken, cloneURL)

	_, d := newTestRouter(t, "secret-token")
	d.GitHubBaseURL = apiBaseURL
	g := rebuildRouter(d)

	// 1. Connect the account.
	body := `{"kind":"github","token":"` + testToken + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 creating integration, got %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		ID         string `json:"id"`
		AccountRef string `json:"accountRef"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.AccountRef != "octocat" {
		t.Fatalf("unexpected integration created: %+v", created)
	}
	if strings.Contains(w.Body.String(), testToken) {
		t.Fatal("the raw token must never appear in the API response")
	}

	// 2. List repos for that integration.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/git/repos?integrationId="+created.ID, nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 listing repos, got %d: %s", w.Code, w.Body.String())
	}
	var repos []struct {
		ID       string `json:"ID"`
		Name     string `json:"Name"`
		CloneURL string `json:"CloneURL"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &repos); err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].ID != "octocat/hello" {
		t.Fatalf("unexpected repos: %+v (body=%s)", repos, w.Body.String())
	}
	if strings.Contains(w.Body.String(), testToken) {
		t.Fatal("the raw token must never appear in the repos response")
	}

	// 3. Clone the repo into a real local temp directory.
	dest := filepath.Join(t.TempDir(), "cloned-repo")
	cloneBody := `{"integrationId":"` + created.ID + `","repo":{"id":"octocat/hello","name":"hello","cloneUrl":"` + repos[0].CloneURL + `"},"dest":"` + dest + `"}`
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/git/clone", strings.NewReader(cloneBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 cloning repo, got %d: %s", w.Code, w.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	if err != nil {
		t.Fatalf("expected the clone to have produced a real checkout: %v", err)
	}
	if string(b) != "hello from fake github\n" {
		t.Fatalf("unexpected cloned content: %q", b)
	}
	if strings.Contains(w.Body.String(), testToken) {
		t.Fatal("the raw token must never appear in the clone response")
	}

	// 4. List branches.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/git/repos/octocat/hello/branches?integrationId="+created.ID, nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 listing branches, got %d: %s", w.Code, w.Body.String())
	}
	var branches []struct {
		Name string `json:"Name"`
		SHA  string `json:"SHA"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &branches); err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0].Name != "main" {
		t.Fatalf("unexpected branches: %+v", branches)
	}

	// 5. Deleting the integration removes both the row and its secret.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+created.ID, nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 deleting integration, got %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/git/repos?integrationId="+created.ID, nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected repos lookup against a deleted integration to 404, got %d", w.Code)
	}
}

func TestCreateIntegrationRejectsBadToken(t *testing.T) {
	apiBaseURL := fakeGitHubAPI(t, "the-real-token", "http://example.invalid/repo.git")
	_, d := newTestRouter(t, "secret-token")
	d.GitHubBaseURL = apiBaseURL
	g := rebuildRouter(d)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations", strings.NewReader(`{"kind":"github","token":"wrong-token"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a token that fails validation, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM integrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("a failed validation must not persist an integration row")
	}
}
