// Package localapi contains HTTP handlers for the local (user-side) server.
// These endpoints do file I/O, run the watcher, and manage local project state.
// No Supabase, no Gemini API key required.
package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/pathguard"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/snapshot"
	"github.com/coding-tutor/internal/watcher"
	"github.com/gin-gonic/gin"
)

const (
	maxGeneratedFiles       = 128
	maxGeneratedFileBytes   = 1 << 20
	maxGeneratedTotalBytes  = 8 << 20
	maxProjectReadFiles     = 256
	maxProjectReadFileBytes = 1 << 20
	maxProjectReadTotal     = 8 << 20
	maxCommandOutputBytes   = 64 << 10
)

var (
	errProjectReadBudget = errors.New("project exceeds read budget")
	errInvalidQuiz       = errors.New("quiz data must be a JSON object")
	errQuizTooLarge      = errors.New("quiz data exceeds size limit")
	ignoredProjectDirs   = map[string]bool{
		".git": true, ".snapshots": true, "node_modules": true, "vendor": true,
		"dist": true, "build": true, "target": true, ".venv": true, "venv": true,
		"coverage": true,
	}
	readableProjectExts = map[string]bool{
		".go": true, ".py": true, ".rs": true, ".ts": true, ".tsx": true,
		".js": true, ".jsx": true, ".sol": true, ".md": true, ".json": true,
		".toml": true, ".yaml": true, ".yml": true,
	}
)

type generatedFile struct {
	name    string
	path    string
	content string
}

func validateGeneratedFiles(base string, files map[string]string) ([]generatedFile, error) {
	if len(files) > maxGeneratedFiles {
		return nil, fmt.Errorf("too many generated files: %d", len(files))
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	validated := make([]generatedFile, 0, len(files))
	seenPaths := make(map[string]string, len(files))
	total := 0
	for _, name := range names {
		content := files[name]
		if len(content) > maxGeneratedFileBytes {
			return nil, fmt.Errorf("generated file %q exceeds size limit", name)
		}
		total += len(content)
		if total > maxGeneratedTotalBytes {
			return nil, errors.New("generated files exceed total size limit")
		}
		if _, err := pathguard.ResolveRelative(base, name); err != nil {
			return nil, fmt.Errorf("invalid generated filename %q: %w", name, err)
		}
		path, err := pathguard.ResolveChildNoSymlinks(base, name)
		if err != nil {
			return nil, fmt.Errorf("invalid generated filename %q: %w", name, err)
		}
		pathKey := strings.ToLower(path)
		if previous, duplicate := seenPaths[pathKey]; duplicate {
			return nil, fmt.Errorf("generated filenames %q and %q resolve to the same path", previous, name)
		}
		seenPaths[pathKey] = name
		validated = append(validated, generatedFile{name: name, path: path, content: content})
	}
	return validated, nil
}

func resolveProjectDir(candidate string) (string, error) {
	return pathguard.Resolve(config.Global.BaseDir, candidate)
}

// SetupProjectReq is sent by the browser after the clinic generates a mission.
// The clinic writes the files and starts the watcher.
type SetupProjectReq struct {
	DirSuffix  string            `json:"dir_suffix"`
	SetupToken string            `json:"setup_token"`
	Files      map[string]string `json:"files"` // filename → content (includes TUTORSYS.md, quiz.json)
	Curriculum string            `json:"curriculum"`
	SkillLevel string            `json:"skill_level"`
	Language   string            `json:"language"`
}

const setupTokenFilename = ".clinic-setup-token"

var setupTokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func setupTokenMatches(projectDir, setupToken string) bool {
	tokenPath, err := pathguard.ResolveChildNoSymlinks(projectDir, setupTokenFilename)
	if err != nil {
		return false
	}
	info, err := os.Lstat(tokenPath)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(tokenPath)
	return err == nil && strings.TrimSpace(string(data)) == setupToken
}

func activateSetupProject(projectDir string) error {
	tutorPath, err := pathguard.ResolveChildNoSymlinks(projectDir, "TUTORSYS.md")
	if err != nil {
		return fmt.Errorf("resolve curriculum: %w", err)
	}
	info, err := os.Lstat(tutorPath)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("generated project has no regular TUTORSYS.md")
	}
	curriculum, err := os.ReadFile(tutorPath)
	if err != nil {
		return fmt.Errorf("read curriculum: %w", err)
	}
	if err := watcher.Start(projectDir); err != nil {
		return fmt.Errorf("watcher: %w", err)
	}
	project.Global.Set(projectDir, string(curriculum))
	return nil
}

// SetupProject writes AI-generated files to disk and starts the file watcher.
// POST /api/project/setup
func SetupProject(c *gin.Context) {
	var req SetupProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !setupTokenPattern.MatchString(strings.TrimSpace(req.SetupToken)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid setup token"})
		return
	}
	req.SetupToken = strings.TrimSpace(req.SetupToken)

	projectDir, err := pathguard.ResolveChildNoSymlinks(config.Global.BaseDir, req.DirSuffix)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project directory"})
		return
	}
	if info, statErr := os.Lstat(projectDir); statErr == nil {
		if !info.IsDir() || !setupTokenMatches(projectDir, req.SetupToken) {
			c.JSON(http.StatusConflict, gin.H{"error": "project directory already exists"})
			return
		}
		if err := activateSetupProject(projectDir); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "project_dir": projectDir, "resumed": true})
		return
	} else if !os.IsNotExist(statErr) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": statErr.Error()})
		return
	}

	baseDir, err := pathguard.Resolve(config.Global.BaseDir, config.Global.BaseDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid base directory"})
		return
	}
	stagingDir, err := os.MkdirTemp(baseDir, ".clinic-setup-")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer os.RemoveAll(stagingDir)

	files, err := validateGeneratedFiles(stagingDir, req.Files)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Write all files
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.path), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := os.WriteFile(file.path, []byte(file.content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// go mod for Go projects (non-fatal)
	if req.Language == "go" || hasGoFiles(req.Files) {
		if err := ensureGoMod(stagingDir); err != nil {
			fmt.Printf("go mod: %v\n", err)
		}
	}

	if err := os.WriteFile(filepath.Join(stagingDir, setupTokenFilename), []byte(req.SetupToken+"\n"), 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := os.Rename(stagingDir, projectDir); err != nil {
		// A retry whose first response was lost is safe only when the opaque
		// setup token matches. Never overwrite an unrelated learner directory.
		if setupTokenMatches(projectDir, req.SetupToken) {
			if activateErr := activateSetupProject(projectDir); activateErr == nil {
				c.JSON(http.StatusOK, gin.H{"ok": true, "project_dir": projectDir, "resumed": true})
				return
			}
		}
		c.JSON(http.StatusConflict, gin.H{"error": "project directory already exists"})
		return
	}
	if err := activateSetupProject(projectDir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"project_dir": projectDir,
	})
}

// ApplyStepReq applies a next step generated by the clinic.
type ApplyStepReq struct {
	NewCurriculum string            `json:"new_curriculum"`
	NewFiles      map[string]string `json:"new_files"`
	QuizData      json.RawMessage   `json:"quiz_data,omitempty"`
}

// ApplyStep saves a snapshot, writes new files, updates project state, and restarts watcher.
// POST /api/project/apply-step
func ApplyStep(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project not loaded"})
		return
	}

	var req ApplyStepReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.NewCurriculum == "" || len(req.NewCurriculum) > maxGeneratedFileBytes || !utf8.ValidString(req.NewCurriculum) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new_curriculum must be valid UTF-8 within the size limit"})
		return
	}
	if len(req.QuizData) > maxGeneratedFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "quiz data exceeds size limit"})
		return
	}
	if err := validateQuizData(req.QuizData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dir := project.Global.GetDir()
	dir, err := resolveProjectDir(dir)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "project is outside base directory"})
		return
	}
	files, err := validateGeneratedFiles(dir, req.NewFiles)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	currentStep := extractCurrentStep(project.Global.GetContent())

	// Save snapshot before overwriting
	if err := snapshot.Save(dir, currentStep); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "snapshot: " + err.Error()})
		return
	}

	// Write new files
	for _, file := range files {
		if isManagedProjectFile(file.name) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file.path), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("write %s: %v", file.name, err)})
			return
		}
		if err := os.WriteFile(file.path, []byte(file.content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("write %s: %v", file.name, err)})
			return
		}
	}

	// Every step replaces quiz state. A missing/null value deliberately removes
	// stale questions from the previous step.
	if err := replaceQuizFile(dir, req.QuizData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update quiz.json: " + err.Error()})
		return
	}

	// Re-run go mod if needed (non-fatal)
	if hasGoFiles(req.NewFiles) {
		if err := ensureGoMod(dir); err != nil {
			fmt.Printf("go mod: %v\n", err)
		}
	}

	// TUTORSYS.md has one authoritative source: new_curriculum. Write it last so
	// a duplicated generated-file entry can never win.
	tutorPath, err := pathguard.ResolveChildNoSymlinks(dir, "TUTORSYS.md")
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid curriculum path"})
		return
	}
	if err := os.WriteFile(tutorPath, []byte(req.NewCurriculum), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write TUTORSYS.md: " + err.Error()})
		return
	}

	project.Global.Set(dir, req.NewCurriculum)

	if err := watcher.Start(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "watcher: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, project.Global.GetStatus())
}

// ReadAllFiles returns all source files in the current project directory.
// The browser sends this data to the clinic when requesting the next step.
// GET /api/project/read-all
func ReadAllFiles(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project not loaded"})
		return
	}
	dir, err := resolveProjectDir(project.Global.GetDir())
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "project is outside base directory"})
		return
	}
	files, err := collectProjectFiles(dir)
	if err != nil {
		if errors.Is(err, errProjectReadBudget) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	curriculum := project.Global.GetContent()
	c.JSON(http.StatusOK, gin.H{
		"files":      files,
		"curriculum": curriculum,
	})
}

func collectProjectFiles(dir string) (map[string]string, error) {
	canonicalDir, err := pathguard.Resolve(dir, dir)
	if err != nil {
		return nil, err
	}
	dir = canonicalDir
	files := map[string]string{}
	totalBytes := int64(0)
	fileCount := 0
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path != dir && info.IsDir() {
			if ignoredProjectDirs[info.Name()] || strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		base := info.Name()
		lowerBase := strings.ToLower(base)
		if isSensitiveProjectFile(base) || lowerBase == "quiz.json" || lowerBase == "tutorsys.md" || lowerBase == "go.sum" || lowerBase == "go.mod" {
			return nil
		}
		if !readableProjectExts[strings.ToLower(filepath.Ext(base))] {
			return nil
		}
		if info.Size() > maxProjectReadFileBytes || fileCount >= maxProjectReadFiles || totalBytes+info.Size() > maxProjectReadTotal {
			return errProjectReadBudget
		}
		resolved, err := pathguard.Resolve(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return nil
		}
		rel, err := filepath.Rel(dir, resolved)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		fileCount++
		totalBytes += int64(len(data))
		return nil
	})
	return files, err
}

func isManagedProjectFile(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return base == "tutorsys.md" || base == "quiz.json"
}

func replaceQuizFile(dir string, raw json.RawMessage) error {
	quizPath, err := pathguard.ResolveChildNoSymlinks(dir, "quiz.json")
	if err != nil {
		return err
	}
	if err := validateQuizData(raw); err != nil {
		return err
	}
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		if err := os.Remove(quizPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(quizPath, raw, 0644)
}

func validateQuizData(raw json.RawMessage) error {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	return validateQuizObject(raw)
}

func validateQuizObject(raw []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return errInvalidQuiz
	}
	return nil
}

func isSensitiveProjectFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, ".") || strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
		return true
	}
	switch lower {
	case "credentials.json", "service-account.json", "service_account.json", "secrets.json", "id_rsa", "id_ed25519":
		return true
	default:
		return false
	}
}

// DeleteProjectFilesReq is sent when deleting a project.
type DeleteProjectFilesReq struct {
	ProjectDir string `json:"project_dir"`
}

// DeleteProjectFiles removes local project files and stops the watcher.
// DELETE /api/project/files
func DeleteProjectFiles(c *gin.Context) {
	var req DeleteProjectFilesReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ProjectDir == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_dir required"})
		return
	}

	projectDir, err := pathguard.ResolveChildNoSymlinks(config.Global.BaseDir, req.ProjectDir)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	projectInfo, err := os.Lstat(projectDir)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project directory not found"})
		return
	}
	if !projectInfo.IsDir() || projectInfo.Mode()&os.ModeSymlink != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project path is not a regular directory"})
		return
	}

	if watcher.Global != nil {
		watcher.Global.Stop()
	}

	if err := os.RemoveAll(projectDir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- Handlers moved from internal/api/project.go (no AI/Supabase needed) ----

func GetProjectStatus(c *gin.Context) {
	c.JSON(http.StatusOK, project.Global.GetStatus())
}

type LoadProjectReq struct {
	Dir string `json:"dir"`
}

func LoadProject(c *gin.Context) {
	var req LoadProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dir, err := resolveProjectDir(req.Dir)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	if _, err := pathguard.ResolveChildNoSymlinks(dir, "TUTORSYS.md"); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid curriculum path"})
		return
	}

	if err := project.Global.Load(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := watcher.EnsureStarted(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "watcher: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, project.Global.GetStatus())
}

func GetQuiz(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	dir, err := resolveProjectDir(project.Global.GetDir())
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	quizPath, err := pathguard.ResolveChildNoSymlinks(dir, "quiz.json")
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	data, err := readQuizFile(quizPath)
	if os.IsNotExist(err) {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	if errors.Is(err, errQuizTooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, errInvalidQuiz) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read quiz data"})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

func readQuizFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxGeneratedFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxGeneratedFileBytes {
		return nil, errQuizTooLarge
	}
	if err := validateQuizObject(data); err != nil {
		return nil, err
	}
	return data, nil
}

type RestoreSnapshotReq struct {
	Step string `json:"step"`
}

func RestoreSnapshot(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project not loaded"})
		return
	}
	var req RestoreSnapshotReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Step == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "step required"})
		return
	}
	dir, err := resolveProjectDir(project.Global.GetDir())
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	if err := snapshot.Restore(dir, req.Step); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := pathguard.ResolveChildNoSymlinks(dir, "TUTORSYS.md"); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid curriculum path"})
		return
	}
	if err := project.Global.Load(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reload: " + err.Error()})
		return
	}
	if err := watcher.Start(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "watcher: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "step": req.Step})
}

func ListSnapshots(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusOK, gin.H{"snapshots": []string{}})
		return
	}
	dir, err := resolveProjectDir(project.Global.GetDir())
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	labels, err := snapshot.List(dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshots": labels})
}

// StopWatcher stops the watcher without removing files.
// Called when completing a mission.
// POST /api/project/stop-watcher
func StopWatcher(c *gin.Context) {
	if watcher.Global != nil {
		watcher.Global.Stop()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- helpers ----

func ensureGoMod(dir string) error {
	goModPath := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		moduleName := filepath.Base(dir)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "mod", "init", moduleName)
		cmd.Dir = dir
		out, err := runBoundedCommand(cmd)
		if err != nil {
			return fmt.Errorf("go mod init: %w: %s", err, out)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = dir
	out, err := runBoundedCommand(cmd)
	if err != nil {
		return fmt.Errorf("go mod tidy: %w: %s", err, out)
	}
	return nil
}

type cappedOutput struct {
	buffer bytes.Buffer
	max    int
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	written := len(p)
	remaining := w.max - w.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = w.buffer.Write(p)
	}
	return written, nil
}

func runBoundedCommand(cmd *exec.Cmd) (string, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if err == syscall.ESRCH {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	output := &cappedOutput{max: maxCommandOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	return output.buffer.String(), err
}

func hasGoFiles(files map[string]string) bool {
	for name := range files {
		if strings.HasSuffix(name, ".go") {
			return true
		}
	}
	return false
}

func extractCurrentStep(content string) string {
	lines := strings.Split(content, "\n")
	inSection := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## 현재 단계") {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(line, "## ") {
				break
			}
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return "Step 1"
}
