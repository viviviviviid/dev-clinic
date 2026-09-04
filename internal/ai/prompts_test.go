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

func TestTopicPromptsRequirePlayfulSemanticDiversity(t *testing.T) {
	for name, prompt := range map[string]string{
		"topics": topicsSystemPrompt,
		"nurse":  nurseSystemPrompt,
	} {
		if !strings.Contains(prompt, "이름") || !strings.Contains(prompt, "유사 프로젝트") {
			t.Fatalf("%s prompt does not reject renamed topic variants", name)
		}
		if !strings.Contains(prompt, "투두") || !strings.Contains(prompt, "creativeLens") {
			t.Fatalf("%s prompt does not require a varied creative direction", name)
		}
		if !strings.Contains(prompt, "현실 사례") || !strings.Contains(prompt, "게임") || !strings.Contains(prompt, "이론") {
			t.Fatalf("%s prompt does not mix structural, real-world, and game-based learning", name)
		}
	}
}

func TestTopicCreativeLensRotatesWithExcludedTopics(t *testing.T) {
	first := topicCreativeLens(nil)
	second := topicCreativeLens([]string{"우주선 고장 탐정", "픽셀 생태계", "버그 경매장"})
	if first == second {
		t.Fatal("creative lens did not rotate after remembering a recommendation set")
	}
	if got := topicCreativeLens([]string{"우주선 고장 탐정", "픽셀 생태계", "버그 경매장"}); got != second {
		t.Fatalf("creative lens is not deterministic: got %q, want %q", got, second)
	}
}
