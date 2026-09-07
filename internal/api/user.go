package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/supabase"
	"github.com/gin-gonic/gin"
)

type UserSettings struct {
	UserID     string    `json:"user_id"`
	BaseDir    string    `json:"base_dir"`
	Language   string    `json:"language"`
	SkillLevel string    `json:"skill_level"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var supportedUserLanguages = map[string]string{
	"go": "Go", "typescript": "TypeScript", "javascript": "JavaScript",
	"rust": "Rust", "python": "Python",
}

func canonicalUserLanguage(value string) (string, bool) {
	language, ok := supportedUserLanguages[strings.ToLower(strings.TrimSpace(value))]
	return language, ok
}

func GetUserSettings(c *gin.Context) {
	userID := c.GetString("user_id")

	var settings []UserSettings
	err := supabase.Get(c.Request.Context(),
		fmt.Sprintf("user_settings?user_id=eq.%s&select=*", supabase.FilterValue(userID)),
		&settings,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(settings) == 0 {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	// Inject server-side base_dir (CLI arg overrides DB value)
	s := settings[0]
	s.BaseDir = config.Global.BaseDir
	if language, ok := canonicalUserLanguage(s.Language); ok {
		s.Language = language
		c.JSON(http.StatusOK, gin.H{
			"user_id": s.UserID, "base_dir": s.BaseDir, "language": s.Language,
			"skill_level": s.SkillLevel, "updated_at": s.UpdatedAt, "language_supported": true,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id": s.UserID, "base_dir": s.BaseDir, "language": s.Language,
		"skill_level": s.SkillLevel, "updated_at": s.UpdatedAt, "language_supported": false,
	})
}

func PutUserSettings(c *gin.Context) {
	userID := c.GetString("user_id")

	var req struct {
		Language   string `json:"language"`
		SkillLevel string `json:"skill_level"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	language, ok := canonicalUserLanguage(req.Language)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported language"})
		return
	}
	skillLevel := strings.ToLower(strings.TrimSpace(req.SkillLevel))
	if skillLevel != "newbie" && skillLevel != "normal" && skillLevel != "experienced" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported skill_level"})
		return
	}

	settings := UserSettings{
		UserID:     userID,
		BaseDir:    config.Global.BaseDir, // always from server config
		Language:   language,
		SkillLevel: skillLevel,
		UpdatedAt:  time.Now().UTC(),
	}

	if err := supabase.Upsert(c.Request.Context(), "user_settings", settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, settings)
}
