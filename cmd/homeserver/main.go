// coding-tutor home server — runs on tutor.abcfe.net.
// Handles Gemini AI, Supabase, user auth, and serves the frontend.
// Credentials live here only; the user-side binary knows nothing.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/api"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/middleware"
	"github.com/gin-gonic/gin"
)

func main() {
	// Try homeserver.toml first, fall back to config.toml
	if _, err := os.Stat("homeserver.toml"); err == nil {
		config.Load("homeserver.toml")
	} else {
		config.Load("config.toml")
	}

	ai.Init()

	r := gin.Default()

	// CORS — allow any origin (frontend served from same domain in production)
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// AI proxy — protected by Supabase JWT (same as all other endpoints)
	r.POST("/api/ai/proxy", api.AIProxy)

	apiGroup := r.Group("/api")
	{
		auth := middleware.Auth()

		// User settings
		apiGroup.GET("/user/settings", auth, api.GetUserSettings)
		apiGroup.PUT("/user/settings", auth, api.PutUserSettings)

		// Daily missions + nurse chat
		apiGroup.GET("/daily", auth, api.GetDaily)
		apiGroup.GET("/daily/history", auth, api.GetDailyHistory)
		apiGroup.POST("/daily/confirm", auth, api.ConfirmDaily)
		apiGroup.POST("/daily/confirm-stream", auth, api.ConfirmDailyStream)
		apiGroup.POST("/daily/nurse-chat", auth, api.NurseChatHandler)

		// Project — AI operations (return files, no disk I/O)
		apiGroup.POST("/project/create", auth, api.CreateProject)
		apiGroup.POST("/project/confirm", auth, api.ConfirmProject)
		apiGroup.POST("/project/nextstep", auth, api.AdvanceToNextStep)
		apiGroup.POST("/project/complete", auth, api.CompleteProject)
		apiGroup.DELETE("/project", auth, api.DeleteProject)

		// Chat / explain
		apiGroup.POST("/chat", auth, api.Chat)
		apiGroup.POST("/explain", auth, api.ExplainWrongAnswer)
	}

	// Serve frontend in production
	if _, err := os.Stat("./frontend/dist"); err == nil {
		r.StaticFS("/assets", http.Dir("./frontend/dist/assets"))
		r.StaticFile("/", "./frontend/dist/index.html")
		r.NoRoute(func(c *gin.Context) {
			path := "./frontend/dist" + c.Request.URL.Path
			if _, err := os.Stat(path); err == nil {
				c.File(path)
				return
			}
			c.File("./frontend/dist/index.html")
		})
	}

	port := config.Global.Server.Port
	log.Printf("home server starting on :%s (model=%s)", port, config.Global.Gemini.Model)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
