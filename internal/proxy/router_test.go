package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterProxiesToRegisteredTarget(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello from backend"))
	}))
	defer backend.Close()

	r := NewRouter()
	r.Set("myapp.test", backend.Listener.Addr().String())

	front := httptest.NewServer(r.Handler())
	defer front.Close()

	req, _ := http.NewRequest("GET", front.URL, nil)
	req.Host = "myapp.test:7080"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from backend" {
		t.Fatalf("expected proxied body, got %q", body)
	}
}

func TestRouterUnknownHostIs404(t *testing.T) {
	r := NewRouter()
	front := httptest.NewServer(r.Handler())
	defer front.Close()

	req, _ := http.NewRequest("GET", front.URL, nil)
	req.Host = "unknown.test"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unbound host, got %d", resp.StatusCode)
	}
}

func TestRouterRemove(t *testing.T) {
	r := NewRouter()
	r.Set("myapp.test", "127.0.0.1:9999")
	if _, ok := r.Lookup("myapp.test"); !ok {
		t.Fatalf("expected lookup to succeed after Set")
	}
	r.Remove("myapp.test")
	if _, ok := r.Lookup("myapp.test"); ok {
		t.Fatalf("expected lookup to fail after Remove")
	}
}
