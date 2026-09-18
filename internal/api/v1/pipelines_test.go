package v1

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/deployment"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/integration"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/project"
	"github.com/alresiainc/alresia-voltpanel/internal/domain/server"
	"github.com/alresiainc/alresia-voltpanel/internal/pipeline"
	"github.com/gin-gonic/gin"
)

func newPipelinesTestRouter(t *testing.T) (*gin.Engine, Deps, string) {
	t.Helper()
	_, d := newTestRouter(t, "secret-token")

	proj, err := project.NewRepository(d.DB()).Create("app", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	servers := server.NewRepository(d.DB())
	remote := &fakeRemoteProvider{session: &fakeRemoteSession{}}
	d.DeployEngine = &deployment.Engine{
		Remote: remote, Servers: servers, Targets: deployment.NewTargetRepository(d.DB()),
		Deployments: deployment.NewRepository(d.DB()), Integrations: integration.NewRepository(d.DB()),
		ResolveSecret: func(ref string) ([]byte, error) { return []byte("tok"), nil },
		LogDir:        t.TempDir(),
	}
	d.PipelineEngine = &pipeline.Engine{Remote: remote, Servers: servers, DeployEngine: d.DeployEngine}

	g := gin.New()
	Mount(g, d)
	return g, d, proj.ID
}

func signWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestCreatePipelineRejectsInvalidYAML(t *testing.T) {
	g, _, projID := newPipelinesTestRouter(t)
	rec := jsonRequest(g, http.MethodPost, "/api/v1/pipelines", "secret-token", `{"projectId":"`+projID+`","name":"ci","definitionYaml":"steps: []"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty-steps pipeline, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreatePipelineExposesWebhookSecretOnceThenHidesIt(t *testing.T) {
	g, _, projID := newPipelinesTestRouter(t)
	yamlDef := "steps:\n  - name: t\n    run: echo hi\n"
	createRec := jsonRequest(g, http.MethodPost, "/api/v1/pipelines", "secret-token", `{"projectId":"`+projID+`","name":"ci","definitionYaml":"`+strings.ReplaceAll(yamlDef, "\n", `\n`)+`"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	secret, _ := created["webhookSecret"].(string)
	if secret == "" {
		t.Fatal("expected the create response to include a webhookSecret")
	}

	listRec := jsonRequest(g, http.MethodGet, "/api/v1/pipelines?projectId="+projID, "secret-token", "")
	if strings.Contains(listRec.Body.String(), secret) {
		t.Fatalf("webhook secret leaked into the list response: %s", listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), "webhookSecret") {
		t.Fatalf("webhookSecret field must not appear at all in list responses: %s", listRec.Body.String())
	}
}

func TestManualRunExecutesAndRecordsHistory(t *testing.T) {
	g, d, projID := newPipelinesTestRouter(t)
	yamlDef := `{"projectId":"` + projID + `","name":"ci","definitionYaml":"steps:\n  - name: t\n    run: echo hi\n"}`
	createRec := jsonRequest(g, http.MethodPost, "/api/v1/pipelines", "secret-token", yamlDef)
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)

	runRec := jsonRequest(g, http.MethodPost, "/api/v1/pipelines/"+id+"/run", "secret-token", "")
	if runRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", runRec.Code, runRec.Body.String())
	}
	var run map[string]any
	if err := json.Unmarshal(runRec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run["status"] != "success" {
		t.Fatalf("expected success, got %+v", run)
	}

	historyRec := jsonRequest(g, http.MethodGet, "/api/v1/pipelines/"+id+"/runs", "secret-token", "")
	var history []map[string]any
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 run in history, got %d", len(history))
	}

	var auditCount int
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM audit_events WHERE action = 'pipeline.run'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit event, got %d", auditCount)
	}
}

func TestWebhookRequiresValidSignature(t *testing.T) {
	g, _, projID := newPipelinesTestRouter(t)
	createRec := jsonRequest(g, http.MethodPost, "/api/v1/pipelines", "secret-token",
		`{"projectId":"`+projID+`","name":"ci","definitionYaml":"steps:\n  - name: t\n    run: echo hi\n"}`)
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)
	secret := created["webhookSecret"].(string)

	body := []byte(`{"ref":"refs/heads/main"}`)

	// No signature at all.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/"+id+"/webhook", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no signature, got %d", rec.Code)
	}

	// Wrong signature.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/"+id+"/webhook", strings.NewReader(string(body)))
	req2.Header.Set("X-Volt-Signature", "sha256=deadbeef")
	rec2 := httptest.NewRecorder()
	g.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a wrong signature, got %d", rec2.Code)
	}

	// Correct signature.
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/"+id+"/webhook", strings.NewReader(string(body)))
	req3.Header.Set("X-Volt-Signature", signWebhook(secret, body))
	rec3 := httptest.NewRecorder()
	g.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 with a correct signature, got %d: %s", rec3.Code, rec3.Body.String())
	}
}

func TestDeletePipelineRequiresConfirm(t *testing.T) {
	g, _, projID := newPipelinesTestRouter(t)
	createRec := jsonRequest(g, http.MethodPost, "/api/v1/pipelines", "secret-token",
		`{"projectId":"`+projID+`","name":"ci","definitionYaml":"steps:\n  - name: t\n    run: echo hi\n"}`)
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)

	noConfirmRec := jsonRequest(g, http.MethodDelete, "/api/v1/pipelines/"+id, "secret-token", "")
	if noConfirmRec.Code != http.StatusBadRequest {
		t.Fatalf("expected delete without confirm=true to be rejected, got %d", noConfirmRec.Code)
	}
	confirmedRec := jsonRequest(g, http.MethodDelete, "/api/v1/pipelines/"+id+"?confirm=true", "secret-token", "")
	if confirmedRec.Code != http.StatusOK {
		t.Fatalf("expected confirmed delete to succeed, got %d: %s", confirmedRec.Code, confirmedRec.Body.String())
	}
}
