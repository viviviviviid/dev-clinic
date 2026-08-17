package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type          string   `json:"type"`
	Content       string   `json:"content,omitempty"`
	LastSync      string   `json:"last_sync,omitempty"`
	Changed       bool     `json:"changed,omitempty"`
	Error         string   `json:"error,omitempty"`
	Passed        bool     `json:"passed"`
	Summary       string   `json:"summary,omitempty"`
	ProjectDir    string   `json:"project_dir,omitempty"`
	Revision      uint64   `json:"revision"`
	SemanticHash  string   `json:"semantic_hash,omitempty"`
	Files         []string `json:"files,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Status        string   `json:"status,omitempty"`
	TestScope     string   `json:"test_scope,omitempty"`
	TestInputHash string   `json:"test_input_hash,omitempty"`
	SessionID     uint64   `json:"session_id,omitempty"`
	RequestID     string   `json:"request_id,omitempty"`
}

type Hub struct {
	mu              sync.RWMutex
	clients         map[*websocket.Conn]*client
	projectSessions map[string]uint64
}

type client struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

var Global = &Hub{
	clients:         make(map[*websocket.Conn]*client),
	projectSessions: make(map[string]uint64),
}

func (h *Hub) SetProjectSession(projectDir string, sessionID uint64) {
	if h == nil || projectDir == "" || sessionID == 0 {
		return
	}
	h.mu.Lock()
	if h.projectSessions == nil {
		h.projectSessions = make(map[string]uint64)
	}
	h.projectSessions[filepath.Clean(projectDir)] = sessionID
	h.mu.Unlock()
}

func (h *Hub) ClearProjectSession(projectDir string, sessionID uint64) {
	if h == nil || projectDir == "" {
		return
	}
	h.mu.Lock()
	key := filepath.Clean(projectDir)
	if h.projectSessions[key] == sessionID {
		delete(h.projectSessions, key)
	}
	h.mu.Unlock()
}

func (h *Hub) ProjectSessionID(projectDir string) uint64 {
	if h == nil || projectDir == "" {
		return 0
	}
	h.mu.RLock()
	sessionID := h.projectSessions[filepath.Clean(projectDir)]
	h.mu.RUnlock()
	return sessionID
}

func (h *Hub) messageForProject(projectDir string, msg Message) Message {
	msg.ProjectDir = projectDir
	msg.SessionID = h.ProjectSessionID(projectDir)
	return msg
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
	h.Broadcast(h.messageForProject(projectDir, Message{
		Type:     "sync_status",
		LastSync: time.Now().Format(time.RFC3339),
		Changed:  changed,
	}))
}

// BroadcastSessionChangedForProject is an authoritative reset frame for every
// tab that still has the previous watcher epoch open. Mutating project flows
// may establish a new baseline without producing a later filesystem event.
func (h *Hub) BroadcastSessionChangedForProject(projectDir string) {
	h.Broadcast(h.messageForProject(projectDir, Message{
		Type:    "session_changed",
		Changed: true,
		Status:  "idle",
	}))
}

func (h *Hub) BroadcastFeedbackStart() {
	h.BroadcastFeedbackStartForProject("")
}

func (h *Hub) BroadcastFeedbackStartForProject(projectDir string) {
	h.Broadcast(h.messageForProject(projectDir, Message{Type: "feedback_start"}))
}

func (h *Hub) BroadcastFeedbackStartForReview(projectDir string, revision uint64, semanticHash string, files []string, requestID string) {
	h.Broadcast(h.messageForProject(projectDir, Message{Type: "feedback_start", Revision: revision, SemanticHash: semanticHash, Files: files, RequestID: requestID}))
}

func (h *Hub) BroadcastFeedbackChunk(chunk string) {
	h.BroadcastFeedbackChunkForProject("", chunk)
}

func (h *Hub) BroadcastFeedbackChunkForProject(projectDir, chunk string) {
	h.Broadcast(h.messageForProject(projectDir, Message{Type: "feedback_chunk", Content: chunk}))
}

func (h *Hub) BroadcastFeedbackChunkForReview(projectDir string, revision uint64, semanticHash string, files []string, requestID, chunk string) {
	h.Broadcast(h.messageForProject(projectDir, Message{Type: "feedback_chunk", Content: chunk, Revision: revision, SemanticHash: semanticHash, Files: files, RequestID: requestID}))
}

func (h *Hub) BroadcastFeedbackEnd() {
	h.BroadcastFeedbackEndForProject("")
}

func (h *Hub) BroadcastFeedbackEndForProject(projectDir string) {
	h.Broadcast(h.messageForProject(projectDir, Message{Type: "feedback_end"}))
}

func (h *Hub) BroadcastFeedbackEndForReview(projectDir string, revision uint64, semanticHash string, files []string, requestID string) {
	h.Broadcast(h.messageForProject(projectDir, Message{Type: "feedback_end", Revision: revision, SemanticHash: semanticHash, Files: files, RequestID: requestID}))
}

func (h *Hub) BroadcastError(errMsg string) {
	h.BroadcastErrorForProject("", errMsg)
}

func (h *Hub) BroadcastErrorForProject(projectDir, errMsg string) {
	h.Broadcast(h.messageForProject(projectDir, Message{Type: "error", Error: errMsg}))
}

func (h *Hub) BroadcastFeedbackErrorForReview(projectDir string, revision uint64, semanticHash string, files []string, requestID, errMsg string) {
	h.Broadcast(h.messageForProject(projectDir, Message{Type: "error", Error: errMsg, Revision: revision, SemanticHash: semanticHash, Files: files, RequestID: requestID}))
}

func (h *Hub) BroadcastReviewEvent(eventType, projectDir string, revision uint64, semanticHash string, files []string, status, reason, errMsg, requestID string) {
	h.Broadcast(h.messageForProject(projectDir, Message{
		Type:         eventType,
		Revision:     revision,
		SemanticHash: semanticHash,
		Files:        files,
		Status:       status,
		Reason:       reason,
		Error:        errMsg,
		RequestID:    requestID,
	}))
}

func (h *Hub) BroadcastTestResult(passed bool, summary string) {
	h.BroadcastTestResultForProject("", passed, summary)
}

func (h *Hub) BroadcastTestResultForProject(projectDir string, passed bool, summary string) {
	h.BroadcastTestResultForProjectWithScope(projectDir, passed, summary, "full")
}

func (h *Hub) BroadcastTestResultForProjectWithScope(projectDir string, passed bool, summary, scope string) {
	h.BroadcastTestResultForProjectWithScopeAndInputHash(projectDir, passed, summary, scope, "")
}

func (h *Hub) BroadcastTestResultForProjectWithInputHash(projectDir string, passed bool, summary, inputHash string) {
	h.BroadcastTestResultForProjectWithScopeAndInputHash(projectDir, passed, summary, "full", inputHash)
}

func (h *Hub) BroadcastTestResultForProjectWithScopeAndInputHash(projectDir string, passed bool, summary, scope, inputHash string) {
	if scope != "targeted" {
		scope = "full"
	}
	h.Broadcast(h.messageForProject(projectDir, Message{
		Type:          "test_result",
		Passed:        passed,
		Summary:       summary,
		TestScope:     scope,
		TestInputHash: inputHash,
	}))
}

func (h *Hub) BroadcastStepComplete(passed bool) {
	h.BroadcastStepCompleteForProject("", passed)
}

func (h *Hub) BroadcastStepCompleteForProject(projectDir string, passed bool) {
	h.Broadcast(h.messageForProject(projectDir, Message{Type: "step_complete", Passed: passed}))
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
