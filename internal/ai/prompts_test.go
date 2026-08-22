package ai

import (
	"strings"
	"testing"
)

func TestCodePromptRequiresExplicitMarkerEnd(t *testing.T) {
	if !strings.Contains(codeFilesSystemPrompt, "[TUTOR:END]") {
		t.Fatal("code generation system prompt does not require [TUTOR:END]")
	}
	if !strings.Contains(codeFilesSystemPrompt, "테스트 파일에는") || !strings.Contains(codeFilesSystemPrompt, "절대 넣지 마세요") {
		t.Fatal("code generation system prompt does not prohibit tutor markers in tests")
	}
	if strings.Contains(feedbackSystemPrompt("normal"), "[STEP_COMPLETE]") {
		t.Fatal("feedback prompt still delegates completion state to the model")
	}
}

func TestFeedbackPromptDoesNotRepeatPassedTestAction(t *testing.T) {
	if !strings.Contains(feedbackSystemPrompt("newbie"), "같은 테스트를 다시 실행하라고 하지 마세요") {
		t.Fatal("feedback prompt does not prevent redundant test rerun advice")
	}
}
