// Package localapi contains HTTP handlers for the local (user-side) server.
// These endpoints do file I/O, run the watcher, and manage local project state.
// No Supabase, no Gemini API key required.
package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/snapshot"
	"github.com/coding-tutor/internal/watcher"
	"github.com/gin-gonic/gin"
)

// SetupProjectReq is sent by the browser after the home server streams back
// the generated files. The local server writes files, starts the watcher, and
// initialises the AI proxy with the token.
type SetupProjectReq struct {
	DirSuffix  string            `json:"dir_suffix"`
	Files      map[string]string `json:"files"` // filename → content (includes TUTORSYS.md, quiz.json)
	Curriculum string            `json:"curriculum"`
	SkillLevel string            `json:"skill_level"`
	Language   string            `json:"language"`
	AIProxyURL string            `json:"ai_proxy_url"` // e.g. https://tutor.abcfe.net/api/ai/proxy
	Token      string            `json:"token,omitempty"`
}

// SetupProject writes AI-generated files to disk and starts the file watcher.
// POST /api/project/setup
func SetupProject(c *gin.Context) {
	var req SetupProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	projectDir := filepath.Join(config.Global.BaseDir, req.DirSuffix)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Write all files
	for name, content := range req.Files {
		path := filepath.Join(projectDir, name)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// go mod for Go projects (non-fatal)
	if req.Language == "go" || hasGoFiles(req.Files) {
		if err := ensureGoMod(projectDir); err != nil {
			fmt.Printf("go mod: %v\n", err)
		}
	}

	// Initialise AI proxy
	if req.AIProxyURL != "" {
		ai.InitProxy(req.AIProxyURL)
	}
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if token == "" {
		token = req.Token
	}
	if ai.Global != nil {
		ai.Global.SetToken(token)
	}

	// Set project state and start watcher
	curriculum := req.Curriculum
	if curriculum == "" {
		if data, err := os.ReadFile(filepath.Join(projectDir, "TUTORSYS.md")); err == nil {
			curriculum = string(data)
		}
	}
	project.Global.Set(projectDir, curriculum)

	if err := watcher.Start(projectDir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "watcher: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"project_dir": projectDir,
	})
}

// ApplyStepReq is sent by the browser after the home server generates the next step.
type ApplyStepReq struct {
	NewCurriculum string            `json:"new_curriculum"`
	NewFiles      map[string]string `json:"new_files"`
	AIProxyURL    string            `json:"ai_proxy_url,omitempty"`
	Token         string            `json:"token,omitempty"`
	QuizData      json.RawMessage   `json:"quiz_data,omitempty"` // 뉴비 전용: 홈서버가 생성한 quiz 데이터
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

	dir := project.Global.GetDir()
	currentStep := extractCurrentStep(project.Global.GetContent())

	// Save snapshot before overwriting
	_ = snapshot.Save(dir, currentStep)

	// Write new files
	for name, content := range req.NewFiles {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("write %s: %v", name, err)})
			return
		}
	}

	// Write quiz.json if provided (뉴비 전용 — apply-step과 원자적으로 처리)
	if len(req.QuizData) > 0 && string(req.QuizData) != "null" {
		quizPath := filepath.Join(dir, "quiz.json")
		if err := os.WriteFile(quizPath, req.QuizData, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "write quiz.json: " + err.Error()})
			return
		}
	}

	// Re-run go mod if needed (non-fatal)
	if hasGoFiles(req.NewFiles) {
		if err := ensureGoMod(dir); err != nil {
			fmt.Printf("go mod: %v\n", err)
		}
	}

	// Update AI token if provided
	if req.AIProxyURL != "" {
		ai.InitProxy(req.AIProxyURL)
	}
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if token == "" {
		token = req.Token
	}
	if ai.Global != nil {
		ai.Global.SetToken(token)
	}

	project.Global.Set(dir, req.NewCurriculum)

	if err := watcher.Start(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "watcher: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, project.Global.GetStatus())
}

// ReadAllFiles returns all source files in the current project directory.
// The browser sends this data to the home server when requesting nextstep.
// GET /api/project/read-all
func ReadAllFiles(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project not loaded"})
		return
	}
	dir := project.Global.GetDir()
	files := map[string]string{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.Contains(path, "/.snapshots") {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		if base == "quiz.json" || base == "go.sum" || base == "go.mod" {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		data, rerr := os.ReadFile(path)
		if rerr == nil {
			files[rel] = string(data)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	curriculum := project.Global.GetContent()
	c.JSON(http.StatusOK, gin.H{
		"files":      files,
		"curriculum": curriculum,
	})
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

	// Security: must be within base_dir
	if !strings.HasPrefix(req.ProjectDir, config.Global.BaseDir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	if watcher.Global != nil {
		watcher.Global.Stop()
	}

	if err := os.RemoveAll(req.ProjectDir); err != nil {
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
	Dir        string `json:"dir"`
	AIProxyURL string `json:"ai_proxy_url,omitempty"`
	Token      string `json:"token,omitempty"`
}

func LoadProject(c *gin.Context) {
	var req LoadProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dir := req.Dir
	// If dir is a relative suffix (no leading /), prepend BaseDir
	if !strings.HasPrefix(dir, "/") {
		dir = filepath.Join(config.Global.BaseDir, dir)
	}

	if err := project.Global.Load(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Initialise AI proxy if provided
	if req.AIProxyURL != "" {
		ai.InitProxy(req.AIProxyURL)
	}
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if token == "" {
		token = req.Token
	}
	if ai.Global != nil {
		ai.Global.SetToken(token)
	}

	if err := watcher.Start(dir); err != nil {
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
	dir := project.Global.GetDir()
	quizPath := filepath.Join(dir, "quiz.json")
	data, err := os.ReadFile(quizPath)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
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
	dir := project.Global.GetDir()
	if err := snapshot.Restore(dir, req.Step); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := project.Global.Load(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reload: " + err.Error()})
		return
	}
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if ai.Global != nil && token != "" {
		ai.Global.SetToken(token)
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
	dir := project.Global.GetDir()
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
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go mod init: %w: %s", err, out)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod tidy: %w: %s", err, out)
	}
	return nil
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

// SaveQuizJSON writes quiz data to the project dir.
// Used after apply-step when the browser has quiz data from the home server.
type SaveQuizReq struct {
	QuizData json.RawMessage `json:"quiz_data"`
}

func SaveQuiz(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project not loaded"})
		return
	}
	var req SaveQuizReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dir := project.Global.GetDir()
	if err := os.WriteFile(filepath.Join(dir, "quiz.json"), req.QuizData, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
