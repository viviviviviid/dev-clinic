package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type       string `json:"type"`
	Content    string `json:"content,omitempty"`
	LastSync   string `json:"last_sync,omitempty"`
	Changed    bool   `json:"changed,omitempty"`
	Error      string `json:"error,omitempty"`
	Passed     bool   `json:"passed"`
	Summary    string `json:"summary,omitempty"`
	ProjectDir string `json:"project_dir,omitempty"`
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]*client
}

type client struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

var Global = &Hub{
	clients: make(map[*websocket.Conn]*client),
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, _, err := Upgrade(w, r)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	c := &client{conn: conn}
	conn.SetReadLimit(64 << 10)
	h.mu.Lock()
	h.clients[conn] = c
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
	clients := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		c.writeMu.Lock()
		_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := c.conn.WriteMessage(websocket.TextMessage, data)
		c.writeMu.Unlock()
		if err != nil {
			log.Printf("ws write error: %v", err)
		}
	}
}

func (h *Hub) BroadcastSyncStatus(changed bool) {
	h.BroadcastSyncStatusForProject("", changed)
}

func (h *Hub) BroadcastSyncStatusForProject(projectDir string, changed bool) {
	h.Broadcast(Message{
		Type:       "sync_status",
		LastSync:   time.Now().Format(time.RFC3339),
		Changed:    changed,
		ProjectDir: projectDir,
	})
}

func (h *Hub) BroadcastFeedbackStart() {
	h.BroadcastFeedbackStartForProject("")
}

func (h *Hub) BroadcastFeedbackStartForProject(projectDir string) {
	h.Broadcast(Message{Type: "feedback_start", ProjectDir: projectDir})
}

func (h *Hub) BroadcastFeedbackChunk(chunk string) {
	h.BroadcastFeedbackChunkForProject("", chunk)
}

func (h *Hub) BroadcastFeedbackChunkForProject(projectDir, chunk string) {
	h.Broadcast(Message{Type: "feedback_chunk", Content: chunk, ProjectDir: projectDir})
}

func (h *Hub) BroadcastFeedbackEnd() {
	h.BroadcastFeedbackEndForProject("")
}

func (h *Hub) BroadcastFeedbackEndForProject(projectDir string) {
	h.Broadcast(Message{Type: "feedback_end", ProjectDir: projectDir})
}

func (h *Hub) BroadcastError(errMsg string) {
	h.BroadcastErrorForProject("", errMsg)
}

func (h *Hub) BroadcastErrorForProject(projectDir, errMsg string) {
	h.Broadcast(Message{Type: "error", Error: errMsg, ProjectDir: projectDir})
}

func (h *Hub) BroadcastTestResult(passed bool, summary string) {
	h.BroadcastTestResultForProject("", passed, summary)
}

func (h *Hub) BroadcastTestResultForProject(projectDir string, passed bool, summary string) {
	h.Broadcast(Message{Type: "test_result", Passed: passed, Summary: summary, ProjectDir: projectDir})
}

func (h *Hub) BroadcastStepComplete(passed bool) {
	h.BroadcastStepCompleteForProject("", passed)
}

func (h *Hub) BroadcastStepCompleteForProject(projectDir string, passed bool) {
	h.Broadcast(Message{Type: "step_complete", Passed: passed, ProjectDir: projectDir})
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
