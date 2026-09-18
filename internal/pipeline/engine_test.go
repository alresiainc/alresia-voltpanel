package pipeline

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/deployment"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/integration"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/project"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/server"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

type pipeFakeSession struct{ out string }

func (f *pipeFakeSession) Exec(ctx context.Context, command string) ([]byte, []byte, error) {
	return []byte(f.out), nil, nil
}
func (f *pipeFakeSession) ListDir(ctx context.Context, path string) ([]providers.RemoteFileInfo, error) {
	return nil, nil
}
func (f *pipeFakeSession) ReadFile(ctx context.Context, path string) ([]byte, error)     { return nil, nil }
func (f *pipeFakeSession) WriteFile(ctx context.Context, path string, data []byte) error { return nil }
func (f *pipeFakeSession) Metrics(ctx context.Context) (providers.RemoteMetrics, error) {
	return providers.RemoteMetrics{}, nil
}
func (f *pipeFakeSession) Close() error { return nil }

type pipeFakeRemote struct{ session *pipeFakeSession }

func (f *pipeFakeRemote) Connect(ctx context.Context, s providers.Server) (providers.RemoteSession, error) {
	return f.session, nil
}

func newTestEngine(t *testing.T) (*Engine, *server.Repository, string) {
	t.Helper()
	db, err := storage.OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	servers := server.NewRepository(db)
	srv, err := servers.Create(server.CreateRequest{Name: "vps", Hostname: "1.2.3.4", Port: 22, Username: "deploy", AuthMethod: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	remote := &pipeFakeRemote{session: &pipeFakeSession{out: "ok"}}
	proj, err := project.NewRepository(db).Create("app", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deployEngine := &deployment.Engine{
		Remote:        remote,
		Servers:       servers,
		Targets:       deployment.NewTargetRepository(db),
		Deployments:   deployment.NewRepository(db),
		Integrations:  integration.NewRepository(db),
		ResolveSecret: func(ref string) ([]byte, error) { return []byte("tok"), nil },
		LogDir:        t.TempDir(),
	}
	target, err := deployEngine.Targets.Create(deployment.CreateTargetRequest{ProjectID: proj.ID, ServerID: srv.ID, RepoURL: "https://github.com/example/app.git", Branch: "main", DeployPath: "/srv/app"})
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{Remote: remote, Servers: servers, DeployEngine: deployEngine}, servers, target.ID
}

func TestRunAllStepKindsSucceed(t *testing.T) {
	healthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer healthSrv.Close()

	e, servers, targetID := newTestEngine(t)
	list, _ := servers.List()
	serverID := list[0].ID

	def := Definition{Steps: []Step{
		{Name: "test", Run: "echo hello"},
		{Name: "remote-check", SSH: &SSHStep{ServerID: serverID, Command: "uptime"}},
		{Name: "ship", Deploy: targetID},
		{Name: "verify", HealthCheck: &HealthCheckStep{URL: healthSrv.URL, ExpectedStatus: 200}},
	}}

	results, err := e.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 step results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "success" {
			t.Fatalf("expected step %q to succeed, got %q (output: %s)", r.Name, r.Status, r.Output)
		}
	}
	if !strings.Contains(results[0].Output, "hello") {
		t.Fatalf("expected local run output to contain 'hello', got %q", results[0].Output)
	}
}

func TestRunStopsAtFirstFailure(t *testing.T) {
	e, _, _ := newTestEngine(t)
	def := Definition{Steps: []Step{
		{Name: "fails", Run: "exit 1"},
		{Name: "never-runs", Run: "echo should-not-run"},
	}}

	results, err := e.Run(context.Background(), def)
	if err == nil {
		t.Fatal("expected Run to return an error when a step fails")
	}
	if len(results) != 1 {
		t.Fatalf("expected only the failing step to be recorded, got %d results", len(results))
	}
	if results[0].Status != "failed" {
		t.Fatalf("expected failed status, got %s", results[0].Status)
	}
}

func TestHealthCheckStepFailsOnUnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	e, _, _ := newTestEngine(t)

	def := Definition{Steps: []Step{{Name: "verify", HealthCheck: &HealthCheckStep{URL: srv.URL, ExpectedStatus: 200}}}}
	_, err := e.Run(context.Background(), def)
	if err == nil {
		t.Fatal("expected a 500 response to fail the healthcheck step")
	}
}
