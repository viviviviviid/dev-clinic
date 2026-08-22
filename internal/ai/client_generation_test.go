package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateCodeFilesRepairsContractViolationOnce(t *testing.T) {
	implementation := `package main

func main() {}

func frequency() int {
	// [TUTOR:HOLE] implement frequency counting
	value := 0
	// [TUTOR:END]
	// [TUTOR:BUG] return the correct value
	return value + 1
	// [TUTOR:END]
}`
	invalidTest := `package main

import "testing"

func TestFrequency(t *testing.T) {
	// [TUTOR:HOLE] complete the test
	if frequency() != 1 { t.Fatal("wrong value") }
	// [TUTOR:END]
}`
	validTest := `package main

import "testing"

func TestFrequency(t *testing.T) {
	if frequency() != 1 { t.Fatal("wrong value") }
}`

	responses := []codeFilesResponse{
		{Files: []generatedFile{{Path: "main.go", Content: implementation}, {Path: "frequency_test.go", Content: invalidTest}}},
		{Files: []generatedFile{{Path: "main.go", Content: implementation}, {Path: "frequency_test.go", Content: validTest}}},
	}
	calls := 0
	client := &Client{generateStructuredHook: func(_ context.Context, _, prompt string, _ map[string]any) (string, error) {
		if calls >= len(responses) {
			t.Fatal("generateStructured called more than twice")
		}
		if calls == 1 {
			for _, want := range []string{"직전 생성 결과", `test file "frequency_test.go" must not contain tutor markers`} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("repair prompt missing %q: %s", want, prompt)
				}
			}
		}
		encoded, err := json.Marshal(responses[calls])
		calls++
		return string(encoded), err
	}}

	files, err := client.GenerateCodeFiles(context.Background(), tutorSystemFixture(1), nil)
	if err != nil {
		t.Fatalf("GenerateCodeFiles() repair failed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("generateStructured calls = %d, want 2", calls)
	}
	if containsTutorMarker(files["frequency_test.go"]) {
		t.Fatal("repaired test file still contains tutor markers")
	}
}

func TestGenerateCodeFilesReturnsSecondContractFailure(t *testing.T) {
	invalid := codeFilesResponse{Files: []generatedFile{{Path: "frequency_test.go", Content: `package main
// [TUTOR:HOLE]
func TestFrequency() {}
// [TUTOR:END]`}}}
	encoded, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &Client{generateStructuredHook: func(_ context.Context, _, _ string, _ map[string]any) (string, error) {
		calls++
		return string(encoded), nil
	}}

	_, err = client.GenerateCodeFiles(context.Background(), tutorSystemFixture(1), nil)
	if err == nil || !strings.Contains(err.Error(), "after one correction") {
		t.Fatalf("GenerateCodeFiles() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("generateStructured calls = %d, want 2", calls)
	}
}
