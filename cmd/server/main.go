package main

import (
	"log"
	"net/http"
	"os"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/api"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/lsp"
	"github.com/coding-tutor/internal/middleware"
	"github.com/coding-tutor/internal/ws"
	"github.com/gin-gonic/gin"
)

func main() {
	config.Load("config.toml")
	ai.Init()

	r := gin.Default()

	// CORS
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

	// WebSocket
	r.GET("/ws", func(c *gin.Context) {
		ws.Global.ServeWS(c.Writer, c.Request)
	})
	r.GET("/ws/terminal", func(c *gin.Context) {
		ws.ServeTerminal(c.Writer, c.Request)
	})
	r.GET("/ws/lsp", lsp.ServeWS)

	// API routes
	apiGroup := r.Group("/api")
	{
		// Auth middleware for protected routes
		auth := middleware.Auth()

		// User settings
		apiGroup.GET("/user/settings", auth, api.GetUserSettings)
		apiGroup.PUT("/user/settings", auth, api.PutUserSettings)

		// Daily mission
		apiGroup.GET("/daily", auth, api.GetDaily)
		apiGroup.GET("/daily/history", auth, api.GetDailyHistory)
		apiGroup.POST("/daily/confirm", auth, api.ConfirmDaily)

		// Filesystem
		apiGroup.GET("/fs/list", auth, api.ListDir)
		apiGroup.GET("/fs/read", auth, api.ReadFile)
		apiGroup.POST("/fs/write", auth, api.WriteFile)
		apiGroup.GET("/fs/validate", auth, api.ValidateDir)

		// Project
		apiGroup.GET("/project/status", auth, api.GetProjectStatus)
		apiGroup.POST("/project/create", auth, api.CreateProject)
		apiGroup.POST("/project/confirm", auth, api.ConfirmProject)
		apiGroup.POST("/project/load", auth, api.LoadProject)
		apiGroup.POST("/project/nextstep", auth, api.AdvanceToNextStep)
		apiGroup.GET("/project/snapshots", auth, api.ListSnapshots)
		apiGroup.POST("/project/snapshot/restore", auth, api.RestoreSnapshot)
		apiGroup.GET("/quiz", auth, api.GetQuiz)
		apiGroup.GET("/run", auth, api.RunCode)
		apiGroup.POST("/explain", auth, api.ExplainWrongAnswer)
		apiGroup.POST("/chat", auth, api.Chat)
		apiGroup.POST("/goto", auth, api.GotoDefinition)
	}

	// Serve frontend in production
	if _, err := os.Stat("./frontend/dist"); err == nil {
		r.StaticFS("/assets", http.Dir("./frontend/dist/assets"))
		r.StaticFile("/", "./frontend/dist/index.html")
		r.NoRoute(func(c *gin.Context) {
			c.File("./frontend/dist/index.html")
		})
	}

	port := config.Global.Server.Port
	log.Printf("Server starting on :%s (model=%s)", port, config.Global.Gemini.Model)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
