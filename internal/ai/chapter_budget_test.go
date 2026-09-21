package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func chapterFiles(holes, bugs int) map[string]string {
	source := func(kind, function string, count int) string {
		var body strings.Builder
		fmt.Fprintf(&body, "package main\nfunc %s() int {\nvalue := 0\n", function)
		for i := 0; i < count; i++ {
			fmt.Fprintf(&body, "// [TUTOR:%s] exercise %d\nvalue++\n// [TUTOR:END]\n", kind, i)
		}
		body.WriteString("return value\n}\n")
		return body.String()
	}
	return map[string]string{
		"main.go":      source("HOLE", "fill", holes) + "func main() {}\n",
		"bugs.go":      source("BUG", "fix", bugs),
		"main_test.go": "package main\nimport \"testing\"\nfunc TestExercise(t *testing.T) { if fill() + fix() != 0 { t.Fatal(\"wrong result\") } }\n",
	}
}

func TestChapterBudgetCountsBothKindsAcrossAllFiles(t *testing.T) {
	contract, err := languageContract("go")
	if err != nil {
		t.Fatal(err)
	}
	for _, counts := range [][2]int{{1, 1}, {1, 2}, {2, 2}, {3, 1}, {1, 3}, {4, 1}, {1, 4}, {4, 4}} {
		t.Run(fmt.Sprint(counts), func(t *testing.T) {
			files := chapterFiles(counts[0], counts[1])
			wantError := counts[0]+counts[1] > 4
			if err := validateGeneratedProject(contract, files, nil); (err != nil) != wantError {
				t.Fatalf("project validation = %v, want error %v", err, wantError)
			}
			markers, err := collectQuizMarkers(files)
			if (err != nil) != wantError {
				t.Fatalf("quiz marker collection = %v, want error %v", err, wantError)
			}
			if !wantError && len(markers) != counts[0]+counts[1] {
				t.Fatalf("marker count = %d", len(markers))
			}
		})
	}
}

func TestChapterBudgetIncludesRetainedFilesWithoutCountingReplacementsTwice(t *testing.T) {
	contract, _ := languageContract("go")
	files := chapterFiles(2, 2)
	existing := chapterFiles(2, 2)
	existing["README.md"] = "Problems are marked with [TUTOR:HOLE] and [TUTOR:BUG]."
	if err := validateGeneratedProject(contract, files, existing); err != nil {
		t.Fatalf("replacement files were counted twice: %v", err)
	}
	existing["previous.go"] = "package main\nfunc previous() {\n// [TUTOR:HOLE]\n_ = 0\n// [TUTOR:END]\n}\n"
	if err := validateGeneratedProject(contract, files, existing); err == nil || !strings.Contains(err.Error(), "chapter has 5") {
		t.Fatalf("retained exercise was not counted: %v", err)
	}
	files["previous.go"] = "package main\nfunc previous() {}\n"
	if err := validateGeneratedProject(contract, files, existing); err != nil {
		t.Fatalf("completed prior exercise still counted: %v", err)
	}
}

func TestQuizBudgetRejectsFiveEvenWhenAllMetadataMatches(t *testing.T) {
	var expected []quizMarker
	var response quizResponse
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("main.go:hole:%d", i)
		expected = append(expected, quizMarker{Key: key, Filename: "main.go", MarkerType: "hole", Index: i})
		response.Items = append(response.Items, QuizItem{Key: key, Filename: "main.go", MarkerType: "hole", MarkerIndex: i, Question: "구현하세요", Hints: []string{"개념", "구조", "방향"}})
	}
	if _, err := validateQuiz(response, expected); err == nil {
		t.Fatal("five matching quiz items were accepted")
	}
	if _, err := validateQuiz(quizResponse{Items: response.Items[:4]}, expected[:4]); err != nil {
		t.Fatalf("four matching quiz items were rejected: %v", err)
	}
	items := quizSchema()["properties"].(map[string]any)["items"].(map[string]any)
	if items["maxItems"] != 4 {
		t.Fatalf("provider schema does not enforce the chapter budget: %v", items["maxItems"])
	}
}

func TestGenerateCodeFilesCorrectsOversizedChapterOrRejectsIt(t *testing.T) {
	for _, nextStep := range []bool{false, true} {
		for _, repairSucceeds := range []bool{false, true} {
			t.Run(fmt.Sprintf("next=%v/repair=%v", nextStep, repairSucceeds), func(t *testing.T) {
				calls := 0
				client := &Client{generateStructuredHook: func(_ context.Context, system, prompt string, _ map[string]any) (string, error) {
					if !strings.Contains(system, "모든 파일을 합쳐 HOLE과 BUG 총 4개 이하") {
						t.Fatal("generation instructions omitted the combined chapter budget")
					}
					if calls == 1 && !strings.Contains(prompt, "chapter has 5 HOLE/BUG exercises across all files; maximum is 4") {
						t.Fatal("repair did not receive the chapter budget violation")
					}
					files := chapterFiles(3, 2)
					if calls == 1 && repairSucceeds {
						files = chapterFiles(2, 2)
					}
					calls++
					var response codeFilesResponse
					for name, content := range files {
						response.Files = append(response.Files, generatedFile{Path: name, Content: content})
					}
					encoded, err := json.Marshal(response)
					return string(encoded), err
				}}
				step := 1
				var existing map[string]string
				if nextStep {
					step = 2
					existing = map[string]string{"previous.go": "package main\nfunc previous() {}\n"}
				}
				files, err := client.GenerateCodeFiles(context.Background(), tutorSystemFixture(step), existing)
				if calls != 2 {
					t.Fatalf("generation attempts = %d, want 2", calls)
				}
				if !repairSucceeds {
					if err == nil || files != nil || !strings.Contains(err.Error(), "after one correction") {
						t.Fatalf("oversized result escaped validation: files=%v err=%v", files, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				markers, err := collectQuizMarkers(files)
				if err != nil || len(markers) != 4 {
					t.Fatalf("corrected chapter = %d markers, err=%v", len(markers), err)
				}
			})
		}
	}
}
