package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// fakeGitHub starts a local httptest server that stands in for
// api.github.com, and returns a *Client pointed at it via WithBaseURL --
// nothing in this test suite ever makes a real network call to the real
// GitHub API. handler is expected to check request paths/headers as each
// test needs.
func fakeGitHub(t *testing.T, token string, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(token, WithBaseURL(srv.URL))
}

func TestValidateTokenSuccess(t *testing.T) {
	const testToken = "fake-pat-abc123"
	var gotAuth string
	client := fakeGitHub(t, testToken, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/user" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"login": "octocat"})
	})

	login, err := client.ValidateToken(context.Background())
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if login != "octocat" {
		t.Fatalf("expected login octocat, got %q", login)
	}
	if gotAuth != "Bearer "+testToken {
		t.Fatalf("expected Authorization header to carry the token, got %q", gotAuth)
	}
}

func TestValidateTokenFailure(t *testing.T) {
	client := fakeGitHub(t, "bad-token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := client.ValidateToken(context.Background()); err == nil {
		t.Fatal("expected an error for a 401 response")
	}
}

func TestListReposPaginates(t *testing.T) {
	pages := 0
	client := fakeGitHub(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		page := r.URL.Query().Get("page")
		pages++
		var repos []apiRepo
		if page == "1" {
			for i := 0; i < 100; i++ {
				repos = append(repos, apiRepo{FullName: "octocat/repo1", Name: "repo1", CloneURL: "https://example.invalid/repo1.git"})
			}
		} else {
			repos = []apiRepo{{FullName: "octocat/repo2", Name: "repo2", CloneURL: "https://example.invalid/repo2.git"}}
		}
		json.NewEncoder(w).Encode(repos)
	})

	repos, err := client.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 101 {
		t.Fatalf("expected 101 repos across two pages, got %d", len(repos))
	}
	if repos[100].ID != "octocat/repo2" {
		t.Fatalf("expected second page's repo, got %+v", repos[100])
	}
	if pages != 2 {
		t.Fatalf("expected exactly 2 pages fetched (stop once a page is short), got %d", pages)
	}
}

func TestBranches(t *testing.T) {
	client := fakeGitHub(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octocat/hello/branches" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]apiBranch{
			{Name: "main", Commit: struct {
				SHA string `json:"sha"`
			}{SHA: "abc123"}},
		})
	})

	branches, err := client.Branches(context.Background(), providers.Repo{ID: "octocat/hello"})
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(branches) != 1 || branches[0].Name != "main" || branches[0].SHA != "abc123" {
		t.Fatalf("unexpected branches: %+v", branches)
	}
}

func TestCommits(t *testing.T) {
	client := fakeGitHub(t, "tok", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octocat/hello/commits" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("sha") != "main" {
			t.Errorf("expected sha=main query param, got %q", r.URL.RawQuery)
		}
		type commitJSON struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
				Author  struct {
					Name string `json:"name"`
					Date string `json:"date"`
				} `json:"author"`
			} `json:"commit"`
		}
		cm := commitJSON{SHA: "deadbeef"}
		cm.Commit.Message = "initial commit"
		cm.Commit.Author.Name = "Octo Cat"
		cm.Commit.Author.Date = "2026-01-01T00:00:00Z"
		json.NewEncoder(w).Encode([]commitJSON{cm})
	})

	commits, err := client.Commits(context.Background(), providers.Repo{ID: "octocat/hello"}, "main", 10)
	if err != nil {
		t.Fatalf("Commits: %v", err)
	}
	if len(commits) != 1 || commits[0].SHA != "deadbeef" || commits[0].Message != "initial commit" {
		t.Fatalf("unexpected commits: %+v", commits)
	}
}

// --- Real git clone/pull against a local bare repo, served over plain
// HTTP (dumb protocol) -- a fully legitimate way to exercise real
// git-clone behavior without ever touching a real network host. ---

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
}

// serveBareRepo creates a bare git repo containing one commit ("hello.txt"),
// enables the dumb HTTP protocol on it (git update-server-info), and serves
// its parent directory over httptest.NewServer. It returns the clone URL
// (http://127.0.0.1:PORT/repo.git) and the bare repo's filesystem path (so
// callers can push further commits to test Pull).
func serveBareRepo(t *testing.T) (cloneURL string, barePath string) {
	t.Helper()
	requireGit(t)

	root := t.TempDir()
	barePath = filepath.Join(root, "srv", "repo.git")
	workPath := filepath.Join(root, "work")

	runGit(t, "", "init", "--bare", barePath)
	runGit(t, "", "init", workPath)
	if err := os.WriteFile(filepath.Join(workPath, "hello.txt"), []byte("hello v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, workPath, "add", "hello.txt")
	runGit(t, workPath, "commit", "-m", "initial commit")
	runGit(t, workPath, "push", barePath, "HEAD:refs/heads/main")
	runGit(t, barePath, "update-server-info")

	srv := httptest.NewServer(http.FileServer(http.Dir(filepath.Join(root, "srv"))))
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git", barePath
}

// runGit runs git with GIT_AUTHOR_*/GIT_COMMITTER_* set so commits work in
// CI without relying on any global git config being present.
func runGit(t *testing.T, dir string, args ...string) {
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

func TestCloneRealLocalBareRepoOverHTTP(t *testing.T) {
	cloneURL, _ := serveBareRepo(t)
	const testToken = "super-secret-test-token"
	client := NewClient(testToken)

	dest := filepath.Join(t.TempDir(), "clone")
	repo := providers.Repo{ID: "octocat/repo", Name: "repo", CloneURL: cloneURL}
	if err := client.Clone(context.Background(), repo, dest); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	if err != nil {
		t.Fatalf("expected cloned file to exist: %v", err)
	}
	if string(b) != "hello v1\n" {
		t.Fatalf("unexpected cloned content: %q", b)
	}
}

func TestPullRealLocalBareRepoOverHTTP(t *testing.T) {
	cloneURL, barePath := serveBareRepo(t)
	client := NewClient("test-token")

	dest := filepath.Join(t.TempDir(), "clone")
	repo := providers.Repo{ID: "octocat/repo", Name: "repo", CloneURL: cloneURL}
	if err := client.Clone(context.Background(), repo, dest); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Push a second commit straight to the bare repo (simulating upstream
	// changes) and re-enable dumb-protocol info files for it.
	work2 := filepath.Join(t.TempDir(), "work2")
	runGit(t, "", "clone", barePath, work2)
	if err := os.WriteFile(filepath.Join(work2, "hello.txt"), []byte("hello v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work2, "add", "hello.txt")
	runGit(t, work2, "commit", "-m", "second commit")
	runGit(t, work2, "push", "origin", "HEAD:refs/heads/main")
	runGit(t, barePath, "update-server-info")

	if err := client.Pull(context.Background(), repo, "main", dest); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello v2\n" {
		t.Fatalf("expected pull to bring in the second commit, got %q", b)
	}
}

func TestCloneFailureRedactsToken(t *testing.T) {
	requireGit(t)
	const testToken = "super-secret-clone-failure-token"
	client := NewClient(testToken)

	// An invalid remote URL causes git to fail and print the URL (with the
	// injected token) to stderr -- exactly the case the redaction guards.
	badRepo := providers.Repo{ID: "x/y", Name: "y", CloneURL: "http://127.0.0.1:1/nope.git"}
	dest := filepath.Join(t.TempDir(), "clone")
	err := client.Clone(context.Background(), badRepo, dest)
	if err == nil {
		t.Fatal("expected clone against an unreachable host to fail")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("token leaked into error message: %v", err)
	}
}
