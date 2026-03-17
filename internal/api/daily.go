package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"strings"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/project"
	"github.com/coding-tutor/internal/supabase"
	"github.com/coding-tutor/internal/watcher"
	"github.com/gin-gonic/gin"
)

type DailyMission struct {
	ID         string    `json:"id,omitempty"`
	UserID     string    `json:"user_id"`
	Date       string    `json:"date"`
	Topic      string    `json:"topic"`
	Slug       string    `json:"slug"`
	ProjectDir string    `json:"project_dir"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

func GetDaily(c *gin.Context) {
	userID := c.GetString("user_id")
	today := time.Now().Format("2006-01-02")

	var missions []DailyMission
	err := supabase.Get(
		fmt.Sprintf("daily_missions?user_id=eq.%s&date=eq.%s&order=created_at.asc&select=*", userID, today),
		&missions,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if missions == nil {
		missions = []DailyMission{}
	}

	// Always generate topic suggestions so user can add more missions
	var settings []UserSettings
	if err := supabase.Get(
		fmt.Sprintf("user_settings?user_id=eq.%s&select=*", userID),
		&settings,
	); err != nil || len(settings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user settings not found"})
		return
	}

	if ai.Global == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI client not initialized"})
		return
	}

	// Fetch all history to avoid repeating past topics
	var allHistory []DailyMission
	supabase.Get(fmt.Sprintf("daily_missions?user_id=eq.%s&order=created_at.desc&select=topic", userID), &allHistory)
	pastTopics := make([]string, 0, len(allHistory))
	seen := map[string]bool{}
	for _, m := range allHistory {
		if !seen[m.Topic] {
			seen[m.Topic] = true
			pastTopics = append(pastTopics, m.Topic)
		}
	}

	topics, err := ai.Global.GenerateDailyTopics(c.Request.Context(), settings[0].Language, settings[0].SkillLevel, pastTopics)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"missions": missions, "topics": topics})
}

func GetDailyHistory(c *gin.Context) {
	userID := c.GetString("user_id")

	dateFilter := c.Query("date")
	var query string
	if dateFilter != "" {
		query = fmt.Sprintf("daily_missions?user_id=eq.%s&date=eq.%s&order=created_at.asc&select=*", userID, dateFilter)
	} else {
		query = fmt.Sprintf("daily_missions?user_id=eq.%s&order=date.desc,created_at.asc&select=*", userID)
	}

	var missions []DailyMission
	if err := supabase.Get(query, &missions); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, missions)
}

type ConfirmDailyReq struct {
	Topic string `json:"topic"`
	Slug  string `json:"slug"`
}

func ConfirmDaily(c *gin.Context) {
	userID := c.GetString("user_id")

	var req ConfirmDailyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user settings (language + skill_level only; base_dir from config)
	var settings []UserSettings
	if err := supabase.Get(
		fmt.Sprintf("user_settings?user_id=eq.%s&select=*", userID),
		&settings,
	); err != nil || len(settings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user settings not found"})
		return
	}
	s := settings[0]

	// Calculate project dir: {base_dir}/{YYMMDD}-{Slug}
	dateStr := time.Now().Format("060102")
	projectDir := filepath.Join(config.Global.BaseDir, fmt.Sprintf("%s-%s", dateStr, req.Slug))

	if err := os.MkdirAll(projectDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if ai.Global == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI client not initialized"})
		return
	}

	// Generate curriculum
	curriculum, err := ai.Global.GenerateCurriculum(c.Request.Context(), s.Language, req.Topic, s.SkillLevel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Write TUTORSYS.md
	if err := os.WriteFile(filepath.Join(projectDir, "TUTORSYS.md"), []byte(curriculum), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Generate code files
	files, err := ai.Global.GenerateCodeFiles(c.Request.Context(), curriculum, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	created := []string{}
	for name, content := range files {
		path := filepath.Join(projectDir, name)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		created = append(created, name)
	}

	// Run go mod init/tidy for Go projects (non-fatal)
	if s.Language == "go" {
		if merr := ensureGoMod(projectDir); merr != nil {
			fmt.Printf("go mod: %v\n", merr)
		}
	}

	// Generate quiz for newbie
	if s.SkillLevel == "newbie" {
		quizData, err := ai.Global.GenerateQuizData(c.Request.Context(), curriculum, files)
		if err == nil && len(quizData) > 0 {
			if quizBytes, err := json.Marshal(quizData); err == nil {
				os.WriteFile(filepath.Join(projectDir, "quiz.json"), quizBytes, 0644)
			}
		}
	}

	// Load project and start watcher
	project.Global.Set(projectDir, curriculum)
	if ai.Global != nil {
		ai.Global.SetToken(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	}
	if err := watcher.Start(projectDir); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "watcher: " + err.Error()})
		return
	}

	// Insert daily mission to Supabase (non-fatal)
	today := time.Now().Format("2006-01-02")
	mission := DailyMission{
		UserID:     userID,
		Date:       today,
		Topic:      req.Topic,
		Slug:       req.Slug,
		ProjectDir: projectDir,
		Status:     "active",
	}
	supabase.Insert("daily_missions", mission)

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"project_dir": projectDir,
		"files":       created,
	})
}

func ConfirmDailyStream(c *gin.Context) {
	userID := c.GetString("user_id")

	var req ConfirmDailyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sendProgress := func(stage, msg string) {
		data, _ := json.Marshal(map[string]string{"stage": stage, "message": msg})
		fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", data)
		flusher.Flush()
	}
	sendError := func(msg string) {
		data, _ := json.Marshal(map[string]string{"error": msg})
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
		flusher.Flush()
	}

	sendProgress("setup", "사용자 설정 불러오는 중...")

	var settings []UserSettings
	if err := supabase.Get(
		fmt.Sprintf("user_settings?user_id=eq.%s&select=*", userID),
		&settings,
	); err != nil || len(settings) == 0 {
		sendError("사용자 설정을 찾을 수 없습니다")
		return
	}
	s := settings[0]

	if ai.Global == nil {
		sendError("AI 클라이언트가 초기화되지 않았습니다")
		return
	}

	sendProgress("curriculum", "AI가 커리큘럼을 생성하고 있습니다...")
	curriculum, err := ai.Global.GenerateCurriculum(c.Request.Context(), s.Language, req.Topic, s.SkillLevel)
	if err != nil {
		sendError("커리큘럼 생성 실패: " + err.Error())
		return
	}

	sendProgress("code", "코드 파일을 생성하고 있습니다...")
	files, err := ai.Global.GenerateCodeFiles(c.Request.Context(), curriculum, nil)
	if err != nil {
		sendError("코드 생성 실패: " + err.Error())
		return
	}

	// Build all-files map (browser will write these via LOCAL /api/project/setup)
	allFiles := make(map[string]string, len(files)+1)
	allFiles["TUTORSYS.md"] = curriculum
	for k, v := range files {
		allFiles[k] = v
	}

	if s.SkillLevel == "newbie" {
		sendProgress("quiz", "퀴즈 데이터를 생성하고 있습니다...")
		quizData, err := ai.Global.GenerateQuizData(c.Request.Context(), curriculum, files)
		if err == nil && len(quizData) > 0 {
			if quizBytes, err := json.Marshal(quizData); err == nil {
				allFiles["quiz.json"] = string(quizBytes)
			}
		}
	}

	// Insert Supabase record — project_dir stores dir_suffix (local server prepends BaseDir)
	dateStr := time.Now().Format("060102")
	dirSuffix := dateStr + "-" + req.Slug
	today := time.Now().Format("2006-01-02")
	mission := DailyMission{
		UserID:     userID,
		Date:       today,
		Topic:      req.Topic,
		Slug:       req.Slug,
		ProjectDir: dirSuffix,
		Status:     "active",
	}
	supabase.Insert("daily_missions", mission)

	doneData, _ := json.Marshal(map[string]interface{}{
		"dir_suffix":  dirSuffix,
		"files":       allFiles,
		"curriculum":  curriculum,
		"skill_level": s.SkillLevel,
		"language":    s.Language,
	})
	fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", doneData)
	flusher.Flush()
}

type NurseChatReq struct {
	Message    string                `json:"message"`
	History    []ai.NurseChatMessage `json:"history"`
	PastTopics []string              `json:"pastTopics"`
}

// NurseChatHandler streams nurse chat responses.
// POST /api/daily/nurse-chat
func NurseChatHandler(c *gin.Context) {
	userID := c.GetString("user_id")

	var req NurseChatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if ai.Global == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI not initialized"})
		return
	}

	var settings []UserSettings
	if err := supabase.Get(
		fmt.Sprintf("user_settings?user_id=eq.%s&select=*", userID),
		&settings,
	); err != nil || len(settings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user settings not found"})
		return
	}

	// If caller didn't supply past topics, fetch from DB
	if len(req.PastTopics) == 0 {
		var allHistory []DailyMission
		supabase.Get(fmt.Sprintf("daily_missions?user_id=eq.%s&order=created_at.desc&select=topic", userID), &allHistory)
		seen := map[string]bool{}
		for _, m := range allHistory {
			if !seen[m.Topic] {
				seen[m.Topic] = true
				req.PastTopics = append(req.PastTopics, m.Topic)
			}
		}
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sendChunk := func(text string) {
		data, _ := json.Marshal(map[string]string{"text": text})
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	err := ai.Global.NurseChat(
		c.Request.Context(),
		req.Message, req.History, req.PastTopics,
		settings[0].Language, settings[0].SkillLevel,
		func(chunk string) { sendChunk(chunk) },
	)
	if err != nil {
		sendChunk("죄송해요, 지금은 대화가 어려워요: " + err.Error())
	}

	fmt.Fprintf(c.Writer, "event: done\ndata: {}\n\n")
	flusher.Flush()
}
