package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectFixtureFile(t *testing.T, dir string) error {
	t.Helper()
	return os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node index.js"}}`), 0o644)
}

func createProjectRequest(t *testing.T, g http.Handler, token, name, path string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"name":"` + name + `","path":"` + strings.ReplaceAll(path, `\`, `\\`) + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Volt-Token", token)
	g.ServeHTTP(w, req)
	return w
}

func TestCreateProjectValidatesAndDetects(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	dir := t.TempDir()

	w := createProjectRequest(t, g, "secret-token", "my-app", dir)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Path         string `json:"path"`
		DetectedKind string `json:"detectedKind"`
		RunCommand   string `json:"runCommand"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID == "" {
		t.Fatal("expected an id in the response")
	}
	if got.DetectedKind != "unknown" {
		t.Fatalf("expected unknown detection for an empty dir, got %q", got.DetectedKind)
	}
}

func TestCreateProjectRejectsRelativePath(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	w := createProjectRequest(t, g, "secret-token", "bad", "relative/path")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a relative path, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProjectRejectsMissingPath(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	w := createProjectRequest(t, g, "secret-token", "bad", "/definitely/does/not/exist/anywhere")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a nonexistent path, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAndGetProject(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	dir := t.TempDir()
	created := createProjectRequest(t, g, "secret-token", "listed", dir)
	if created.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d", created.Code)
	}
	var cp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &cp); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), cp.ID) {
		t.Fatalf("expected list to contain created project id %q: %s", cp.ID, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+cp.ID, nil)
	req2.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestGetUnknownProjectReturns404(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/does-not-exist", nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteProjectRequiresConfirm(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	dir := t.TempDir()
	created := createProjectRequest(t, g, "secret-token", "to-delete", dir)
	var cp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &cp); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+cp.ID, nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected delete without confirm=true to be rejected, got %d: %s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+cp.ID+"?confirm=true", nil)
	req2.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected confirmed delete to succeed, got %d: %s", w2.Code, w2.Body.String())
	}

	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+cp.ID, nil)
	req3.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w3, req3)
	if w3.Code != http.StatusNotFound {
		t.Fatalf("expected deleted project to 404, got %d", w3.Code)
	}
}

func TestRedetectProjectEndpoint(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	dir := t.TempDir()
	created := createProjectRequest(t, g, "secret-token", "shifting", dir)
	var cp struct {
		ID           string `json:"id"`
		DetectedKind string `json:"detectedKind"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &cp); err != nil {
		t.Fatal(err)
	}
	if cp.DetectedKind != "unknown" {
		t.Fatalf("expected initial detection to be unknown, got %q", cp.DetectedKind)
	}

	// package.json with a start script appears after the project was added.
	if err := writeProjectFixtureFile(t, dir); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+cp.ID+"/detect", nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		DetectedKind string `json:"detectedKind"`
		RunCommand   string `json:"runCommand"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DetectedKind != "node" || got.RunCommand != "npm start" {
		t.Fatalf("expected redetect to pick up node/npm start, got %+v", got)
	}
}

func TestMutatingProjectCallsWriteAuditEvents(t *testing.T) {
	g, d := newTestRouter(t, "secret-token")
	dir := t.TempDir()
	w := createProjectRequest(t, g, "secret-token", "audited", dir)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM audit_events WHERE action = 'project.create'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit event for project.create, got %d", count)
	}
}
