package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func dial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestServeWsRejectsBadAuth(t *testing.T) {
	hub := NewHub("secret", false)
	go hub.Run()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	defer srv.Close()

	c := dial(t, srv)
	defer c.Close()

	if err := c.WriteJSON(map[string]string{"type": "auth", "token": "wrong"}); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("expected connection to be closed for bad token")
	}
}

func TestServeWsAcceptsGoodAuthAndBroadcasts(t *testing.T) {
	hub := NewHub("secret", false)
	go hub.Run()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	defer srv.Close()

	c := dial(t, srv)
	defer c.Close()

	if err := c.WriteJSON(map[string]string{"type": "auth", "token": "secret"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // let the server register the client

	payload, _ := json.Marshal(map[string]string{"type": "log", "data": "hello"})
	hub.Emit(payload)

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(msg), "hello") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestServeWsDevModeSkipsAuth(t *testing.T) {
	hub := NewHub("secret", true)
	go hub.Run()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	defer srv.Close()

	c := dial(t, srv)
	defer c.Close()
	time.Sleep(100 * time.Millisecond)

	payload, _ := json.Marshal(map[string]string{"type": "log", "data": "devhello"})
	hub.Emit(payload)

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(msg), "devhello") {
		t.Fatalf("unexpected message: %s", msg)
	}
}
