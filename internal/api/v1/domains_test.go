package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/providers/domainprovider/hosts"
	"github.com/gin-gonic/gin"
)

// newDomainsTestRouter mirrors router_test.go's newTestRouter but wires a
// real hosts.Provider pointed at a temp file, never the real
// /etc/hosts -- this exercises the actual DomainProvider implementation,
// not a hand-rolled mock, while staying fully safe to run automatically.
// It also returns the hosts file's own path, so a test can seed it with a
// raw (non-volt-managed) line to simulate a pre-existing foreign entry.
func newDomainsTestRouter(t *testing.T) (*gin.Engine, Deps, string) {
	t.Helper()
	_, d := newTestRouter(t, "secret-token")
	hostsPath := filepath.Join(t.TempDir(), "hosts")
	d.Domains = hosts.NewProvider(hostsPath)
	g := gin.New()
	Mount(g, d)
	return g, d, hostsPath
}

func jsonRequest(g *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Volt-Token", token)
	}
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	return rec
}

func TestCreateAndListDomains(t *testing.T) {
	g, _, _ := newDomainsTestRouter(t)

	rec := jsonRequest(g, http.MethodPost, "/api/v1/domains", "secret-token", `{"hostname":"myapp.test","port":3000}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	listRec := jsonRequest(g, http.MethodGet, "/api/v1/domains", "secret-token", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	var domains []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &domains); err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0]["hostname"] != "myapp.test" {
		t.Fatalf("unexpected domains list: %+v", domains)
	}
}

func TestCreateDomainRejectsHostsFileConflict(t *testing.T) {
	g, _, hostsPath := newDomainsTestRouter(t)
	// Conflicts() only ever flags a *foreign* entry -- a hostname mapped
	// outside VoltPanel's own "volt-managed" lines (per hosts.go's own doc
	// comment, re-Add()-ing an already-managed hostname is an intentional
	// idempotent no-op, not a conflict). Simulate a foreign entry the way
	// some other program (or the user, by hand) would have left it: a
	// plain hosts-file line with no volt-managed marker.
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 taken.test # some-other-tool\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := jsonRequest(g, http.MethodPost, "/api/v1/domains", "secret-token", `{"hostname":"taken.test"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on hosts-file conflict, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteDomainRequiresConfirm(t *testing.T) {
	g, _, _ := newDomainsTestRouter(t)

	createRec := jsonRequest(g, http.MethodPost, "/api/v1/domains", "secret-token", `{"hostname":"deleteme.test"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)

	noConfirmRec := jsonRequest(g, http.MethodDelete, "/api/v1/domains/"+id, "secret-token", "")
	if noConfirmRec.Code != http.StatusBadRequest {
		t.Fatalf("expected delete without confirm=true to be rejected, got %d", noConfirmRec.Code)
	}

	confirmedRec := jsonRequest(g, http.MethodDelete, "/api/v1/domains/"+id+"?confirm=true", "secret-token", "")
	if confirmedRec.Code != http.StatusOK {
		t.Fatalf("expected confirmed delete to succeed, got %d: %s", confirmedRec.Code, confirmedRec.Body.String())
	}
}
