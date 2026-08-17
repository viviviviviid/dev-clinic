package toolchain

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"
)

var (
	goPackageMainPattern = regexp.MustCompile(`(?m)^[\t ]*package[\t ]+main[\t ]*$`)
	goMainPattern        = regexp.MustCompile(`(?m)^[\t ]*func[\t ]+main[\t ]*\(`)
	rustMainPattern      = regexp.MustCompile(`(?m)^[\t ]*fn[\t ]+main[\t ]*\(`)
)

// Spec is the single source of truth for a language's local run/test contract.
// Its fields are intentionally private so callers cannot mutate the registry.
type Spec struct {
	name                 string
	comment              string
	instructions         string
	runCommand           []string
	testCommand          func(string) []string
	entrypoint           func(map[string]string) bool
	testFile             func(string, string) bool
	dedicatedTestFile    func(string) bool
	testDeclaration      *regexp.Regexp
	sourceFileExtensions map[string]struct{}
}

var registry = []*Spec{
	{
		name:       "go",
		comment:    "//",
		runCommand: []string{"go", "run", "."},
		testCommand: func(testName string) []string {
			if testName != "" {
				return []string{"go", "test", "-v", "-run", testName, "./..."}
			}
			return []string{"go", "test", "-v", "./..."}
		},
		instructions: `Go 실행 계약:
- 프로젝트 루트에서 "go run ."으로 실행되어야 합니다. 루트의 .go 파일 중 하나에 package main과 func main()이 있어야 합니다.
- 테스트는 *_test.go와 표준 testing 패키지를 사용하며 "go test -v ./..."으로 실행되어야 합니다.`,
		entrypoint: func(files map[string]string) bool {
			for filename, content := range files {
				if !strings.Contains(filename, "/") && strings.HasSuffix(filename, ".go") &&
					!strings.HasSuffix(filename, "_test.go") && goPackageMainPattern.MatchString(content) &&
					goMainPattern.MatchString(content) {
					return true
				}
			}
			return false
		},
		testFile: func(filename, _ string) bool {
			return strings.HasSuffix(path.Base(filename), "_test.go")
		},
		dedicatedTestFile: func(filename string) bool {
			return strings.HasSuffix(path.Base(filename), "_test.go")
		},
		testDeclaration:      regexp.MustCompile(`(?m)^[\t ]*func[\t ]+Test[A-Za-z0-9_]*[\t ]*\(`),
		sourceFileExtensions: extensionSet(".go"),
	},
	{
		name:       "python",
		comment:    "#",
		runCommand: []string{"python3", "main.py"},
		testCommand: func(testName string) []string {
			command := []string{"python3", "-m", "pytest", "-v"}
			if testName != "" {
				command = append(command, "-k", testName)
			}
			return command
		},
		instructions: `Python 실행 계약:
- 진입점은 프로젝트 루트의 main.py이며 "python3 main.py"로 실행되어야 합니다.
- 테스트는 test_*.py와 pytest를 사용하며 "python3 -m pytest -v"로 실행되어야 합니다.`,
		entrypoint: func(files map[string]string) bool {
			return strings.TrimSpace(files["main.py"]) != ""
		},
		testFile: func(filename, _ string) bool {
			base := path.Base(filename)
			return strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py")
		},
		dedicatedTestFile: func(filename string) bool {
			base := path.Base(filename)
			return strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py")
		},
		testDeclaration:      regexp.MustCompile(`(?m)^[\t ]*def[\t ]+test_[A-Za-z0-9_]*[\t ]*\(`),
		sourceFileExtensions: extensionSet(".py"),
	},
	{
		name:       "rust",
		comment:    "//",
		runCommand: []string{"cargo", "run"},
		testCommand: func(testName string) []string {
			command := []string{"cargo", "test"}
			if testName != "" {
				command = append(command, testName)
			}
			return command
		},
		instructions: `Rust 실행 계약:
- Cargo.toml과 src/main.rs를 포함하고 "cargo run"으로 실행되어야 합니다.
- 테스트는 #[cfg(test)] 모듈 또는 tests/*.rs이며 "cargo test"로 실행되어야 합니다.`,
		entrypoint: func(files map[string]string) bool {
			return strings.Contains(files["cargo.toml"], "[package]") && rustMainPattern.MatchString(files["src/main.rs"])
		},
		testFile: func(filename, content string) bool {
			return (strings.HasPrefix(filename, "tests/") && strings.HasSuffix(filename, ".rs")) ||
				strings.Contains(content, "#[cfg(test)]")
		},
		dedicatedTestFile: func(filename string) bool {
			return (strings.HasPrefix(filename, "tests/") || strings.Contains(filename, "/tests/")) &&
				strings.HasSuffix(filename, ".rs")
		},
		testDeclaration:      regexp.MustCompile(`(?m)^[\t ]*#[\t ]*\[[\t ]*test[\t ]*\]`),
		sourceFileExtensions: extensionSet(".rs"),
	},
	{
		name:       "typescript",
		comment:    "//",
		runCommand: []string{"npx", "--no-install", "ts-node", "src/index.ts"},
		testCommand: func(testName string) []string {
			command := []string{"npx", "--no-install", "jest", "--no-coverage"}
			if testName != "" {
				command = append(command, "-t", testName)
			}
			return command
		},
		instructions: `TypeScript 실행 계약:
- package.json과 src/index.ts를 포함하고 "npx --no-install ts-node src/index.ts"로 실행되어야 합니다.
- package.json의 scripts.test는 Jest를 실행해야 합니다. TypeScript 테스트 변환 설정(ts-jest 또는 동등한 로컬 설정)과 typescript, ts-node, jest도 프로젝트 로컬 의존성으로 선언합니다.
- 테스트는 *.test.ts, *.spec.ts 또는 TSX 대응 확장자와 Jest를 사용하며 "npx --no-install jest --no-coverage"로 실행되어야 합니다.
- clinic은 패키지를 자동 설치하지 않습니다. 실행 중 npx 설치나 npm install을 요구하는 코드를 만들지 마세요.`,
		entrypoint: func(files map[string]string) bool {
			return hasJestPackageScript(files["package.json"]) && strings.TrimSpace(files["src/index.ts"]) != ""
		},
		testFile: func(filename, _ string) bool {
			return hasAnySuffix(path.Base(filename), ".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx")
		},
		dedicatedTestFile: func(filename string) bool {
			return hasAnySuffix(path.Base(filename), ".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx")
		},
		testDeclaration:      regexp.MustCompile(`(?m)^[\t ]*(?:test|it)[\t ]*\(`),
		sourceFileExtensions: extensionSet(".ts", ".tsx"),
	},
	{
		name:       "javascript",
		comment:    "//",
		runCommand: []string{"node", "index.js"},
		testCommand: func(testName string) []string {
			command := []string{"npx", "--no-install", "jest", "--no-coverage"}
			if testName != "" {
				command = append(command, "-t", testName)
			}
			return command
		},
		instructions: `JavaScript 실행 계약:
- package.json과 프로젝트 루트의 index.js를 포함하고 "node index.js"로 실행되어야 합니다.
- package.json의 scripts.test는 Jest를 실행해야 하며 jest는 프로젝트 로컬 의존성으로 선언합니다.
- 테스트는 *.test.js, *.spec.js 또는 JSX 대응 확장자와 Jest를 사용하며 "npx --no-install jest --no-coverage"로 실행되어야 합니다.
- clinic은 패키지를 자동 설치하지 않습니다. 실행 중 npx 설치나 npm install을 요구하는 코드를 만들지 마세요.`,
		entrypoint: func(files map[string]string) bool {
			return hasJestPackageScript(files["package.json"]) && strings.TrimSpace(files["index.js"]) != ""
		},
		testFile: func(filename, _ string) bool {
			return hasAnySuffix(path.Base(filename), ".test.js", ".spec.js", ".test.jsx", ".spec.jsx")
		},
		dedicatedTestFile: func(filename string) bool {
			return hasAnySuffix(path.Base(filename), ".test.js", ".spec.js", ".test.jsx", ".spec.jsx")
		},
		testDeclaration:      regexp.MustCompile(`(?m)^[\t ]*(?:test|it)[\t ]*\(`),
		sourceFileExtensions: extensionSet(".js", ".jsx"),
	},
}

// Lookup accepts canonical names and annotated values such as "TypeScript (Node 22)".
func Lookup(language string) (*Spec, bool) {
	normalized := strings.ToLower(strings.TrimSpace(language))
	for _, spec := range registry {
		if strings.HasPrefix(normalized, spec.name) {
			return spec, true
		}
	}
	return nil, false
}

// IsDedicatedTestFile reports whether any supported toolchain owns filename as
// a standalone test file. It is useful before a project language is available.
func IsDedicatedTestFile(filename string) bool {
	for _, spec := range registry {
		if spec.IsDedicatedTestFile(filename) {
			return true
		}
	}
	return false
}

func (s *Spec) Name() string { return s.name }

func (s *Spec) Comment() string { return s.comment }

func (s *Spec) Instructions() string { return s.instructions }

func (s *Spec) RunCommand() []string { return append([]string(nil), s.runCommand...) }

func (s *Spec) TestCommand(testName string) []string {
	return append([]string(nil), s.testCommand(testName)...)
}

func (s *Spec) HasRequiredEntrypoint(files map[string]string) bool {
	return s.entrypoint(normalizeFiles(files))
}

func (s *Spec) IsTestFile(filename, content string) bool {
	return s.testFile(filepathKey(filename), content)
}

func (s *Spec) IsDedicatedTestFile(filename string) bool {
	return s.dedicatedTestFile(filepathKey(filename))
}

func (s *Spec) HasTestDeclaration(content string) bool {
	return s.testDeclaration.MatchString(content)
}

func (s *Spec) IsSourceFile(filename string) bool {
	_, ok := s.sourceFileExtensions[strings.ToLower(path.Ext(filename))]
	return ok
}

func extensionSet(extensions ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		result[extension] = struct{}{}
	}
	return result
}

func hasAnySuffix(value string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func normalizeFiles(files map[string]string) map[string]string {
	result := make(map[string]string, len(files))
	for filename, content := range files {
		result[filepathKey(filename)] = content
	}
	return result
}

func filepathKey(filename string) string {
	return strings.ToLower(strings.ReplaceAll(filename, "\\", "/"))
}

func hasJestPackageScript(content string) bool {
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(content), &manifest) != nil {
		return false
	}
	fields := strings.Fields(manifest.Scripts["test"])
	return len(fields) > 0 && fields[0] == "jest"
}
