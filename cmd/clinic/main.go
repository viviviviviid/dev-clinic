// coding-tutor clinic is the single local backend used by the Vercel frontend.
// It owns AI calls, Supabase access, project files, code execution, and WebSockets.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coding-tutor/internal/ai"
	"github.com/coding-tutor/internal/api"
	"github.com/coding-tutor/internal/config"
	"github.com/coding-tutor/internal/localapi"
	"github.com/coding-tutor/internal/lsp"
	"github.com/coding-tutor/internal/middleware"
	"github.com/coding-tutor/internal/ws"
	"github.com/gin-gonic/gin"
)

const maxRequestBody = 8 << 20 // 8 MiB

func main() {
	config.Load("config.toml")
	configureBaseDir()
	if err := validateRuntimeConfig(); err != nil {
		log.Fatal(err)
	}
	ai.Init()

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), localAccessMiddleware())
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatalf("trusted proxies: %v", err)
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"ok":          true,
			"ai_provider": config.Global.AIProvider,
		})
	})

	// WebSocket handlers authenticate the JWT subprotocol before upgrading.
	r.GET("/ws", func(c *gin.Context) { ws.Global.ServeWS(c.Writer, c.Request) })
	r.GET("/ws/terminal", func(c *gin.Context) { ws.ServeTerminal(c.Writer, c.Request) })
	r.GET("/ws/lsp", lsp.ServeWS)

	auth := middleware.Auth()
	apiGroup := r.Group("/api", auth)
	registerAPIRoutes(apiGroup)

	addr := "127.0.0.1:" + config.Global.Server.Port
	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	log.Printf("clinic starting on http://%s (base_dir=%s, ai=%s)", addr, config.Global.BaseDir, config.Global.AIProvider)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func registerAPIRoutes(apiGroup *gin.RouterGroup) {
	apiGroup.GET("/user/settings", api.GetUserSettings)
	apiGroup.PUT("/user/settings", api.PutUserSettings)

	apiGroup.GET("/daily", api.GetDaily)
	apiGroup.GET("/daily/history", api.GetDailyHistory)
	apiGroup.POST("/daily/confirm-stream", api.ConfirmDailyStream)
	apiGroup.POST("/daily/finalize", api.FinalizeDailyMission)
	apiGroup.POST("/daily/nurse-chat", api.NurseChatHandler)

	apiGroup.POST("/project/nextstep", api.AdvanceToNextStep)
	apiGroup.POST("/project/complete", api.CompleteProject)
	apiGroup.DELETE("/project", api.DeleteProject)

	apiGroup.GET("/fs/list", api.ListDir)
	apiGroup.GET("/fs/read", api.ReadFile)
	apiGroup.POST("/fs/write", api.WriteFile)
	apiGroup.GET("/fs/validate", api.ValidateDir)
	apiGroup.GET("/fs/search/files", api.SearchFiles)
	apiGroup.GET("/fs/search/content", api.SearchContent)
	apiGroup.POST("/fs/rename", api.RenameFile)
	apiGroup.DELETE("/fs/delete", api.DeleteFsEntry)
	apiGroup.GET("/fs/git-diff", api.GitDiff)

	apiGroup.GET("/run", api.RunCode)
	apiGroup.GET("/test", api.RunTest)
	apiGroup.POST("/goto", api.GotoDefinition)
	apiGroup.POST("/explain", api.ExplainWrongAnswer)
	apiGroup.POST("/chat", api.Chat)
	apiGroup.GET("/review/status", api.GetReviewStatus)
	apiGroup.POST("/review", api.StartReview)
	apiGroup.POST("/review/cancel", api.CancelReview)

	apiGroup.GET("/project/status", localapi.GetProjectStatus)
	apiGroup.POST("/project/load", localapi.LoadProject)
	apiGroup.POST("/project/setup", localapi.SetupProject)
	apiGroup.POST("/project/apply-step", localapi.ApplyStep)
	apiGroup.GET("/project/read-all", localapi.ReadAllFiles)
	apiGroup.DELETE("/project/files", localapi.DeleteProjectFiles)
	apiGroup.POST("/project/stop-watcher", localapi.StopWatcher)
	apiGroup.GET("/project/snapshots", localapi.ListSnapshots)
	apiGroup.POST("/project/snapshot/restore", localapi.RestoreSnapshot)
	apiGroup.GET("/quiz", localapi.GetQuiz)
}

func configureBaseDir() {
	dir := strings.TrimSpace(config.Global.BaseDir)
	if dir == "" {
		dir = "."
	}
	if config.Global.BaseDir == "" && len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		dir = os.Args[1]
	}
	if dir == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home
		}
	} else if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		log.Fatalf("invalid base_dir: %v", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		log.Fatalf("cannot create base_dir %s: %v", abs, err)
	}
	config.Global.BaseDir = abs
}

func validateRuntimeConfig() error {
	port, err := strconv.Atoi(config.Global.Server.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid clinic port %q", config.Global.Server.Port)
	}
	supabaseURL, err := url.Parse(strings.TrimSpace(config.Global.Supabase.URL))
	if err != nil || supabaseURL.Host == "" || supabaseURL.Scheme != "https" && supabaseURL.Scheme != "http" {
		return fmt.Errorf("SUPABASE_URL (or [supabase].url) must be configured")
	}
	if strings.TrimSpace(config.Global.Supabase.ServiceRoleKey) == "" {
		return fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY (or [supabase].service_role_key) must be configured")
	}
	return nil
}

func localAccessMiddleware() gin.HandlerFunc {
	allowed := map[string]struct{}{
		"https://tutor.abcfe.net":  {},
		"https://clinic.abcfe.net": {},
	}
	for _, origin := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		if !isLoopbackHost(c.Request.Host) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid host"})
			return
		}

		origin := c.GetHeader("Origin")
		if origin != "" && !isAllowedOrigin(origin, allowed) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "origin not allowed"})
			return
		}
		if origin != "" {
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Allow-Private-Network", "true")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBody)
		}
		c.Next()
	}
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if parsed, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback()
}

func isAllowedOrigin(origin string, allowed map[string]struct{}) bool {
	if _, ok := allowed[origin]; ok {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback()
}
