package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/agent"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
	"github.com/gin-gonic/gin"
)

// fakeRuntimeProvider is a mocked providers.RuntimeProvider -- router
// tests must never shell out to a real `node`/`php`/version manager (per
// the plan: "don't actually install Node/PHP in unit tests").
type fakeRuntimeProvider struct {
	kind          string
	detected      []providers.RuntimeVersion
	detectErr     error
	setDefaultErr error
	setDefaultLog []string
}

func (f *fakeRuntimeProvider) Kind() string { return f.kind }
func (f *fakeRuntimeProvider) DetectInstalled(ctx context.Context) ([]providers.RuntimeVersion, error) {
	return f.detected, f.detectErr
}
func (f *fakeRuntimeProvider) Install(ctx context.Context, version string, progress providers.ProgressFunc) error {
	return nil
}
func (f *fakeRuntimeProvider) Remove(ctx context.Context, version string) error { return nil }
func (f *fakeRuntimeProvider) SetDefault(ctx context.Context, version string) error {
	f.setDefaultLog = append(f.setDefaultLog, version)
	return f.setDefaultErr
}

// newRuntimesTestRouter mirrors router_test.go's newTestRouter but also
// wires a Registry of fake RuntimeProviders, so these tests exercise the
// full authed HTTP path without touching the real system.
func newRuntimesTestRouter(t *testing.T, providersList ...providers.RuntimeProvider) (*gin.Engine, Deps) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfgDir := t.TempDir()
	root := t.TempDir()
	st, err := storage.NewStoreWithRoot(cfgDir, root)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := security.NewSessionSecret()
	if err != nil {
		t.Fatal(err)
	}
	session, err := security.NewSessionAuth(secret)
	if err != nil {
		t.Fatal(err)
	}
	token := "secret-token"
	hub := ws.NewHub(token, false, session)
	registry := providers.NewRegistry()
	for _, p := range providersList {
		registry.RegisterRuntime(p)
	}
	d := Deps{Store: st, Mgr: agent.NewManager(st), Hub: hub, Session: session, Token: token, Dev: false, Providers: registry}
	g := gin.New()
	Mount(g, d)
	return g, d
}

func doRequest(g *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("X-Volt-Token", token)
	}
	g.ServeHTTP(w, req)
	return w
}

func TestListRuntimesDetectsAndPersists(t *testing.T) {
	node := &fakeRuntimeProvider{kind: "node", detected: []providers.RuntimeVersion{
		{Version: "20.11.0", InstallPath: "/usr/local/bin/node", IsDefault: true},
	}}
	php := &fakeRuntimeProvider{kind: "php", detected: []providers.RuntimeVersion{
		{Version: "8.3.1", InstallPath: "/opt/homebrew/bin/php", IsDefault: true},
	}}
	g, _ := newRuntimesTestRouter(t, node, php)

	w := doRequest(g, http.MethodGet, "/api/v1/runtimes", "secret-token")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var out []runtimeView
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 runtimes, got %d: %+v", len(out), out)
	}
	byKind := map[string]runtimeView{}
	for _, rt := range out {
		byKind[rt.Kind] = rt
	}
	if len(byKind["node"].Versions) != 1 || byKind["node"].Versions[0].Version != "20.11.0" {
		t.Fatalf("unexpected node runtime: %+v", byKind["node"])
	}
	if len(byKind["php"].Versions) != 1 || byKind["php"].Versions[0].Version != "8.3.1" {
		t.Fatalf("unexpected php runtime: %+v", byKind["php"])
	}
}

func TestListRuntimesRequiresAuth(t *testing.T) {
	g, _ := newRuntimesTestRouter(t, &fakeRuntimeProvider{kind: "node"})
	w := doRequest(g, http.MethodGet, "/api/v1/runtimes", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestListRuntimesSkipsFailingProviderButKeepsOthers(t *testing.T) {
	broken := &fakeRuntimeProvider{kind: "node", detectErr: errors.New("no node found")}
	ok := &fakeRuntimeProvider{kind: "php", detected: []providers.RuntimeVersion{{Version: "8.3.1", IsDefault: true}}}
	g, _ := newRuntimesTestRouter(t, broken, ok)

	w := doRequest(g, http.MethodGet, "/api/v1/runtimes", "secret-token")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out []runtimeView
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// The broken provider never got a runtimes row created for it (nothing
	// was ever successfully detected); the healthy one should still show up.
	if len(out) != 1 || out[0].Kind != "php" {
		t.Fatalf("expected only php to appear, got %+v", out)
	}
}

func TestDetectRuntimeKindUnknownKind404(t *testing.T) {
	g, _ := newRuntimesTestRouter(t, &fakeRuntimeProvider{kind: "node"})
	w := doRequest(g, http.MethodPost, "/api/v1/runtimes/python/detect", "secret-token")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDetectRuntimeKindWritesAuditEvent(t *testing.T) {
	node := &fakeRuntimeProvider{kind: "node", detected: []providers.RuntimeVersion{{Version: "20.11.0", IsDefault: true}}}
	g, d := newRuntimesTestRouter(t, node)

	w := doRequest(g, http.MethodPost, "/api/v1/runtimes/node/detect", "secret-token")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM audit_events WHERE action = 'runtime.detect' AND target_id = 'node'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("expected at least one runtime.detect audit event")
	}
}

func TestSetDefaultRuntimeVersionUpdatesProviderAndPersists(t *testing.T) {
	node := &fakeRuntimeProvider{kind: "node", detected: []providers.RuntimeVersion{
		{Version: "20.11.0", IsDefault: true},
		{Version: "18.19.0"},
	}}
	g, _ := newRuntimesTestRouter(t, node)

	// Seed detection first so the version row exists to switch.
	if w := doRequest(g, http.MethodGet, "/api/v1/runtimes", "secret-token"); w.Code != http.StatusOK {
		t.Fatalf("seed detect failed: %d %s", w.Code, w.Body.String())
	}

	w := doRequest(g, http.MethodPost, "/api/v1/runtimes/node/versions/18.19.0/default", "secret-token")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(node.setDefaultLog) != 1 || node.setDefaultLog[0] != "18.19.0" {
		t.Fatalf("expected provider.SetDefault called with 18.19.0, got %+v", node.setDefaultLog)
	}

	var rt runtimeView
	if err := json.Unmarshal(w.Body.Bytes(), &rt); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var defaultVersion string
	for _, v := range rt.Versions {
		if v.IsDefault {
			defaultVersion = v.Version
		}
	}
	if defaultVersion != "18.19.0" {
		t.Fatalf("expected 18.19.0 to be the new default, got %+v", rt.Versions)
	}
}

func TestSetDefaultRuntimeVersionProviderErrorDoesNotPersist(t *testing.T) {
	node := &fakeRuntimeProvider{
		kind:          "node",
		detected:      []providers.RuntimeVersion{{Version: "20.11.0", IsDefault: true}},
		setDefaultErr: errors.New("nvm alias default failed"),
	}
	g, _ := newRuntimesTestRouter(t, node)
	if w := doRequest(g, http.MethodGet, "/api/v1/runtimes", "secret-token"); w.Code != http.StatusOK {
		t.Fatalf("seed detect failed: %d %s", w.Code, w.Body.String())
	}

	w := doRequest(g, http.MethodPost, "/api/v1/runtimes/node/versions/20.11.0/default", "secret-token")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when provider.SetDefault fails, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetDefaultRuntimeVersionUnknownProvider404(t *testing.T) {
	g, _ := newRuntimesTestRouter(t)
	w := doRequest(g, http.MethodPost, "/api/v1/runtimes/node/versions/20.11.0/default", "secret-token")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
