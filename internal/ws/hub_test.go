package ws

import (
	"encoding/json"
	"strings"
	"testing"
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
