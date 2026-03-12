package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/coding-tutor/internal/ai"
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

	topics, err := ai.Global.GenerateDailyTopics(c.Request.Context(), settings[0].Language, settings[0].SkillLevel)
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

	// Get user settings
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
	projectDir := filepath.Join(s.BaseDir, fmt.Sprintf("%s-%s", dateStr, req.Slug))

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
