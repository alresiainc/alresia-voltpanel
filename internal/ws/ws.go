package ws

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	clients map[*websocket.Conn]bool
	broadcast chan []byte
	mu sync.Mutex
	token string
	dev bool
}

// NewHub creates a Hub. token is the shared secret clients must present in
// their first WebSocket message before being allowed to join; dev disables
// that check (mirrors the HTTP API's dev-mode auth bypass).
func NewHub(token string, dev bool) *Hub {
	return &Hub{clients: map[*websocket.Conn]bool{}, broadcast: make(chan []byte, 1024), token: token, dev: dev}
}

func (h *Hub) Run() {
	for msg := range h.broadcast {
		h.mu.Lock()
		for c := range h.clients {
			_ = c.WriteMessage(websocket.TextMessage, msg)
		}
		h.mu.Unlock()
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // auth handled post-upgrade, see authenticate()
}

type authMessage struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

// ServeWs upgrades the connection and, unless the hub is in dev mode,
// requires the client's first message to be {"type":"auth","token":"..."}
// matching the hub's token. Browsers cannot set custom headers on a
// WebSocket handshake, so auth has to happen over the socket itself rather
// than via a request header.
func ServeWs(h *Hub, w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }

	if !h.authenticate(c) {
		_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "unauthorized"))
		_ = c.Close()
		return
	}

	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	c.SetCloseHandler(func(code int, text string) error {
		h.mu.Lock(); delete(h.clients, c); h.mu.Unlock(); return nil
	})

	// gorilla/websocket requires an active reader for control frames (pings,
	// close) to be processed at all; without this loop disconnected clients
	// were never pruned from h.clients.
	go func() {
		defer func() { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }()
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

func (h *Hub) authenticate(c *websocket.Conn) bool {
	if h.dev {
		return true
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer c.SetReadDeadline(time.Time{})

	_, msg, err := c.ReadMessage()
	if err != nil {
		return false
	}
	var auth authMessage
	if err := json.Unmarshal(msg, &auth); err != nil {
		return false
	}
	if auth.Type != "auth" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(auth.Token), []byte(h.token)) == 1
}

func (h *Hub) Emit(b []byte) { select { case h.broadcast <- b: default: } }
