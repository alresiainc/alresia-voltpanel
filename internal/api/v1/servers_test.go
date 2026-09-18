package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/gin-gonic/gin"
)

// srvRequest is a local, unexported helper (deliberately not named
// jsonRequest -- a same-named helper already exists in a sibling file from
// a different phase's work and would collide once both land on main).
func srvRequest(g *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Volt-Token", token)
	}
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	return rec
}

// fakeRemoteSession/fakeRemoteProvider let router tests exercise the full
// HTTP path (routing, auth, confirm-gating, secret storage/cleanup, audit
// logging) without an actual network connection -- the real protocol-level
// behavior is already covered by internal/providers/remote/ssh's own tests
// against an in-process fake SSH server.
type fakeRemoteSession struct {
	execOut      string
	metrics      providers.RemoteMetrics
	writeFileErr error
	writtenPath  string
	writtenData  []byte
}

func (f *fakeRemoteSession) Exec(ctx context.Context, command string) ([]byte, []byte, error) {
	return []byte(f.execOut), nil, nil
}
func (f *fakeRemoteSession) ListDir(ctx context.Context, path string) ([]providers.RemoteFileInfo, error) {
	return []providers.RemoteFileInfo{{Name: "file.txt"}}, nil
}
func (f *fakeRemoteSession) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return []byte("file contents"), nil
}
func (f *fakeRemoteSession) WriteFile(ctx context.Context, path string, data []byte) error {
	f.writtenPath, f.writtenData = path, data
	return f.writeFileErr
}
func (f *fakeRemoteSession) Metrics(ctx context.Context) (providers.RemoteMetrics, error) {
	return f.metrics, nil
}
func (f *fakeRemoteSession) Close() error { return nil }

type fakeRemoteProvider struct {
	session    *fakeRemoteSession
	connectErr error
}

func (f *fakeRemoteProvider) Connect(ctx context.Context, s providers.Server) (providers.RemoteSession, error) {
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	return f.session, nil
}

func newServersTestRouter(t *testing.T, remote *fakeRemoteProvider) (*gin.Engine, Deps) {
	t.Helper()
	_, d := newTestRouter(t, "secret-token")
	d.Remote = remote
	secrets, err := security.NewSecretStore(d.DB(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d.Secrets = secrets
	g := gin.New()
	Mount(g, d)
	return g, d
}

func TestCreateListDeleteServerAgentAuth(t *testing.T) {
	g, _ := newServersTestRouter(t, &fakeRemoteProvider{session: &fakeRemoteSession{}})

	createRec := srvRequest(g, http.MethodPost, "/api/v1/servers", "secret-token", `{"name":"vps1","hostname":"1.2.3.4","port":22,"username":"deploy","authMethod":"agent"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)

	listRec := srvRequest(g, http.MethodGet, "/api/v1/servers", "secret-token", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	noConfirmRec := srvRequest(g, http.MethodDelete, "/api/v1/servers/"+id, "secret-token", "")
	if noConfirmRec.Code != http.StatusBadRequest {
		t.Fatalf("expected delete without confirm=true to be rejected, got %d", noConfirmRec.Code)
	}
	confirmedRec := srvRequest(g, http.MethodDelete, "/api/v1/servers/"+id+"?confirm=true", "secret-token", "")
	if confirmedRec.Code != http.StatusOK {
		t.Fatalf("expected confirmed delete to succeed, got %d: %s", confirmedRec.Code, confirmedRec.Body.String())
	}
}

func TestCreateServerWithKeyStoresAndDeletesSecret(t *testing.T) {
	g, d := newServersTestRouter(t, &fakeRemoteProvider{session: &fakeRemoteSession{}})

	createRec := srvRequest(g, http.MethodPost, "/api/v1/servers", "secret-token",
		`{"name":"vps2","hostname":"1.2.3.4","port":22,"username":"deploy","authMethod":"key","key":"-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----\n"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	// The raw key must never appear in the response.
	if body := createRec.Body.String(); containsKeyMaterial(body) {
		t.Fatalf("response leaked key material: %s", body)
	}
	id := created["id"].(string)

	var secretCount int
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM secrets WHERE owner_id = ?`, id).Scan(&secretCount); err != nil {
		t.Fatal(err)
	}
	if secretCount != 1 {
		t.Fatalf("expected exactly 1 secret row stored for the new server, got %d", secretCount)
	}

	if rec := srvRequest(g, http.MethodDelete, "/api/v1/servers/"+id+"?confirm=true", "secret-token", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected confirmed delete to succeed, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM secrets WHERE owner_id = ?`, id).Scan(&secretCount); err != nil {
		t.Fatal(err)
	}
	if secretCount != 0 {
		t.Fatal("expected the server's secret to be deleted along with the server row")
	}
}

func containsKeyMaterial(s string) bool {
	return len(s) > 0 && (contains(s, "BEGIN OPENSSH PRIVATE KEY") || contains(s, "fake\\n"))
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestExecServerAuditsCommandAndReturnsOutput(t *testing.T) {
	g, d := newServersTestRouter(t, &fakeRemoteProvider{session: &fakeRemoteSession{execOut: "hello"}})

	createRec := srvRequest(g, http.MethodPost, "/api/v1/servers", "secret-token", `{"name":"vps3","hostname":"1.2.3.4","port":22,"username":"deploy"}`)
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)

	execRec := srvRequest(g, http.MethodPost, "/api/v1/servers/"+id+"/exec", "secret-token", `{"command":"echo hello"}`)
	if execRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", execRec.Code, execRec.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(execRec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["stdout"] != "hello" {
		t.Fatalf("unexpected exec result: %+v", result)
	}

	var count int
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM audit_events WHERE action = 'server.exec' AND metadata_json LIKE '%echo hello%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit event recording the exec'd command, got %d", count)
	}
}

func TestServerConnectFailureReturnsBadGateway(t *testing.T) {
	g, _ := newServersTestRouter(t, &fakeRemoteProvider{connectErr: context.DeadlineExceeded})

	createRec := srvRequest(g, http.MethodPost, "/api/v1/servers", "secret-token", `{"name":"vps4","hostname":"1.2.3.4","port":22,"username":"deploy"}`)
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)

	rec := srvRequest(g, http.MethodPost, "/api/v1/servers/"+id+"/test", "secret-token", "")
	if rec.Code != http.StatusBadGateway && rec.Code != http.StatusBadRequest {
		t.Fatalf("expected a connect failure to surface as a client-visible error, got %d: %s", rec.Code, rec.Body.String())
	}
}
