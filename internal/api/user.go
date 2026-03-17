package api

import (
	"fmt"
	"net/http"
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

func GetUserSettings(c *gin.Context) {
	userID := c.GetString("user_id")

	var settings []UserSettings
	err := supabase.Get(
		fmt.Sprintf("user_settings?user_id=eq.%s&select=*", userID),
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
	c.JSON(http.StatusOK, s)
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

	settings := UserSettings{
		UserID:     userID,
		BaseDir:    config.Global.BaseDir, // always from server config
		Language:   req.Language,
		SkillLevel: req.SkillLevel,
		UpdatedAt:  time.Now().UTC(),
	}

	if err := supabase.Upsert("user_settings", settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, settings)
}
