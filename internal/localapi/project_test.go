package localapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/coding-tutor/internal/config"
	"github.com/gin-gonic/gin"
)

func TestValidateGeneratedFilesRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"../escape.go", filepath.Join(root, "absolute.go")} {
		if _, err := validateGeneratedFiles(projectDir, map[string]string{name: "x"}); err == nil {
			t.Fatalf("validateGeneratedFiles() accepted %q", name)
		}
	}
	if _, err := validateGeneratedFiles(projectDir, map[string]string{
		"main.go":   "first",
		"./main.go": "second",
	}); err == nil {
		t.Fatal("validateGeneratedFiles() accepted duplicate canonical paths")
	}

	link := filepath.Join(projectDir, "linked")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := validateGeneratedFiles(projectDir, map[string]string{"linked/escape.go": "x"}); err == nil {
			t.Fatal("validateGeneratedFiles() accepted a symlink escape")
		}
	}
	internalTarget := filepath.Join(projectDir, "internal-target")
	if err := os.Mkdir(internalTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	internalLink := filepath.Join(projectDir, "internal-link")
	if err := os.Symlink(internalTarget, internalLink); err == nil {
		if _, err := validateGeneratedFiles(projectDir, map[string]string{"internal-link/main.go": "x"}); err == nil {
			t.Fatal("validateGeneratedFiles() accepted an internal directory symlink")
		}
	}

	files, err := validateGeneratedFiles(projectDir, map[string]string{"src/main.go": "package main"})
	if err != nil {
		t.Fatalf("valid files error = %v", err)
	}
	canonicalProjectDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].path != filepath.Join(canonicalProjectDir, "src", "main.go") {
		t.Fatalf("validated files = %#v", files)
	}
}

func TestCollectProjectFilesIgnoresSecretsDependenciesAndBinaries(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"main.go":               "package main",
		"TUTORSYS.md":           "managed curriculum",
		"quiz.json":             `{"old":true}`,
		".env":                  "SECRET=value",
		"credentials.json":      `{"token":"secret"}`,
		"node_modules/pkg/a.js": "dependency",
		"image.bin":             "binary-ish",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := collectProjectFiles(dir)
	if err != nil {
		t.Fatalf("collectProjectFiles() error = %v", err)
	}
	if len(got) != 1 || got["main.go"] != "package main" {
		t.Fatalf("collectProjectFiles() = %#v", got)
	}
}

func TestReplaceQuizFileReplacesAndRemovesStaleQuiz(t *testing.T) {
	dir := t.TempDir()
	quizPath := filepath.Join(dir, "quiz.json")
	if err := os.WriteFile(quizPath, []byte(`{"old":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	newQuiz := json.RawMessage(`{"main.go:hole:0":{"question":"q"}}`)
	if err := replaceQuizFile(dir, newQuiz); err != nil {
		t.Fatalf("replace quiz: %v", err)
	}
	if got, err := os.ReadFile(quizPath); err != nil || string(got) != string(newQuiz) {
		t.Fatalf("quiz file = %q, %v", got, err)
	}

	if err := replaceQuizFile(dir, json.RawMessage("null")); err != nil {
		t.Fatalf("remove null quiz: %v", err)
	}
	if _, err := os.Stat(quizPath); !os.IsNotExist(err) {
		t.Fatalf("stale quiz still exists: %v", err)
	}
	if err := replaceQuizFile(dir, nil); err != nil {
		t.Fatalf("remove missing quiz: %v", err)
	}

	if err := replaceQuizFile(dir, json.RawMessage(`[]`)); !errors.Is(err, errInvalidQuiz) {
		t.Fatalf("array quiz error = %v", err)
	}
}

func TestReplaceQuizFileRejectsSymlinkWithoutChangingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	if err := os.WriteFile(target, []byte("must remain"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "quiz.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := replaceQuizFile(dir, json.RawMessage(`{"overwrite":true}`)); err == nil {
		t.Fatal("replaceQuizFile() accepted a symbolic link")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "must remain" {
		t.Fatalf("quiz symlink target changed: %q, %v", data, err)
	}
}

func TestManagedProjectFilesAreReservedAtAnyDepth(t *testing.T) {
	for _, name := range []string{"TUTORSYS.md", "state/tutorsys.MD", "quiz.json", "state/QUIZ.JSON"} {
		if !isManagedProjectFile(name) {
			t.Errorf("managed path %q was not reserved", name)
		}
	}
	if isManagedProjectFile("lesson.json") {
		t.Fatal("ordinary JSON file was treated as managed")
	}
}

func TestReadQuizFileIsBoundedAndRequiresJSONObject(t *testing.T) {
	dir := t.TempDir()
	quizPath := filepath.Join(dir, "quiz.json")

	if _, err := readQuizFile(quizPath); !os.IsNotExist(err) {
		t.Fatalf("missing quiz error = %v", err)
	}
	if err := os.WriteFile(quizPath, []byte(`{"item":{"question":"q"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := readQuizFile(quizPath); err != nil || string(data) != `{"item":{"question":"q"}}` {
		t.Fatalf("valid quiz = %q, %v", data, err)
	}

	for _, invalid := range []string{"null", "[]", "{broken"} {
		if err := os.WriteFile(quizPath, []byte(invalid), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readQuizFile(quizPath); !errors.Is(err, errInvalidQuiz) {
			t.Fatalf("invalid quiz %q error = %v", invalid, err)
		}
	}

	file, err := os.Create(quizPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxGeneratedFileBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readQuizFile(quizPath); !errors.Is(err, errQuizTooLarge) {
		t.Fatalf("oversized quiz error = %v", err)
	}
}

func TestCollectProjectFilesEnforcesBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.go")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxProjectReadFileBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()

	if _, err := collectProjectFiles(dir); !errors.Is(err, errProjectReadBudget) {
		t.Fatalf("collectProjectFiles() error = %v, want budget error", err)
	}
}

func TestSetupProjectRejectsDirectoryTraversal(t *testing.T) {
	base := t.TempDir()
	previous := config.Global.BaseDir
	config.Global.BaseDir = base
	t.Cleanup(func() { config.Global.BaseDir = previous })
	gin.SetMode(gin.TestMode)

	body, _ := json.Marshal(SetupProjectReq{
		DirSuffix: "../escape",
		Files:     map[string]string{"main.go": "package main"},
	})
	router := gin.New()
	router.POST("/setup", SetupProject)
	request := httptest.NewRequest(http.MethodPost, "/setup", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("SetupProject() status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestSetupProjectRejectsExistingGeneratedFileSymlink(t *testing.T) {
	base := t.TempDir()
	projectDir := filepath.Join(base, "260817-GoBasics")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectDir, "important.go")
	if err := os.WriteFile(target, []byte("must remain"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(projectDir, "main.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	previous := config.Global.BaseDir
	config.Global.BaseDir = base
	t.Cleanup(func() { config.Global.BaseDir = previous })
	gin.SetMode(gin.TestMode)
	body, _ := json.Marshal(SetupProjectReq{
		DirSuffix: "260817-GoBasics",
		Files:     map[string]string{"main.go": "overwritten"},
	})
	router := gin.New()
	router.POST("/setup", SetupProject)
	request := httptest.NewRequest(http.MethodPost, "/setup", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("SetupProject() status = %d, body = %s", response.Code, response.Body.String())
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "must remain" {
		t.Fatalf("generated-file symlink target changed: %q, %v", data, err)
	}
}

func TestDeleteProjectFilesRejectsSymlinkWithoutDeletingTarget(t *testing.T) {
	base := t.TempDir()
	targetDir := filepath.Join(base, "real-project")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(targetDir, "main.go")
	if err := os.WriteFile(sentinel, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkedProject := filepath.Join(base, "linked-project")
	if err := os.Symlink(targetDir, linkedProject); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	previous := config.Global.BaseDir
	config.Global.BaseDir = base
	t.Cleanup(func() { config.Global.BaseDir = previous })
	gin.SetMode(gin.TestMode)
	body, _ := json.Marshal(DeleteProjectFilesReq{ProjectDir: linkedProject})
	router := gin.New()
	router.DELETE("/project", DeleteProjectFiles)
	request := httptest.NewRequest(http.MethodDelete, "/project", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("DeleteProjectFiles() status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("symlink target was deleted: %v", err)
	}
}
