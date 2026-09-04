package ai

import (
	"strings"
	"testing"
)

func TestSupportedLanguageContractsMatchRuntime(t *testing.T) {
	tests := []struct {
		language  string
		generated map[string]string
	}{
		{
			language: "go",
			generated: map[string]string{
				"main.go": `package main
func main() {}
func exercise() int {
	// [TUTOR:HOLE] compute a value
	value := 0
	// [TUTOR:END]
	// [TUTOR:BUG] return the wrong value
	return value + 1
	// [TUTOR:END]
}`,
				"main_test.go": "package main\nimport \"testing\"\nfunc TestExercise(t *testing.T) {}",
			},
		},
		{
			language: "python",
			generated: map[string]string{
				"main.py": `def exercise():
    # [TUTOR:HOLE] compute a value
    value = 0
    # [TUTOR:END]
    # [TUTOR:BUG] return the wrong value
    return value + 1
    # [TUTOR:END]

if __name__ == "__main__":
    print(exercise())`,
				"test_main.py": "def test_exercise():\n    assert True",
			},
		},
		{
			language: "rust",
			generated: map[string]string{
				"Cargo.toml": "[package]\nname = \"lesson\"\nversion = \"0.1.0\"",
				"src/main.rs": `fn exercise() -> i32 {
    // [TUTOR:HOLE] compute a value
    let value = 0;
    // [TUTOR:END]
    // [TUTOR:BUG] return the wrong value
    value + 1
    // [TUTOR:END]
}
fn main() { println!("{}", exercise()); }
#[cfg(test)]
mod tests {
    #[test]
    fn exercise_works() { assert_eq!(2, 2); }
}`,
			},
		},
		{
			language: "typescript",
			generated: map[string]string{
				"package.json": `{"scripts":{"test":"jest"}}`,
				"src/index.ts": `export function exercise(): number {
  // [TUTOR:HOLE] compute a value
  const value = 0
  // [TUTOR:END]
  // [TUTOR:BUG] return the wrong value
  return value + 1
  // [TUTOR:END]
}`,
				"src/index.test.ts": "import { exercise } from './index'\ntest('exercise', () => { expect(exercise()).toBe(1) })",
			},
		},
		{
			language: "javascript",
			generated: map[string]string{
				"package.json": `{"scripts":{"test":"jest"}}`,
				"index.js": `function exercise() {
  // [TUTOR:HOLE] compute a value
  const value = 0
  // [TUTOR:END]
  // [TUTOR:BUG] return the wrong value
  return value + 1
  // [TUTOR:END]
}
module.exports = { exercise }`,
				"index.test.js": "const { exercise } = require('./index')\ntest('exercise', () => { expect(exercise()).toBe(1) })",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			contract, err := languageContract(tt.language)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateGeneratedProject(contract, tt.generated, nil); err != nil {
				t.Fatalf("valid %s project contract: %v", tt.language, err)
			}
		})
	}

	if _, err := languageContract("solidity"); err == nil {
		t.Fatal("Solidity generation contract was accepted without a runtime executor")
	}
}

func TestValidateMarkerLayoutRequiresClosedNonOverlappingRanges(t *testing.T) {
	contract, err := languageContract("go")
	if err != nil {
		t.Fatal(err)
	}
	valid := `package main
func exercise() int {
	// [TUTOR:HOLE] multiline implementation
	value := 0
	value++
	// [TUTOR:END]
	// [TUTOR:BUG] multiline bug
	if value > 0 {
		value--
	}
	// [TUTOR:END]
	return value
}`
	if holes, bugs, err := validateMarkerLayout(contract, "main.go", valid, false); err != nil || holes != 1 || bugs != 1 {
		t.Fatalf("valid ranges = holes %d bugs %d error %v", holes, bugs, err)
	}

	tests := map[string]string{
		"missing end": `// [TUTOR:HOLE]
value := 0`,
		"stray end": `// [TUTOR:END]
value := 0`,
		"nested": `// [TUTOR:HOLE]
// [TUTOR:BUG]
value := 0
// [TUTOR:END]
// [TUTOR:END]`,
		"empty body": `// [TUTOR:HOLE]
// hint only
// [TUTOR:END]`,
		"inline marker": `value := "[TUTOR:HOLE]"
// [TUTOR:END]`,
		"end description": `// [TUTOR:HOLE]
value := 0
// [TUTOR:END] trailing`,
		"oversized body": `// [TUTOR:HOLE]
one := 1
two := 2
three := 3
four := 4
five := 5
// [TUTOR:END]`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := validateMarkerLayout(contract, "main.go", content, false); err == nil {
				t.Fatal("invalid marker range was accepted")
			}
		})
	}

	if _, _, err := validateMarkerLayout(contract, "main_test.go", valid, true); err == nil {
		t.Fatal("markers in a test file were accepted")
	}
}

func TestCodeGenerationPromptKeepsTasksSmallAndUnsolved(t *testing.T) {
	for _, want := range []string{"현재 단계 과제", "정답 코드", "최대 4줄", "독립 marker"} {
		if !strings.Contains(codeFilesSystemPrompt, want) {
			t.Fatalf("code generation prompt is missing %q", want)
		}
	}
}

func TestGeneratedProjectRejectsFilenameOnlyTestFixture(t *testing.T) {
	contract, err := languageContract("go")
	if err != nil {
		t.Fatal(err)
	}
	generated := map[string]string{
		"main.go": `package main
func main() {}
func exercise() int {
    // [TUTOR:HOLE]
    value := 0
    // [TUTOR:END]
    // [TUTOR:BUG]
    return value + 1
    // [TUTOR:END]
}`,
		"main_test.go": "package main",
	}
	if err := validateGeneratedProject(contract, generated, nil); err == nil || !strings.Contains(err.Error(), "include tests") {
		t.Fatalf("empty test file error = %v", err)
	}
}

func TestEntrypointAndTestDeclarationsRequireExecutableContent(t *testing.T) {
	invalidEntrypoints := map[string]map[string]string{
		"go":         {"main.go": "// package main\n// func main() {}"},
		"python":     {"main.py": "  \n"},
		"rust":       {"cargo.toml": "[package]\nname = \"lesson\"", "src/main.rs": "// fn main() {}"},
		"typescript": {"package.json": "{}", "src/index.ts": ""},
		"javascript": {"package.json": "{}", "index.js": "\n"},
	}
	for language, files := range invalidEntrypoints {
		if hasRequiredEntrypoint(language, files) {
			t.Errorf("%s empty/comment-only entrypoint was accepted", language)
		}
	}

	invalidTests := map[string]string{
		"go":         "// func TestExercise(t *testing.T) {}",
		"python":     "# def test_exercise(): pass",
		"rust":       "// #[test]",
		"typescript": "// test('exercise', () => {})",
		"javascript": "// it('exercise', () => {})",
	}
	for language, content := range invalidTests {
		if hasTestDeclaration(language, content) {
			t.Errorf("%s comment-only test declaration was accepted", language)
		}
	}
}
