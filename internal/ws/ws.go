package ws

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type Hub struct {
	clients map[*websocket.Conn]bool
	broadcast chan []byte
	mu sync.Mutex
}

func NewHub() *Hub {
	return &Hub{clients: map[*websocket.Conn]bool{}, broadcast: make(chan []byte, 1024)}
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
	CheckOrigin: func(r *http.Request) bool { return true }, // auth handled upstream
}

func ServeWs(h *Hub, w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	c.SetCloseHandler(func(code int, text string) error {
		h.mu.Lock(); delete(h.clients, c); h.mu.Unlock(); return nil
	})
}

func (h *Hub) Emit(b []byte) { select { case h.broadcast <- b: default: } }
