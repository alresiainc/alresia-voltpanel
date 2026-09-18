package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/extension"
	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/gin-gonic/gin"
)

// extRequest is a local, unexported helper (deliberately not named
// jsonRequest -- a same-named helper already exists in a sibling file from
// a different phase's work and would collide once both land on main).
func extRequest(g *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Volt-Token", token)
	}
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	return rec
}

// repoTestdata mirrors internal/domain/extension's own helper: resolves
// testdata/<name> at the repository root regardless of the test binary's
// working directory.
func repoTestdataPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 20; i++ {
		candidate := filepath.Join(dir, "testdata")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return filepath.Join(candidate, name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate repository testdata/ directory")
	return ""
}

func newExtensionsTestRouter(t *testing.T) (*gin.Engine, Deps) {
	t.Helper()
	_, d := newTestRouter(t, "secret-token")
	d.Providers = providers.NewRegistry()
	d.Extensions = extension.NewRepository(d.DB(), d.Providers)
	g := gin.New()
	Mount(g, d)
	return g, d
}

func TestInstallEnableDisableExtensionEndToEnd(t *testing.T) {
	g, d := newExtensionsTestRouter(t)
	samplePath := repoTestdataPath(t, "sample-python-provider")

	installRec := extRequest(g, http.MethodPost, "/api/v1/extensions", "secret-token", `{"path":"`+samplePath+`"}`)
	if installRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", installRec.Code, installRec.Body.String())
	}
	var ext map[string]any
	if err := json.Unmarshal(installRec.Body.Bytes(), &ext); err != nil {
		t.Fatal(err)
	}
	id := ext["id"].(string)

	// Before enabling, the sample provider's kind must not be registered.
	if _, ok := d.Providers.Runtime(ext["name"].(string)); ok {
		t.Fatal("expected the extension's kind to be unregistered before Enable")
	}

	enableRec := extRequest(g, http.MethodPost, "/api/v1/extensions/"+id+"/enable", "secret-token", "")
	if enableRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", enableRec.Code, enableRec.Body.String())
	}

	kind := ext["name"].(string)
	provider, ok := d.Providers.Runtime(kind)
	if !ok {
		t.Fatalf("expected kind %q to be registered in the shared Registry after Enable", kind)
	}
	// This is the acceptance criterion itself: calling through the plain
	// providers.RuntimeProvider interface, indistinguishable from a
	// built-in provider.
	if _, err := provider.DetectInstalled(t.Context()); err != nil {
		t.Fatalf("DetectInstalled through the registered external provider failed: %v", err)
	}

	disableRec := extRequest(g, http.MethodPost, "/api/v1/extensions/"+id+"/disable", "secret-token", "")
	if disableRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", disableRec.Code, disableRec.Body.String())
	}
	if _, ok := d.Providers.Runtime(kind); ok {
		t.Fatal("expected the extension's kind to be unregistered after Disable")
	}
}

func TestRemoveExtensionRequiresConfirm(t *testing.T) {
	g, _ := newExtensionsTestRouter(t)
	samplePath := repoTestdataPath(t, "sample-python-provider")

	installRec := extRequest(g, http.MethodPost, "/api/v1/extensions", "secret-token", `{"path":"`+samplePath+`"}`)
	var ext map[string]any
	if err := json.Unmarshal(installRec.Body.Bytes(), &ext); err != nil {
		t.Fatal(err)
	}
	id := ext["id"].(string)

	noConfirmRec := extRequest(g, http.MethodDelete, "/api/v1/extensions/"+id, "secret-token", "")
	if noConfirmRec.Code != http.StatusBadRequest {
		t.Fatalf("expected removal without confirm=true to be rejected, got %d", noConfirmRec.Code)
	}

	confirmedRec := extRequest(g, http.MethodDelete, "/api/v1/extensions/"+id+"?confirm=true", "secret-token", "")
	if confirmedRec.Code != http.StatusOK {
		t.Fatalf("expected confirmed removal to succeed, got %d: %s", confirmedRec.Code, confirmedRec.Body.String())
	}
}
