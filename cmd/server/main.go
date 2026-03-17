// coding-tutor local server — runs on the user's machine.
// No secrets required. File I/O, code execution, watcher, LSP, terminal.
// The browser (served from tutor.abcfe.net) orchestrates AI and Supabase via
// the home server, then calls this local server to write files and start the watcher.
package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/coding-tutor/internal/api"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/localapi"
	"github.com/coding-tutor/internal/lsp"
	"github.com/coding-tutor/internal/ws"
	"github.com/gin-gonic/gin"
)

func main() {
	// config.toml is optional — defaults suffice
	config.Load("config.toml")

	// base_dir: CLI arg → BASE_DIR env → current dir
	if config.Global.BaseDir == "" {
		dir := "."
		if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
			dir = os.Args[1]
		}
		if strings.HasPrefix(dir, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				dir = filepath.Join(home, dir[2:])
			}
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			log.Fatalf("invalid base_dir: %v", err)
		}
		if err := os.MkdirAll(abs, 0755); err != nil {
			log.Fatalf("cannot create base_dir %s: %v", abs, err)
		}
		config.Global.BaseDir = abs
	}
	log.Printf("local server — base_dir: %s", config.Global.BaseDir)

	r := gin.Default()

	// CORS: allow the home server origin + localhost for development
	r.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "https://tutor.abcfe.net" ||
			strings.HasPrefix(origin, "http://localhost") ||
			strings.HasPrefix(origin, "http://127.0.0.1") {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// WebSocket endpoints
	r.GET("/ws", func(c *gin.Context) {
		ws.Global.ServeWS(c.Writer, c.Request)
	})
	r.GET("/ws/terminal", func(c *gin.Context) {
		ws.ServeTerminal(c.Writer, c.Request)
	})
	r.GET("/ws/lsp", lsp.ServeWS)

	apiGroup := r.Group("/api")
	{
		// Filesystem
		apiGroup.GET("/fs/list", api.ListDir)
		apiGroup.GET("/fs/read", api.ReadFile)
		apiGroup.POST("/fs/write", api.WriteFile)
		apiGroup.GET("/fs/validate", api.ValidateDir)
		apiGroup.GET("/fs/search/files", api.SearchFiles)
		apiGroup.GET("/fs/search/content", api.SearchContent)
		apiGroup.POST("/fs/rename", api.RenameFile)
		apiGroup.DELETE("/fs/delete", api.DeleteFsEntry)
		apiGroup.GET("/fs/git-diff", api.GitDiff)

		// Code execution
		apiGroup.GET("/run", api.RunCode)
		apiGroup.GET("/test", api.RunTest)
		apiGroup.POST("/goto", api.GotoDefinition)
		apiGroup.POST("/explain", api.ExplainWrongAnswer)
		apiGroup.POST("/chat", api.Chat)

		// Project — local operations (no AI, no Supabase)
		apiGroup.GET("/project/status", localapi.GetProjectStatus)
		apiGroup.POST("/project/load", localapi.LoadProject)
		apiGroup.POST("/project/setup", localapi.SetupProject)
		apiGroup.POST("/project/apply-step", localapi.ApplyStep)
		apiGroup.GET("/project/read-all", localapi.ReadAllFiles)
		apiGroup.DELETE("/project/files", localapi.DeleteProjectFiles)
		apiGroup.POST("/project/stop-watcher", localapi.StopWatcher)
		apiGroup.GET("/project/snapshots", localapi.ListSnapshots)
		apiGroup.POST("/project/snapshot/restore", localapi.RestoreSnapshot)
		apiGroup.POST("/project/save-quiz", localapi.SaveQuiz)
		apiGroup.GET("/quiz", localapi.GetQuiz)
	}

	port := config.Global.Server.Port
	log.Printf("local server starting on 127.0.0.1:%s", port)
	if err := r.Run("127.0.0.1:" + port); err != nil {
		log.Fatal(err)
	}
}
