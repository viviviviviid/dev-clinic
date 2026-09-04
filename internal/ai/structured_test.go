package ai

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeStrictJSON(t *testing.T) {
	var got struct {
		Name string `json:"name"`
	}
	if err := decodeStrictJSON("```json\n{\"name\":\"ok\"}\n```", &got); err != nil {
		t.Fatalf("valid fenced JSON: %v", err)
	}
	if got.Name != "ok" {
		t.Fatalf("name = %q", got.Name)
	}
	if err := decodeStrictJSON(`{"name":"ok","extra":true}`, &got); err == nil {
		t.Fatal("unknown field was accepted")
	}
	if err := decodeStrictJSON(`{"name":"ok"} {"name":"again"}`, &got); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
	if err := decodeStrictJSON("```json\n{\"name\":\"ok\"}\n```\nignored", &got); err == nil {
		t.Fatal("an unmatched fence with trailing text was accepted")
	}
}

func TestValidateGeneratedFiles(t *testing.T) {
	valid := codeFilesResponse{Files: []generatedFile{
		{Path: "cmd/app/main.go", Content: "package main"},
		{Path: "cmd/app/main_test.go", Content: "package main"},
	}}
	files, err := validateGeneratedFiles(valid)
	if err != nil {
		t.Fatalf("valid files: %v", err)
	}
	if len(files) != 2 || files["cmd/app/main.go"] != "package main" {
		t.Fatalf("unexpected files: %#v", files)
	}

	tests := []struct {
		name string
		path string
	}{
		{"parent traversal", "../escape.go"},
		{"absolute", "/tmp/escape.go"},
		{"windows absolute", "C:/tmp/escape.go"},
		{"backslash", `dir\escape.go`},
		{"control character", "dir/bad\nname.go"},
		{"non canonical", "a/../escape.go"},
		{"hidden directory", ".git/config.json"},
		{"executable extension", "run.sh"},
		{"missing extension", "Makefile"},
		{"reserved curriculum", "TUTORSYS.md"},
		{"nested reserved quiz", "state/QUIZ.JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateGeneratedFiles(codeFilesResponse{Files: []generatedFile{{Path: tt.path, Content: "x"}}})
			if err == nil {
				t.Fatalf("unsafe path %q was accepted", tt.path)
			}
		})
	}
}

func TestValidateGeneratedFilesRejectsDuplicateAndLimits(t *testing.T) {
	_, err := validateGeneratedFiles(codeFilesResponse{Files: []generatedFile{
		{Path: "Main.go", Content: "a"},
		{Path: "main.go", Content: "b"},
	}})
	if err == nil {
		t.Fatal("case-insensitive duplicate was accepted")
	}
	_, err = validateGeneratedFiles(codeFilesResponse{Files: []generatedFile{
		{Path: "main.go", Content: strings.Repeat("x", maxGeneratedFileBytes+1)},
	}})
	if err == nil {
		t.Fatal("oversized file was accepted")
	}
}

func TestValidateGeneratedFilesAllowsGoModuleManifest(t *testing.T) {
	files, err := validateGeneratedFiles(codeFilesResponse{Files: []generatedFile{{Path: "go.mod", Content: "module lesson"}}})
	if err != nil {
		t.Fatalf("go.mod was rejected: %v", err)
	}
	if files["go.mod"] != "module lesson" {
		t.Fatalf("go.mod content = %q", files["go.mod"])
	}
}

func TestValidateQuizMatchesMarkers(t *testing.T) {
	markers := []quizMarker{{Key: "main.go:hole:0", Filename: "main.go", MarkerType: "hole", Index: 0}}
	valid := quizResponse{Items: []QuizItem{{
		Key: "main.go:hole:0", Filename: "main.go", MarkerType: "hole", MarkerIndex: 0,
		Question: "무엇을 구현해야 하나요?", Hints: []string{"개념", "구조", "API"},
	}}}
	if _, err := validateQuiz(valid, markers); err != nil {
		t.Fatalf("valid quiz: %v", err)
	}

	wrongMetadata := valid
	wrongMetadata.Items = append([]QuizItem(nil), valid.Items...)
	wrongMetadata.Items[0].MarkerIndex = 1
	if _, err := validateQuiz(wrongMetadata, markers); err == nil {
		t.Fatal("marker mismatch was accepted")
	}

	wrongHints := valid
	wrongHints.Items = append([]QuizItem(nil), valid.Items...)
	wrongHints.Items[0].Hints = []string{"one", "two"}
	if _, err := validateQuiz(wrongHints, markers); err == nil {
		t.Fatal("quiz without exactly three hints was accepted")
	}
}

func TestValidateTopics(t *testing.T) {
	input := []TopicSuggestion{
		{Name: "상급", Slug: "AdvancedTopic", Difficulty: "상", Style: "구조실험"},
		{Name: "초급", Slug: "BeginnerTopic", Difficulty: "하", Style: "테마형"},
		{Name: "중급", Slug: "MiddleTopic", Difficulty: "중", Style: "현실사례"},
	}
	got, err := validateTopics(input)
	if err != nil {
		t.Fatalf("valid topics: %v", err)
	}
	if difficulties := []string{got[0].Difficulty, got[1].Difficulty, got[2].Difficulty}; !reflect.DeepEqual(difficulties, []string{"하", "중", "상"}) {
		t.Fatalf("difficulty order = %v", difficulties)
	}

	invalid := []TopicSuggestion{
		{Name: "A", Slug: "bad-slug", Difficulty: "하", Style: "테마형"},
		{Name: "B", Slug: "Middle", Difficulty: "중", Style: "현실사례"},
		{Name: "C", Slug: "Advanced", Difficulty: "상", Style: "구조실험"},
	}
	if _, err := validateTopics(invalid); err == nil {
		t.Fatal("invalid slug was accepted")
	}

	duplicateDifficulty := append([]TopicSuggestion(nil), input...)
	duplicateDifficulty[0].Difficulty = "중"
	if _, err := validateTopics(duplicateDifficulty); err == nil {
		t.Fatal("duplicate difficulty was accepted")
	}

	missingTheme := append([]TopicSuggestion(nil), input...)
	missingTheme[1].Style = "구조실험"
	if _, err := validateTopics(missingTheme); err == nil {
		t.Fatal("topic set without exactly one themed training was accepted")
	}
}

func TestSortedNamedContentsIsDeterministic(t *testing.T) {
	got, err := sortedNamedContents(map[string]string{"z.go": "z", "a.go": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Path != "a.go" || got[1].Path != "z.go" {
		t.Fatalf("files not sorted: %#v", got)
	}
}

func TestSortedNamedContentsRejectsAmbiguousAndExcessiveInput(t *testing.T) {
	if _, err := sortedNamedContents(map[string]string{"Main.go": "a", "main.go": "b"}); err == nil {
		t.Fatal("case-insensitive duplicate input path was accepted")
	}
	tooMany := make(map[string]string, maxInputFileCount+1)
	for i := 0; i <= maxInputFileCount; i++ {
		tooMany[fmt.Sprintf("file-%d.go", i)] = "package example"
	}
	if _, err := sortedNamedContents(tooMany); err == nil {
		t.Fatal("excessive input file count was accepted")
	}
}

func TestCollectQuizMarkersUsesStableFileOrder(t *testing.T) {
	markers, err := collectQuizMarkers(map[string]string{
		"z.go": "// [TUTOR:BUG]\nvar z = 1\n// [TUTOR:END]",
		"a.go": "// [TUTOR:HOLE]\nfunc a() {}\n// [TUTOR:END]",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 2 || markers[0].Filename != "a.go" || markers[1].Filename != "z.go" {
		t.Fatalf("marker order = %#v", markers)
	}
}
