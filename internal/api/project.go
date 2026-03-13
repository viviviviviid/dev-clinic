package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/snapshot"
	"github.com/coding-tutor/internal/supabase"
	"github.com/coding-tutor/internal/watcher"
	"github.com/gin-gonic/gin"
)

func GetProjectStatus(c *gin.Context) {
	c.JSON(http.StatusOK, project.Global.GetStatus())
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

	// Write TUTORSYS.md
	tutorPath := filepath.Join(req.Dir, "TUTORSYS.md")
	if err := os.WriteFile(tutorPath, []byte(req.Curriculum), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Generate code files
	files, err := ai.Global.GenerateCodeFiles(c.Request.Context(), req.Curriculum, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Write code files
	created := []string{}
	for name, content := range files {
		path := filepath.Join(req.Dir, name)
		dir := filepath.Dir(path)
		os.MkdirAll(dir, 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		created = append(created, name)
	}

	// Generate quiz data for newbie level
	if req.SkillLevel == "newbie" {
		quizData, err := ai.Global.GenerateQuizData(c.Request.Context(), req.Curriculum, files)
		if err == nil && len(quizData) > 0 {
			quizBytes, jsonErr := json.Marshal(quizData)
			if jsonErr == nil {
				quizPath := filepath.Join(req.Dir, "quiz.json")
				os.WriteFile(quizPath, quizBytes, 0644)
			}
		}
	}

	// Load project
	project.Global.Set(req.Dir, req.Curriculum)

	// Start watcher
	if err := watcher.Start(req.Dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "watcher: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":    true,
		"files": created,
		"dir":   req.Dir,
	})
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

	if err := project.Global.Load(req.Dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := watcher.Start(req.Dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "watcher: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, project.Global.GetStatus())
}

func findNextStep(content, currentStep string) (string, string) {
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
	// currentStep이 "Step 2: 설명" 형태일 수 있으므로 label만 추출해서 비교
	currentLabel := currentStep
	if idx := strings.Index(currentStep, ":"); idx > 0 {
		currentLabel = strings.TrimSpace(currentStep[:idx])
	}
	for i, s := range steps {
		if s.label == currentLabel && i+1 < len(steps) {
			return steps[i+1].label, steps[i+1].full
		}
	}
	return "", ""
}

func AdvanceToNextStep(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project not loaded"})
		return
	}

	if ai.Global == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI client not initialized"})
		return
	}

	status := project.Global.GetStatus()
	nextLabel, nextFull := findNextStep(status.Content, status.CurrentStep)
	if nextLabel == "" {
		c.JSON(http.StatusOK, gin.H{"done": true, "message": "모든 단계를 완료했습니다"})
		return
	}

	dir := project.Global.GetDir()

	// 현재 코드 파일 읽기 (소스 + 테스트 모두 포함) — GenerateNextStep과 GenerateCodeFiles에서 사용
	currentFiles := map[string]string{}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() || e.Name() == "TUTORSYS.md" || e.Name() == "quiz.json" {
				continue
			}
			name := e.Name()
			data, rerr := os.ReadFile(filepath.Join(dir, name))
			if rerr == nil {
				currentFiles[name] = string(data)
			}
		}
	}

	newCurriculum, err := ai.Global.GenerateNextStep(c.Request.Context(), status.Content, nextFull, currentFiles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 현재 단계 스냅샷 저장 (비치명적 — 실패해도 진행)
	_ = snapshot.Save(dir, status.CurrentStep)

	tutorPath := filepath.Join(dir, "TUTORSYS.md")
	if err := os.WriteFile(tutorPath, []byte(newCurriculum), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	files, err := ai.Global.GenerateCodeFiles(c.Request.Context(), newCurriculum, currentFiles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte(content), 0644)
	}

	if status.SkillLevel == "newbie" {
		quizData, err := ai.Global.GenerateQuizData(c.Request.Context(), newCurriculum, files)
		if err == nil && len(quizData) > 0 {
			quizBytes, jsonErr := json.Marshal(quizData)
			if jsonErr == nil {
				os.WriteFile(filepath.Join(dir, "quiz.json"), quizBytes, 0644)
			}
		}
	}

	project.Global.Set(dir, newCurriculum)

	if err := watcher.Start(dir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "watcher: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, project.Global.GetStatus())
}

func CompleteProject(c *gin.Context) {
	if !project.Global.IsLoaded() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project not loaded"})
		return
	}
	userID := c.GetString("user_id")
	dir := project.Global.GetDir()

	// daily_missions에서 project_dir 일치하는 미션 status를 completed로 업데이트
	path := fmt.Sprintf("daily_missions?user_id=eq.%s&project_dir=eq.%s", userID, dir)
	if err := supabase.Patch(path, map[string]string{"status": "completed"}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if watcher.Global != nil {
		watcher.Global.Stop()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
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
	// TUTORSYS.md를 다시 로드
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
	dir := project.Global.GetDir()
	labels, err := snapshot.List(dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshots": labels})
}
