package ws

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestStepCompleteMessageSerializesFalse(t *testing.T) {
	data, err := json.Marshal(Message{Type: "step_complete", Passed: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"passed":false`) {
		t.Fatalf("step_complete false was omitted: %s", data)
	}
}

func TestMessageSerializesProjectScope(t *testing.T) {
	data, err := json.Marshal(Message{Type: "feedback_start", ProjectDir: "/projects/current"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"project_dir":"/projects/current"`) {
		t.Fatalf("project scope was omitted: %s", data)
	}
}

func TestTestResultSerializesScope(t *testing.T) {
	data, err := json.Marshal(Message{Type: "test_result", Passed: false, TestScope: "targeted", TestInputHash: "input-v1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"test_scope":"targeted"`) {
		t.Fatalf("test scope was omitted: %s", data)
	}
	if !strings.Contains(string(data), `"test_input_hash":"input-v1"`) {
		t.Fatalf("test input hash was omitted: %s", data)
	}
}

func TestReviewMessageSerializesRevisionMetadata(t *testing.T) {
	data, err := json.Marshal(Message{
		Type:         "review_ready",
		ProjectDir:   "/projects/current",
		Revision:     7,
		SemanticHash: "abc",
		Files:        []string{"src/index.ts"},
		Status:       "ready",
		SessionID:    9,
		RequestID:    "request-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"revision":7`, `"semantic_hash":"abc"`, `"files":["src/index.ts"]`, `"status":"ready"`, `"session_id":9`, `"request_id":"request-7"`} {
		if !strings.Contains(string(data), field) {
			t.Errorf("review metadata %s missing from %s", field, data)
		}
	}
}

func TestProjectSessionRegistrationIsConditionalOnEpoch(t *testing.T) {
	hub := &Hub{clients: make(map[*websocket.Conn]*client)}
	hub.SetProjectSession("/projects/current", 10)
	if got := hub.ProjectSessionID("/projects/current"); got != 10 {
		t.Fatalf("session = %d, want 10", got)
	}
	hub.ClearProjectSession("/projects/current", 9)
	if got := hub.ProjectSessionID("/projects/current"); got != 10 {
		t.Fatalf("old epoch cleared current session: %d", got)
	}
	hub.ClearProjectSession("/projects/current", 10)
	if got := hub.ProjectSessionID("/projects/current"); got != 0 {
		t.Fatalf("current epoch remained registered: %d", got)
	}
}

func TestSessionChangedMessageCarriesAuthoritativeReset(t *testing.T) {
	hub := &Hub{clients: make(map[*websocket.Conn]*client)}
	hub.SetProjectSession("/projects/current", 11)
	msg := hub.messageForProject("/projects/current", Message{Type: "session_changed", Changed: true, Status: "idle"})
	if msg.SessionID != 11 || msg.ProjectDir != "/projects/current" || !msg.Changed || msg.Status != "idle" {
		t.Fatalf("session reset message = %#v", msg)
	}
}
