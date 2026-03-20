package api

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
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/supabase"
	"github.com/coding-tutor/internal/watcher"
	"github.com/gin-gonic/gin"
)

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

type CreateProjectReq struct {
	Dir        string `json:"dir"`
	Language   string `json:"language"`
	Topic      string `json:"topic"`
	SkillLevel string `json:"skillLevel"`
}

func CreateProject(c *gin.Context) {
	var req CreateProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if ai.Global == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI client not initialized"})
		return
	}

	curriculum, err := ai.Global.GenerateCurriculum(c.Request.Context(), req.Language, req.Topic, req.SkillLevel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"curriculum": curriculum,
		"dir":        req.Dir,
	})
}

type ConfirmProjectReq struct {
	Dir        string `json:"dir"`
	Curriculum string `json:"curriculum"`
	SkillLevel string `json:"skillLevel"`
}

// ConfirmProject generates code files from a curriculum and returns them.
// The caller (browser) is responsible for writing files via LOCAL /api/project/setup.
func ConfirmProject(c *gin.Context) {
	var req ConfirmProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if ai.Global == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI client not initialized"})
		return
	}

	files, err := ai.Global.GenerateCodeFiles(c.Request.Context(), req.Curriculum, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	allFiles := make(map[string]string, len(files)+1)
	allFiles["TUTORSYS.md"] = req.Curriculum
	for k, v := range files {
		allFiles[k] = v
	}

	if req.SkillLevel == "newbie" {
		quizData, err := ai.Global.GenerateQuizData(c.Request.Context(), req.Curriculum, files)
		if err == nil && len(quizData) > 0 {
			quizBytes, jsonErr := json.Marshal(quizData)
			if jsonErr == nil {
				allFiles["quiz.json"] = string(quizBytes)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":    true,
		"files": allFiles,
		"dir":   req.Dir,
	})
}

func findNextStep(content, currentStep string) (string, string, error) {
	lines := strings.Split(content, "\n")
	inCurriculum := false
	type stepInfo struct{ label, full string }
	var steps []stepInfo
	for _, line := range lines {
		if strings.HasPrefix(line, "## 커리큘럼 단계") {
			inCurriculum = true
			continue
		}
		if inCurriculum {
			if strings.HasPrefix(line, "## ") {
				break
			}
			if strings.HasPrefix(line, "- [ ] ") || strings.HasPrefix(line, "- [x] ") {
				step := strings.TrimPrefix(line, "- [ ] ")
				step = strings.TrimPrefix(step, "- [x] ")
				step = strings.TrimSpace(step)
				label := step
				if idx := strings.Index(step, ":"); idx > 0 {
					label = strings.TrimSpace(step[:idx])
				}
				steps = append(steps, stepInfo{label: label, full: step})
			}
		}
	}
	if len(steps) == 0 {
		return "", "", fmt.Errorf("TUTORSYS.md에 커리큘럼 단계가 없습니다")
	}
	currentLabel := currentStep
	if idx := strings.Index(currentStep, ":"); idx > 0 {
		currentLabel = strings.TrimSpace(currentStep[:idx])
	}
	for i, s := range steps {
		if s.label == currentLabel {
			if i+1 < len(steps) {
				return steps[i+1].label, steps[i+1].full, nil
			}
			// 마지막 단계 — 완료
			return "", "", nil
		}
	}
	return "", "", fmt.Errorf("현재 단계 %q를 커리큘럼에서 찾을 수 없습니다", currentStep)
}

// extractCurrentStepFromContent parses the "현재 단계" section from TUTORSYS.md content.
func extractCurrentStepFromContent(content string) string {
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

// NextStepReq is sent by the browser with the current project state.
// The home server generates the next step and returns new files without touching disk.
type NextStepReq struct {
	Curriculum   string            `json:"curriculum"`
	CurrentFiles map[string]string `json:"current_files"`
	SkillLevel   string            `json:"skill_level"`
}

func AdvanceToNextStep(c *gin.Context) {
	if ai.Global == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI client not initialized"})
		return
	}

	var req NextStepReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	currentStep := extractCurrentStepFromContent(req.Curriculum)
	nextLabel, nextFull, err := findNextStep(req.Curriculum, currentStep)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if nextLabel == "" {
		c.JSON(http.StatusOK, gin.H{"done": true, "message": "모든 단계를 완료했습니다"})
		return
	}

	newCurriculum, err := ai.Global.GenerateNextStep(c.Request.Context(), req.Curriculum, nextFull, req.CurrentFiles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	files, err := ai.Global.GenerateCodeFiles(c.Request.Context(), newCurriculum, req.CurrentFiles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Include TUTORSYS.md so the local server can write it
	allFiles := make(map[string]string, len(files)+1)
	allFiles["TUTORSYS.md"] = newCurriculum
	for k, v := range files {
		allFiles[k] = v
	}

	resp := gin.H{
		"new_curriculum": newCurriculum,
		"new_files":      allFiles,
	}

	if req.SkillLevel == "newbie" {
		quizData, err := ai.Global.GenerateQuizData(c.Request.Context(), newCurriculum, files)
		if err == nil && len(quizData) > 0 {
			resp["quiz_data"] = quizData
		}
	}

	c.JSON(http.StatusOK, resp)
}

type CompleteProjectReq struct {
	ProjectDir string `json:"project_dir"`
}

func CompleteProject(c *gin.Context) {
	userID := c.GetString("user_id")

	var req CompleteProjectReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ProjectDir == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_dir required"})
		return
	}

	// Match by exact path first, then by dir_suffix
	path := fmt.Sprintf("daily_missions?user_id=eq.%s&project_dir=eq.%s", userID, req.ProjectDir)
	if err := supabase.Patch(path, map[string]string{"status": "completed"}); err != nil {
		suffix := req.ProjectDir
		if idx := strings.LastIndex(req.ProjectDir, "/"); idx >= 0 {
			suffix = req.ProjectDir[idx+1:]
		}
		path2 := fmt.Sprintf("daily_missions?user_id=eq.%s&project_dir=eq.%s", userID, suffix)
		if err2 := supabase.Patch(path2, map[string]string{"status": "completed"}); err2 != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type DeleteProjectReq struct {
	ProjectDir string `json:"project_dir"`
}

func DeleteProject(c *gin.Context) {
	userID := c.GetString("user_id")
	var req DeleteProjectReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ProjectDir == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_dir required"})
		return
	}

	// Remove from DB — match by exact path or by dir_suffix
	_ = supabase.Delete(fmt.Sprintf("daily_missions?user_id=eq.%s&project_dir=eq.%s", userID, req.ProjectDir))
	if idx := strings.LastIndex(req.ProjectDir, "/"); idx >= 0 {
		suffix := req.ProjectDir[idx+1:]
		_ = supabase.Delete(fmt.Sprintf("daily_missions?user_id=eq.%s&project_dir=eq.%s", userID, suffix))
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- kept for single-binary backward compat (used by cmd/server when AI is local) ----

func GetProjectStatus(c *gin.Context) {
	c.JSON(http.StatusOK, project.Global.GetStatus())
}

type LoadProjectReq struct {
	Dir        string `json:"dir"`
	AIProxyURL string `json:"ai_proxy_url,omitempty"`
}

func LoadProject(c *gin.Context) {
	var req LoadProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := project.Global.Load(req.Dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if ai.Global != nil {
		ai.Global.SetToken(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	}
	if err := watcher.Start(req.Dir); err != nil {
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
