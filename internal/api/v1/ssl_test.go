package v1

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/providers/ssl/localca"
	"github.com/gin-gonic/gin"
)

// newSSLTestRouter wires a real localca.Provider pointed at a temp dir --
// generating an actual CA and signing actual leaf certificates, just never
// under the daemon's real config dir and never calling TrustCA for real.
func newSSLTestRouter(t *testing.T) (*gin.Engine, Deps) {
	t.Helper()
	_, d := newTestRouter(t, "secret-token")
	d.SSL = localca.NewWithDir(t.TempDir())
	g := gin.New()
	Mount(g, d)
	return g, d
}

func TestEnsureCACreatesRealCA(t *testing.T) {
	g, _ := newSSLTestRouter(t)
	rec := jsonRequest(g, http.MethodPost, "/api/v1/ssl/ca/ensure", "secret-token", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var info map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info["trusted"] != false {
		t.Fatalf("a freshly generated CA must not report as trusted: %+v", info)
	}
}

func TestIssueCertificateForRegisteredDomain(t *testing.T) {
	g, _ := newSSLTestRouter(t)

	domainRec := jsonRequest(g, http.MethodPost, "/api/v1/domains", "secret-token", `{"hostname":"secure.test"}`)
	if domainRec.Code != http.StatusCreated {
		// No DomainProvider wired in this router -- Domains is nil, so
		// createDomain skips the hosts-file step entirely and just
		// persists the DB row; that's fine, SSL issuance only needs the
		// domain's hostname from the repo, not a real hosts-file entry.
		t.Fatalf("expected 201, got %d: %s", domainRec.Code, domainRec.Body.String())
	}
	var domain map[string]any
	if err := json.Unmarshal(domainRec.Body.Bytes(), &domain); err != nil {
		t.Fatal(err)
	}
	domainID := domain["id"].(string)

	if rec := jsonRequest(g, http.MethodPost, "/api/v1/ssl/ca/ensure", "secret-token", ""); rec.Code != http.StatusOK {
		t.Fatalf("ensure CA failed: %d %s", rec.Code, rec.Body.String())
	}

	certRec := jsonRequest(g, http.MethodPost, "/api/v1/ssl/certificates", "secret-token", `{"domainId":"`+domainID+`"}`)
	if certRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", certRec.Code, certRec.Body.String())
	}
}

func TestIssueCertificateUnknownDomain(t *testing.T) {
	g, _ := newSSLTestRouter(t)
	rec := jsonRequest(g, http.MethodPost, "/api/v1/ssl/certificates", "secret-token", `{"domainId":"nonexistent"}`)
	if rec.Code == http.StatusCreated {
		t.Fatalf("expected issuing a cert for an unknown domain to fail, got 201")
	}
}

func TestTrustCARefusesWithoutConfirmation(t *testing.T) {
	g, _ := newSSLTestRouter(t)

	rec := jsonRequest(g, http.MethodPost, "/api/v1/ssl/ca/trust", "secret-token", `{"confirmed":false}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected trust-without-confirmation to be rejected with 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "confirmed") {
		t.Fatalf("expected the error to explain the confirmation requirement, got: %s", rec.Body.String())
	}

	// Also verify an entirely missing body (no "confirmed" key at all)
	// is treated as unconfirmed, not as an error that accidentally lets
	// the request through.
	rec2 := jsonRequest(g, http.MethodPost, "/api/v1/ssl/ca/trust", "secret-token", `{}`)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected a missing confirmed field to be treated as unconfirmed, got %d", rec2.Code)
	}
}
