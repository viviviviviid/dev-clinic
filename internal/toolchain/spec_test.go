package toolchain

import (
	"reflect"
	"strings"
	"testing"
)

func TestRegistryCommands(t *testing.T) {
	tests := []struct {
		language string
		run      []string
		testAll  []string
		testOne  []string
	}{
		{"Go (1.25)", []string{"go", "run", "."}, []string{"go", "test", "-v", "./..."}, []string{"go", "test", "-v", "-run", "TestOne", "./..."}},
		{"Python", []string{"python3", "main.py"}, []string{"python3", "-m", "pytest", "-v"}, []string{"python3", "-m", "pytest", "-v", "-k", "TestOne"}},
		{"Rust", []string{"cargo", "run"}, []string{"cargo", "test"}, []string{"cargo", "test", "TestOne"}},
		{"TypeScript (Node 22)", []string{"npx", "--no-install", "ts-node", "src/index.ts"}, []string{"npx", "--no-install", "jest", "--no-coverage"}, []string{"npx", "--no-install", "jest", "--no-coverage", "-t", "TestOne"}},
		{"JavaScript", []string{"node", "index.js"}, []string{"npx", "--no-install", "jest", "--no-coverage"}, []string{"npx", "--no-install", "jest", "--no-coverage", "-t", "TestOne"}},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			spec, ok := Lookup(tt.language)
			if !ok {
				t.Fatal("language was not found")
			}
			if got := spec.RunCommand(); !reflect.DeepEqual(got, tt.run) {
				t.Fatalf("RunCommand() = %#v, want %#v", got, tt.run)
			}
			if got := spec.TestCommand(""); !reflect.DeepEqual(got, tt.testAll) {
				t.Fatalf("TestCommand(all) = %#v, want %#v", got, tt.testAll)
			}
			if got := spec.TestCommand("TestOne"); !reflect.DeepEqual(got, tt.testOne) {
				t.Fatalf("TestCommand(one) = %#v, want %#v", got, tt.testOne)
			}
		})
	}
}

func TestNodeToolchainsRequireJestProjectContract(t *testing.T) {
	tests := []struct {
		language   string
		entrypoint string
		testFile   string
		sourceFile string
	}{
		{"typescript", "src/index.ts", "src/index.test.tsx", "src/view.tsx"},
		{"javascript", "index.js", "index.spec.jsx", "src/view.jsx"},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			spec, _ := Lookup(tt.language)
			files := map[string]string{
				"package.json": `{"scripts":{"test":"jest --no-coverage"}}`,
				tt.entrypoint:  "export const value = 1",
			}
			if !spec.HasRequiredEntrypoint(files) {
				t.Fatal("valid Jest project contract was rejected")
			}
			files["package.json"] = `{"scripts":{"test":"vitest"}}`
			if spec.HasRequiredEntrypoint(files) {
				t.Fatal("non-Jest test script was accepted")
			}
			if !spec.IsTestFile(tt.testFile, "") || !spec.IsDedicatedTestFile(tt.testFile) {
				t.Fatal("dedicated test file pattern was not recognized")
			}
			if !spec.IsSourceFile(tt.sourceFile) {
				t.Fatal("source extension was not recognized")
			}
			if !spec.HasTestDeclaration("test('works', () => {})") {
				t.Fatal("Jest declaration was not recognized")
			}
			if !strings.Contains(strings.Join(spec.TestCommand(""), " "), "--no-install") {
				t.Fatal("Node test command can install packages")
			}
			if !strings.Contains(spec.Instructions(), "자동 설치하지 않습니다") {
				t.Fatal("generation contract does not prohibit automatic package installation")
			}
		})
	}
}

func TestCommandSlicesCannotMutateRegistry(t *testing.T) {
	spec, _ := Lookup("typescript")
	command := spec.RunCommand()
	command[0] = "changed"
	if got := spec.RunCommand()[0]; got != "npx" {
		t.Fatalf("registry command was mutated: %q", got)
	}
}

func TestDedicatedTestFileDetectionAcrossToolchains(t *testing.T) {
	for _, filename := range []string{
		"/project/main_test.go",
		"/project/test_main.py",
		"/project/tests/exercise.rs",
		"/project/src/index.test.ts",
		"/project/src/component.spec.tsx",
		"/project/index.spec.js",
		"/project/component.test.jsx",
	} {
		if !IsDedicatedTestFile(filename) {
			t.Errorf("dedicated test file was not recognized: %q", filename)
		}
	}
	if IsDedicatedTestFile("/project/src/index.ts") {
		t.Fatal("source file was classified as a dedicated test")
	}
}

func TestTestConfigurationRegistry(t *testing.T) {
	for _, filename := range []string{
		"go.mod", "go.work.sum", "Cargo.toml", ".cargo/config.toml",
		"package.json", "package-lock.json", "tsconfig.test.json", "jest.config.ts",
		"pyproject.toml", "pytest.ini", "requirements-dev.txt",
	} {
		if !IsTestConfigurationFile(filename) {
			t.Errorf("test configuration not registered: %s", filename)
		}
	}
	for _, filename := range []string{"main.go", "src/index.ts", "notes.md", "quiz.json", ".env"} {
		if IsTestConfigurationFile(filename) {
			t.Errorf("ordinary file registered as test configuration: %s", filename)
		}
	}
}
