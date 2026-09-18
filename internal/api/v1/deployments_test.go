package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/deployment"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/integration"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/project"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/server"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/gin-gonic/gin"
)

// deployFakeSession/deployFakeRemote are named distinctly from the
// similarly-shaped fakes in servers_test.go/deployment/engine_test.go to
// avoid a symbol collision within this package.
type deployFakeSession struct{ fail bool }

func (f *deployFakeSession) Exec(ctx context.Context, command string) ([]byte, []byte, error) {
	if f.fail {
		return nil, []byte("boom"), errDeployFake
	}
	// The pre-checkout "previous HEAD" lookup uses this best-effort form
	// (see engine.go) -- simulate "no repo checked out yet" for it, so a
	// fresh deploy correctly ends up with no rollback ref.
	if strings.Contains(command, "2>/dev/null || true") {
		return nil, nil, nil
	}
	return []byte("abc123\n"), nil, nil
}
func (f *deployFakeSession) ListDir(ctx context.Context, path string) ([]providers.RemoteFileInfo, error) { return nil, nil }
func (f *deployFakeSession) ReadFile(ctx context.Context, path string) ([]byte, error)                    { return nil, nil }
func (f *deployFakeSession) WriteFile(ctx context.Context, path string, data []byte) error                { return nil }
func (f *deployFakeSession) Metrics(ctx context.Context) (providers.RemoteMetrics, error)                 { return providers.RemoteMetrics{}, nil }
func (f *deployFakeSession) Close() error                                                                   { return nil }

var errDeployFake = &deployFakeErr{}

type deployFakeErr struct{}

func (e *deployFakeErr) Error() string { return "fake deploy command failure" }

type deployFakeRemote struct{ session *deployFakeSession }

func (f *deployFakeRemote) Connect(ctx context.Context, s providers.Server) (providers.RemoteSession, error) {
	return f.session, nil
}

func newDeploymentsTestRouter(t *testing.T) (*gin.Engine, Deps, string, string) {
	t.Helper()
	_, d := newTestRouter(t, "secret-token")

	srv, err := server.NewRepository(d.DB()).Create(server.CreateRequest{Name: "vps", Hostname: "1.2.3.4", Port: 22, Username: "deploy", AuthMethod: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := project.NewRepository(d.DB()).Create("app", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	d.DeployEngine = &deployment.Engine{
		Remote:        &deployFakeRemote{session: &deployFakeSession{}},
		Servers:       server.NewRepository(d.DB()),
		Targets:       deployment.NewTargetRepository(d.DB()),
		Deployments:   deployment.NewRepository(d.DB()),
		Integrations:  integration.NewRepository(d.DB()),
		ResolveSecret: func(ref string) ([]byte, error) { return []byte("tok"), nil },
		LogDir:        t.TempDir(),
	}

	g := gin.New()
	Mount(g, d)
	return g, d, proj.ID, srv.ID
}

func TestCreateTargetAndDeployEndToEnd(t *testing.T) {
	g, d, projID, srvID := newDeploymentsTestRouter(t)

	createRec := jsonRequest(g, http.MethodPost, "/api/v1/deployment-targets", "secret-token",
		`{"projectId":"`+projID+`","serverId":"`+srvID+`","repoUrl":"https://github.com/example/app.git","branch":"main","deployPath":"/srv/app"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var target map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}
	targetID := target["id"].(string)

	deployRec := jsonRequest(g, http.MethodPost, "/api/v1/deployments", "secret-token", `{"targetId":"`+targetID+`"}`)
	if deployRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", deployRec.Code, deployRec.Body.String())
	}
	var dep map[string]any
	if err := json.Unmarshal(deployRec.Body.Bytes(), &dep); err != nil {
		t.Fatal(err)
	}
	if dep["status"] != "success" {
		t.Fatalf("expected success, got %+v", dep)
	}
	if dep["migrationRollbackSupported"] != false {
		t.Fatalf("migrationRollbackSupported must default false, got %+v", dep["migrationRollbackSupported"])
	}

	listRec := jsonRequest(g, http.MethodGet, "/api/v1/deployments?projectId="+projID, "secret-token", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	var history []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 deployment in history, got %d", len(history))
	}

	var auditCount int
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM audit_events WHERE action = 'deployment.deploy'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit event for deployment.deploy, got %d", auditCount)
	}
}

func TestDeleteDeploymentTargetRequiresConfirm(t *testing.T) {
	g, _, projID, srvID := newDeploymentsTestRouter(t)
	createRec := jsonRequest(g, http.MethodPost, "/api/v1/deployment-targets", "secret-token",
		`{"projectId":"`+projID+`","serverId":"`+srvID+`","repoUrl":"https://github.com/example/app.git","deployPath":"/srv/app"}`)
	var target map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}
	id := target["id"].(string)

	noConfirmRec := jsonRequest(g, http.MethodDelete, "/api/v1/deployment-targets/"+id, "secret-token", "")
	if noConfirmRec.Code != http.StatusBadRequest {
		t.Fatalf("expected delete without confirm=true to be rejected, got %d", noConfirmRec.Code)
	}
	confirmedRec := jsonRequest(g, http.MethodDelete, "/api/v1/deployment-targets/"+id+"?confirm=true", "secret-token", "")
	if confirmedRec.Code != http.StatusOK {
		t.Fatalf("expected confirmed delete to succeed, got %d: %s", confirmedRec.Code, confirmedRec.Body.String())
	}
}

func TestRollbackSurfacesNoMigrationSupport(t *testing.T) {
	g, _, projID, srvID := newDeploymentsTestRouter(t)
	createRec := jsonRequest(g, http.MethodPost, "/api/v1/deployment-targets", "secret-token",
		`{"projectId":"`+projID+`","serverId":"`+srvID+`","repoUrl":"https://github.com/example/app.git","deployPath":"/srv/app"}`)
	var target map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}
	targetID := target["id"].(string)

	// First deploy has no prior commit to roll back to -- expect the
	// rollback attempt on it to be refused rather than silently no-op.
	deployRec := jsonRequest(g, http.MethodPost, "/api/v1/deployments", "secret-token", `{"targetId":"`+targetID+`"}`)
	var dep map[string]any
	if err := json.Unmarshal(deployRec.Body.Bytes(), &dep); err != nil {
		t.Fatal(err)
	}
	id := dep["id"].(string)

	rec := jsonRequest(g, http.MethodPost, "/api/v1/deployments/"+id+"/rollback", "secret-token", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected rollback with no rollback ref to be rejected, got %d: %s", rec.Code, rec.Body.String())
	}
}
