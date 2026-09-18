package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/service"
	"github.com/alresiainc/alresia-voltpanel/internal/security"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
	"github.com/alresiainc/alresia-voltpanel/internal/ws"
	"github.com/gin-gonic/gin"
)

func newTestRouter(t *testing.T, token string) (*gin.Engine, Deps) {
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
	hub := ws.NewHub(token, false, session)
	secrets, err := security.NewSecretStore(st.DB(), cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	d := Deps{Store: st, Mgr: service.NewManager(st, hub), Hub: hub, Session: session, Token: token, Dev: false, Secrets: secrets}
	g := gin.New()
	Mount(g, d)
	return g, d
}

// rebuildRouter re-mounts a fresh router over a (possibly mutated) Deps
// value -- needed because Mount's handlers close over Deps by value, so a
// test that adjusts a field after newTestRouter (e.g. git_test.go pointing
// GitHubBaseURL at a fake server) must remount rather than mutate the
// already-built *gin.Engine's captured copy.
func rebuildRouter(d Deps) *gin.Engine {
	g := gin.New()
	Mount(g, d)
	return g
}

func TestUnauthenticatedRequestRejected(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
	g.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestTokenHeaderAuthorizes(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTokenVerifyIssuesSessionCookie(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token/verify", strings.NewReader(`{"token":"secret-token"}`))
	req.Header.Set("Content-Type", "application/json")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := w.Result()
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookieName {
			found = true
			if !c.HttpOnly {
				t.Fatal("session cookie must be HttpOnly")
			}
		}
	}
	if !found {
		t.Fatal("expected a session cookie to be set")
	}

	// The session cookie alone should now authorize a normal API call.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
	for _, c := range resp.Cookies() {
		req2.AddCookie(c)
	}
	g.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected session cookie to authorize, got %d", w2.Code)
	}
}

func TestCrossOriginStateChangeRejected(t *testing.T) {
	g, _ := newTestRouter(t, "secret-token")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/files", strings.NewReader(`{"path":"x.txt","content":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Volt-Token", "secret-token")
	req.Header.Set("Origin", "https://evil.example.com")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected cross-origin state change to be rejected with 403, got %d", w.Code)
	}
}

func TestDeleteFileRequiresConfirm(t *testing.T) {
	g, d := newTestRouter(t, "secret-token")
	if err := d.Store.WriteFile("gone.txt", []byte("bye")); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files?path=gone.txt", nil)
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected delete without confirm=true to be rejected, got %d: %s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/files?path=gone.txt&confirm=true", nil)
	req2.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected confirmed delete to succeed, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestMutatingCallsWriteAuditEvents(t *testing.T) {
	g, d := newTestRouter(t, "secret-token")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/files", strings.NewReader(`{"path":"audited.txt","content":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Volt-Token", "secret-token")
	g.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected write to succeed, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := d.DB().QueryRow(`SELECT COUNT(1) FROM audit_events WHERE action = 'file.write'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit event for file.write, got %d", count)
	}
}
