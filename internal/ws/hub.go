package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Message struct {
	Type     string `json:"type"`
	Content  string `json:"content,omitempty"`
	LastSync string `json:"last_sync,omitempty"`
	Changed  bool   `json:"changed,omitempty"`
	Error    string `json:"error,omitempty"`
	Passed   bool   `json:"passed,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
}

var Global = &Hub{
	clients: make(map[*websocket.Conn]bool),
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	h.mu.Lock()
	h.clients[conn] = true
	h.mu.Unlock()

	log.Println("ws: client connected")

	defer func() {
		h.mu.Lock()
		delete(h.clients, conn)
		h.mu.Unlock()
		conn.Close()
		log.Println("ws: client disconnected")
	}()

	// Keep alive ping
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (h *Hub) Broadcast(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("ws write error: %v", err)
		}
	}
}

func (h *Hub) BroadcastSyncStatus(changed bool) {
	h.Broadcast(Message{
		Type:    "sync_status",
		LastSync: time.Now().Format(time.RFC3339),
		Changed: changed,
	})
}

func (h *Hub) BroadcastFeedbackStart() {
	h.Broadcast(Message{Type: "feedback_start"})
}

func (h *Hub) BroadcastFeedbackChunk(chunk string) {
	h.Broadcast(Message{Type: "feedback_chunk", Content: chunk})
}

func (h *Hub) BroadcastFeedbackEnd() {
	h.Broadcast(Message{Type: "feedback_end"})
}

func (h *Hub) BroadcastError(errMsg string) {
	h.Broadcast(Message{Type: "error", Error: errMsg})
}

func (h *Hub) BroadcastTestResult(passed bool, summary string) {
	h.Broadcast(Message{Type: "test_result", Passed: passed, Summary: summary})
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
