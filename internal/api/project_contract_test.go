package api

import (
	"errors"
	"testing"

	"github.com/coding-tutor/internal/ai"
)

func TestGeneratedFilesWithCurriculumUsesAuthoritativeCurriculum(t *testing.T) {
	generated := map[string]string{
		"main.go":     "package main",
		"TUTORSYS.md": "provider value",
	}
	got := generatedFilesWithCurriculum(generated, "authoritative value")
	if got["TUTORSYS.md"] != "authoritative value" {
		t.Fatalf("TUTORSYS.md = %q", got["TUTORSYS.md"])
	}
	if generated["TUTORSYS.md"] != "provider value" {
		t.Fatal("input file map was mutated")
	}
}

func TestRequireGeneratedQuizFailsClosed(t *testing.T) {
	generationErr := errors.New("provider failed")
	if _, err := requireGeneratedQuiz(nil, generationErr); !errors.Is(err, generationErr) {
		t.Fatalf("provider error = %v", err)
	}
	if _, err := requireGeneratedQuiz(map[string]ai.QuizItem{}, nil); err == nil {
		t.Fatal("empty required quiz was accepted")
	}

	quiz := map[string]ai.QuizItem{"main.go:hole:0": {Question: "question"}}
	got, err := requireGeneratedQuiz(quiz, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("valid quiz = %#v, %v", got, err)
	}
}

func TestExtractTutorSectionUsesExactHeading(t *testing.T) {
	content := "## 학습 수준 설명\nexperienced\n\n## 학습 수준\nnewbie\n\n## 현재 단계\nStep 2: 확장"
	if got := extractTutorSectionFromContent(content, "학습 수준"); got != "newbie" {
		t.Fatalf("skill level = %q", got)
	}
	if got := extractCurrentStepFromContent(content); got != "Step 2: 확장" {
		t.Fatalf("current step = %q", got)
	}
}
