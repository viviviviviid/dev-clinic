package ai

import (
	"strings"
	"testing"
)

func TestCodePromptRequiresExplicitMarkerEnd(t *testing.T) {
	if !strings.Contains(codeFilesSystemPrompt, "[TUTOR:END]") {
		t.Fatal("code generation system prompt does not require [TUTOR:END]")
	}
	if strings.Contains(feedbackSystemPrompt("normal"), "[STEP_COMPLETE]") {
		t.Fatal("feedback prompt still delegates completion state to the model")
	}
}
